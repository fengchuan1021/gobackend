package scrcpyudp

import (
	"encoding/binary"
	"log"
	"net"
	"sync"
	"time"

	"gobackend/internal/udpserver"
)

const (
	Magic          = 0x53435255 // "SCRU"
	HeaderSize     = 16
	MaxPayload     = 1200
	MaxFrags       = 2048
	MaxFrame       = 2 * 1024 * 1024
	DefaultPort    = 27183
	TypeHello      = 1
	TypeMeta       = 2
	TypeVideo      = 3
	TypeControl    = 4
	TypeDeviceMsg  = 5
	DeviceNameLen  = 64
	SerialLen      = 64
	MetaLen        = DeviceNameLen + SerialLen
	PeerTimeout    = 60 * time.Second
	KeepaliveEvery = 20 * time.Second
	SubQueueSize   = 16
)

var Default *Server

var (
	expectedMu sync.Mutex
	expected   = map[string]time.Time{}
)

// ExpectSerial 记录网页刚下发 beginScrcpy 的设备，便于 META 序列号不一致时绑定。
func ExpectSerial(serial string) {
	if serial == "" {
		return
	}
	expectedMu.Lock()
	expected[serial] = time.Now()
	expectedMu.Unlock()
}

func takeExpectedSerial(used map[string]*peer) string {
	expectedMu.Lock()
	defer expectedMu.Unlock()
	now := time.Now()
	var best string
	var bestT time.Time
	for serial, t := range expected {
		if now.Sub(t) > 2*time.Minute {
			delete(expected, serial)
			continue
		}
		if p := used[serial]; p != nil && time.Since(p.lastRx) < PeerTimeout {
			continue
		}
		if best == "" || t.After(bestT) {
			best = serial
			bestT = t
		}
	}
	if best != "" {
		delete(expected, best)
	}
	return best
}

type Subscriber struct {
	serial string
	ch     chan []byte
	closed chan struct{}
}

func (s *Subscriber) C() <-chan []byte {
	return s.ch
}

type peer struct {
	addr       *net.UDPAddr
	name       string
	serial     string
	lastRx     time.Time
	videoAsm   reassembly
	codecPkt   []byte
	sessionPkt []byte
	configPkt  []byte
}

type reassembly struct {
	active    bool
	frameID   uint32
	flags     byte
	fragCount uint16
	received  uint16
	buf       []byte
	fragLen   []uint16
}

func (a *reassembly) reset() {
	a.active = false
	a.frameID = 0
	a.flags = 0
	a.fragCount = 0
	a.received = 0
	a.buf = nil
	a.fragLen = nil
}

type Server struct {
	mu      sync.Mutex
	conn    *net.UDPConn
	peers   map[string]*peer // ip:port
	serials map[string]*peer
	subs    map[string]map[*Subscriber]struct{}
}

func New() *Server {
	return &Server{
		peers:   make(map[string]*peer),
		serials: make(map[string]*peer),
		subs:    make(map[string]map[*Subscriber]struct{}),
	}
}

func Run(port int) {
	if port <= 0 {
		port = DefaultPort
	}
	s := New()
	Default = s
	if err := s.listen(port); err != nil {
		log.Printf("scrcpyudp listen :%d failed: %v", port, err)
		return
	}
	log.Printf("scrcpyudp listening on UDP :%d", port)
	go s.sweepLoop()
	go s.keepaliveLoop()
	s.recvLoop()
}

func (s *Server) listen(port int) error {
	addr := &net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return err
	}
	if err := conn.SetReadBuffer(4 * 1024 * 1024); err != nil {
		log.Printf("scrcpyudp set read buffer: %v", err)
	}
	s.conn = conn
	return nil
}

func (s *Server) recvLoop() {
	buf := make([]byte, HeaderSize+MaxPayload)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("scrcpyudp read: %v", err)
			return
		}
		s.handleDatagram(addr, buf[:n])
	}
}

func (s *Server) handleDatagram(addr *net.UDPAddr, raw []byte) {
	typ, flags, frameID, fragIdx, fragCount, payload, ok := parseHeader(raw)
	if !ok || typ == TypeControl {
		return
	}

	key := addr.String()
	s.mu.Lock()
	p := s.peers[key]
	if p == nil {
		p = &peer{addr: addr, lastRx: time.Now()}
		s.peers[key] = p
		log.Printf("scrcpyudp peer added %s", key)
	}
	p.lastRx = time.Now()
	p.addr = addr
	s.resolveSerialLocked(p)

	switch typ {
	case TypeHello:
		s.mu.Unlock()
		return
	case TypeMeta:
		serial, cached := s.handleMetaLocked(p, payload)
		s.mu.Unlock()
		for _, pkt := range cached {
			s.broadcast(serial, pkt)
		}
		return
	case TypeVideo:
		complete, _, done := p.videoAsm.add(frameID, flags, fragIdx, fragCount, payload)
		if !done {
			s.mu.Unlock()
			return
		}
		serial := p.serial
		s.cacheVideoLocked(p, complete)
		s.mu.Unlock()
		if serial != "" {
			s.broadcast(serial, complete)
		}
		return
	case TypeDeviceMsg:
		s.mu.Unlock()
		return
	default:
		s.mu.Unlock()
	}
}

func (s *Server) resolveSerialLocked(p *peer) {
	if p.serial != "" {
		return
	}
	ip := ""
	if p.addr != nil {
		ip = p.addr.IP.String()
	}
	serial := takeExpectedSerial(s.serials)
	if serial == "" {
		serial = udpserver.FindSerialByIP(ip)
	}
	if serial == "" {
		return
	}
	p.serial = serial
	s.serials[serial] = p
}

func (s *Server) handleMetaLocked(p *peer, payload []byte) (string, [][]byte) {
	if len(payload) < DeviceNameLen {
		return "", nil
	}
	name := cString(payload[:DeviceNameLen])
	serial := ""
	if len(payload) >= MetaLen {
		serial = cString(payload[DeviceNameLen : DeviceNameLen+SerialLen])
	}
	p.name = name
	first := p.serial == ""
	s.resolveSerialLocked(p)
	if p.serial == "" {
		p.serial = serial
	}
	if p.serial == "" {
		return "", nil
	}
	if old := s.serials[p.serial]; old != nil && old != p {
		oldKey := old.addr.String()
		delete(s.peers, oldKey)
	}
	s.serials[p.serial] = p
	if serial != "" && serial != p.serial {
		s.serials[serial] = p
	}
	log.Printf("scrcpyudp device %s serial=%s meta=%s peer=%s", name, p.serial, serial, p.addr)
	if !first {
		return p.serial, nil
	}
	cached := make([][]byte, 0, 3)
	if len(p.codecPkt) > 0 {
		cached = append(cached, clone(p.codecPkt))
	}
	if len(p.sessionPkt) > 0 {
		cached = append(cached, clone(p.sessionPkt))
	}
	if len(p.configPkt) > 0 {
		cached = append(cached, clone(p.configPkt))
	}
	return p.serial, cached
}

func (s *Server) cacheVideoLocked(p *peer, data []byte) {
	if isCodecPacket(data) {
		p.codecPkt = clone(data)
		return
	}
	if isSessionPacket(data) {
		p.sessionPkt = clone(data)
		return
	}
	if isConfigPacket(data) {
		p.configPkt = clone(data)
	}
}

func (s *Server) Cached(serial string) [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.serials[serial]
	if p == nil {
		return nil
	}
	out := make([][]byte, 0, 3)
	if len(p.codecPkt) > 0 {
		out = append(out, clone(p.codecPkt))
	}
	if len(p.sessionPkt) > 0 {
		out = append(out, clone(p.sessionPkt))
	}
	if len(p.configPkt) > 0 {
		out = append(out, clone(p.configPkt))
	}
	return out
}

func (s *Server) Subscribe(serial string) *Subscriber {
	sub := &Subscriber{
		serial: serial,
		ch:     make(chan []byte, SubQueueSize),
		closed: make(chan struct{}),
	}
	s.mu.Lock()
	if s.subs[serial] == nil {
		s.subs[serial] = make(map[*Subscriber]struct{})
	}
	s.subs[serial][sub] = struct{}{}
	s.mu.Unlock()
	return sub
}

func (s *Server) Unsubscribe(sub *Subscriber) {
	if sub == nil {
		return
	}
	s.mu.Lock()
	if set, ok := s.subs[sub.serial]; ok {
		delete(set, sub)
		if len(set) == 0 {
			delete(s.subs, sub.serial)
		}
	}
	s.mu.Unlock()
	select {
	case <-sub.closed:
	default:
		close(sub.closed)
	}
}

func (s *Server) broadcast(serial string, data []byte) {
	s.mu.Lock()
	set := s.subs[serial]
	if len(set) == 0 {
		s.mu.Unlock()
		return
	}
	subs := make([]*Subscriber, 0, len(set))
	for sub := range set {
		subs = append(subs, sub)
	}
	s.mu.Unlock()

	for _, sub := range subs {
		cp := clone(data)
		select {
		case sub.ch <- cp:
		default:
		}
	}
}

func (s *Server) sweepLoop() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for range t.C {
		s.mu.Lock()
		now := time.Now()
		for key, p := range s.peers {
			if now.Sub(p.lastRx) < PeerTimeout {
				continue
			}
			log.Printf("scrcpyudp peer timeout %s serial=%s", key, p.serial)
			if p.serial != "" && s.serials[p.serial] == p {
				delete(s.serials, p.serial)
			}
			delete(s.peers, key)
		}
		s.mu.Unlock()
	}
}

func (s *Server) keepaliveLoop() {
	t := time.NewTicker(KeepaliveEvery)
	defer t.Stop()
	hello := makeHello()
	for range t.C {
		s.mu.Lock()
		addrs := make([]*net.UDPAddr, 0, len(s.peers))
		for _, p := range s.peers {
			if p.addr != nil {
				addrs = append(addrs, p.addr)
			}
		}
		conn := s.conn
		s.mu.Unlock()
		if conn == nil {
			continue
		}
		for _, addr := range addrs {
			if _, err := conn.WriteToUDP(hello, addr); err != nil {
				log.Printf("scrcpyudp keepalive %s: %v", addr, err)
			}
		}
	}
}

func makeHello() []byte {
	pkt := make([]byte, HeaderSize)
	binary.BigEndian.PutUint32(pkt[0:4], Magic)
	pkt[4] = TypeHello
	return pkt
}

func parseHeader(raw []byte) (typ, flags byte, frameID uint32, fragIdx, fragCount uint16, payload []byte, ok bool) {
	if len(raw) < HeaderSize {
		return
	}
	if binary.BigEndian.Uint32(raw[0:4]) != Magic {
		return
	}
	typ = raw[4]
	flags = raw[5]
	frameID = binary.BigEndian.Uint32(raw[6:10])
	fragIdx = binary.BigEndian.Uint16(raw[10:12])
	fragCount = binary.BigEndian.Uint16(raw[12:14])
	payloadLen := binary.BigEndian.Uint16(raw[14:16])
	if int(HeaderSize)+int(payloadLen) != len(raw) {
		return
	}
	payload = raw[HeaderSize:]
	ok = true
	return
}

func (a *reassembly) add(frameID uint32, flags byte, fragIdx, fragCount uint16, payload []byte) ([]byte, byte, bool) {
	if fragCount == 0 || fragCount > MaxFrags || fragIdx >= fragCount || len(payload) > MaxPayload {
		return nil, 0, false
	}
	if fragCount == 1 {
		a.reset()
		return clone(payload), flags, true
	}
	if !a.active || a.frameID != frameID {
		a.reset()
		capacity := int(fragCount) * MaxPayload
		if capacity > MaxFrame {
			return nil, 0, false
		}
		a.active = true
		a.frameID = frameID
		a.flags = flags
		a.fragCount = fragCount
		a.buf = make([]byte, capacity)
		a.fragLen = make([]uint16, fragCount)
	}
	if a.fragLen[fragIdx] != 0 {
		return nil, 0, false
	}
	copy(a.buf[int(fragIdx)*MaxPayload:], payload)
	a.fragLen[fragIdx] = uint16(len(payload))
	a.received++
	if a.received != a.fragCount {
		return nil, 0, false
	}
	total := 0
	for _, n := range a.fragLen {
		total += int(n)
	}
	out := make([]byte, total)
	off := 0
	for i := uint16(0); i < a.fragCount; i++ {
		n := int(a.fragLen[i])
		copy(out[off:], a.buf[int(i)*MaxPayload:int(i)*MaxPayload+n])
		off += n
	}
	outFlags := a.flags
	a.reset()
	return out, outFlags, true
}

func isCodecPacket(data []byte) bool {
	return len(data) == 4
}

func isSessionPacket(data []byte) bool {
	return len(data) >= 12 && data[0]&0x80 != 0
}

func isConfigPacket(data []byte) bool {
	if len(data) < 12 || data[0]&0x80 != 0 {
		return false
	}
	ptsFlags := binary.BigEndian.Uint64(data[:8])
	return ptsFlags&(1<<62) != 0
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

func clone(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

func Subscribe(serial string) *Subscriber {
	if Default == nil {
		return &Subscriber{serial: serial, ch: make(chan []byte), closed: make(chan struct{})}
	}
	return Default.Subscribe(serial)
}

func Unsubscribe(sub *Subscriber) {
	if Default != nil {
		Default.Unsubscribe(sub)
		return
	}
	if sub != nil {
		select {
		case <-sub.closed:
		default:
			close(sub.closed)
		}
	}
}

func Cached(serial string) [][]byte {
	if Default == nil {
		return nil
	}
	return Default.Cached(serial)
}
