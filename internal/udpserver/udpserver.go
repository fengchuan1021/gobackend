package udpserver

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/model"
)

const (
	heartbeatWorkerCount = 8
	heartbeatQueueSize   = 256

	clientStaleTimeout       = 40 * time.Second
	clientStaleSweepInterval = 1 * time.Minute
)

const (
	Magic               = 0x53434F52 // "SCOR"
	HeaderSize          = 16
	CmdHeartbeat        = 0
	CmdGetScreenshot    = 1
	CmdGetXmlLayout     = 2
	CmdSetToken         = 3
	CmdExecuteCommand   = 4
	CmdAck              = 5
	CmdExecuteDevScript = 6
	CmdRunTaskScript    = 7
	CmdStopTask         = 8
	CmdBackupApps       = 9
	CmdResetDevice      = 10
)

type ConnInfo struct {
	Conn          *net.UDPAddr
	Ip            string
	LastHeartbeat time.Time
}

// TaskDevicesByUserIP 维护「用户 ID → 设备所在 IP → 正在执行任务的设备 serial」。
// IP 建议使用与 ConnInfo.Ip 一致的规范化字符串（如 net.IP.String()），便于与在线信息对齐。
type TaskDevicesByUserIP struct {
	mu     sync.RWMutex
	byUser map[uint]map[string]map[string]struct{} // userID -> ip -> serials
}

func NewTaskDevicesByUserIP() *TaskDevicesByUserIP {
	return &TaskDevicesByUserIP{
		byUser: make(map[uint]map[string]map[string]struct{}),
	}
}

// Add 将设备标记为该用户、该 IP 下正在执行任务（同一 serial 重复添加不增加数量）。
func (t *TaskDevicesByUserIP) Add(userID uint, ip, serial string) {
	if t == nil || serial == "" || ip == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	byIP := t.byUser[userID]
	if byIP == nil {
		byIP = make(map[string]map[string]struct{})
		t.byUser[userID] = byIP
	}
	set := byIP[ip]
	if set == nil {
		set = make(map[string]struct{})
		byIP[ip] = set
	}
	set[serial] = struct{}{}
}

// remoteBySerial 在已持有 t.mu 的前提下，遍历该用户下所有 IP 的 serial 集合，删除匹配的 serial（IP 变更后可能挂在旧 IP 下）。
func (t *TaskDevicesByUserIP) remoteBySerial(userID uint, serial string) {
	if t == nil || serial == "" {
		return
	}
	byIP, ok := t.byUser[userID]
	if !ok {
		return
	}
	for ip, set := range byIP {
		if _, found := set[serial]; !found {
			continue
		}
		delete(set, serial)
		if len(set) == 0 {
			delete(byIP, ip)
		}
	}
	if len(byIP) == 0 {
		delete(t.byUser, userID)
	}
}

// Remove 取消该用户、该 IP 下某设备的正在执行标记。
func (t *TaskDevicesByUserIP) Remove(userID uint, ip, serial string) {
	if t == nil || serial == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	byIP, ok := t.byUser[userID]
	if !ok {
		return
	}
	set, ok := byIP[ip]
	if !ok {
		t.remoteBySerial(userID, serial)
		return
	}
	if _, has := set[serial]; !has {
		t.remoteBySerial(userID, serial)
		return
	}
	delete(set, serial)
	if len(set) == 0 {
		delete(byIP, ip)
	}
	if len(byIP) == 0 {
		delete(t.byUser, userID)
	}
}

// RunningCount 返回指定用户、指定 IP 下正在执行任务的设备数量（按 serial 去重）。
func (t *TaskDevicesByUserIP) RunningCount(userID uint, ip string) int {
	if t == nil || ip == "" {
		return 0
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	byIP, ok := t.byUser[userID]
	if !ok {
		return 0
	}
	set, ok := byIP[ip]
	if !ok {
		return 0
	}
	return len(set)
}

var (
	clients   = make(map[string]*ConnInfo)
	clientsMu sync.RWMutex
	conn      *net.UDPConn
	connMu    sync.RWMutex
	pending   sync.Map // msgID (uint32) -> chan []byte
	nextMsgID uint32   = 1

	heartbeatCh chan heartbeatJob
	heartbeatWG sync.WaitGroup

	// TaskDevices 全局：按用户与设备 IP 统计当前正在执行任务的设备，供限流或展示使用。
	TaskDevices = NewTaskDevicesByUserIP()
)

type heartbeatJob struct {
	uid     uint
	serial  string
	hasTask uint32
	from    *net.UDPAddr
}

func parsePacket(buf []byte) (magic uint32, length uint32, cmdType uint32, messageID uint32, payload []byte, ok bool) {
	if len(buf) < HeaderSize {
		return 0, 0, 0, 0, nil, false
	}
	magic = binary.LittleEndian.Uint32(buf[0:4])
	length = binary.LittleEndian.Uint32(buf[4:8])
	cmdType = binary.LittleEndian.Uint32(buf[8:12])
	messageID = binary.LittleEndian.Uint32(buf[12:16])
	if magic != Magic || length < HeaderSize || int(length) > len(buf) {
		return 0, 0, 0, 0, nil, false
	}
	if length > HeaderSize {
		payload = buf[HeaderSize:length]
	}
	return magic, length, cmdType, messageID, payload, true
}

func buildPacket(cmdType uint32, messageID uint32, payload []byte) []byte {
	plen := len(payload)
	buf := make([]byte, HeaderSize+plen)
	binary.LittleEndian.PutUint32(buf[0:4], Magic)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(HeaderSize+plen))
	binary.LittleEndian.PutUint32(buf[8:12], cmdType)
	binary.LittleEndian.PutUint32(buf[12:16], messageID)
	if plen > 0 {
		copy(buf[HeaderSize:], payload)
	}
	return buf
}

// heartbeatAckPacket 即 buildPacket(CmdHeartbeat, 0, nil)，只分配一次供心跳回复复用
var heartbeatAckPacket = buildPacket(CmdHeartbeat, 0, nil)

// NextMsgID 获取下一个消息 ID
func NextMsgID() uint32 {
	return atomic.AddUint32(&nextMsgID, 1)
}

func cloneUDPAddr(a *net.UDPAddr) *net.UDPAddr {
	if a == nil {
		return nil
	}
	b := *a
	if len(a.IP) > 0 {
		b.IP = append(net.IP(nil), a.IP...)
	}
	return &b
}

func registerHeartbeatClient(job *heartbeatJob) {
	if job.serial == "" {
		return
	}
	clientsMu.Lock()
	if ci, ok := clients[job.serial]; ok {
		ci.Conn = job.from
		ci.Ip = job.from.IP.String()
		ci.LastHeartbeat = time.Now()
	} else {
		clients[job.serial] = &ConnInfo{
			Conn:          job.from,
			Ip:            job.from.IP.String(),
			LastHeartbeat: time.Now(),
		}
	}
	clientsMu.Unlock()
}

// pruneStaleClients 删除 LastHeartbeat 早于 clientStaleTimeout 的在线记录
func pruneStaleClients() {
	now := time.Now()
	clientsMu.Lock()
	for serial, ci := range clients {
		if ci == nil || now.Sub(ci.LastHeartbeat) > clientStaleTimeout {
			delete(clients, serial)
		}
	}
	clientsMu.Unlock()
}

func staleClientCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(clientStaleSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruneStaleClients()
		}
	}
}

// maybeRunPendingTaskFromHeartbeat 在设备空闲心跳时检查是否有待运行任务并下发
func maybeRunPendingTaskFromHeartbeat(job *heartbeatJob) {
	if job.hasTask != 0 {
		TaskDevices.Add(job.uid, job.from.IP.String(), job.serial)
		return
	} else {
		TaskDevices.Remove(job.uid, job.from.IP.String(), job.serial)
	}
	var newTask model.Task
	if err := database.DB.Preload("Device").Where("device_serial = ? and (status=0 or status=3 or status=1)", job.serial).Order("left_round desc").First(&newTask).Error; err != nil {
		return
	}
	if newTask.Device.ExpireAt != nil && newTask.Device.ExpireAt.After(time.Now()) && newTask.ID != 0 {
		go SendCommand(job.serial, CmdRunTaskScript, []byte(strconv.Itoa(int(newTask.ID))), job.uid)
	}
}

func handleHeartbeatJob(c *net.UDPConn, job heartbeatJob) {
	registerHeartbeatClient(&job)
	maybeRunPendingTaskFromHeartbeat(&job)
	if _, err := c.WriteToUDP(heartbeatAckPacket, job.from); err != nil {
		log.Printf("UDP heartbeat reply failed: %v", err)
	}
}

func startHeartbeatWorkers(c *net.UDPConn) {
	heartbeatCh = make(chan heartbeatJob, heartbeatQueueSize)
	for i := 0; i < heartbeatWorkerCount; i++ {
		heartbeatWG.Add(1)
		go func() {
			defer heartbeatWG.Done()
			for job := range heartbeatCh {
				handleHeartbeatJob(c, job)
			}
		}()
	}
}

func stopHeartbeatWorkers() {
	if heartbeatCh == nil {
		return
	}
	close(heartbeatCh)
	heartbeatWG.Wait()
	heartbeatCh = nil
}

// Run 启动 UDP 服务
func Run(port int) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Printf("UDP resolve failed: %v", err)
		return
	}
	c, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("UDP listen failed: %v", err)
		return
	}
	defer c.Close()

	connMu.Lock()
	conn = c
	connMu.Unlock()
	defer func() {
		connMu.Lock()
		conn = nil
		connMu.Unlock()
	}()

	startHeartbeatWorkers(c)
	defer stopHeartbeatWorkers()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go staleClientCleanupLoop(ctx)

	log.Printf("UDP server listening on :%d", port)

	buf := make([]byte, 65536)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			log.Printf("UDP read error: %v", err)
			continue
		}
		if n < HeaderSize {
			continue
		}

		_, _, cmdType, msgID, payload, ok := parsePacket(buf[:n])
		if !ok {
			continue
		}

		switch cmdType {
		case CmdHeartbeat:
			serialAndUid := string(append([]byte(nil), payload...))
			serialAndUidSplit := strings.Split(serialAndUid, ",")
			if len(serialAndUidSplit) != 2 {
				continue
			}
			serial := serialAndUidSplit[0]
			uid, err := strconv.ParseUint(serialAndUidSplit[1], 10, 32)
			if err != nil {
				continue
			}
			job := heartbeatJob{
				uid:     uint(uid),
				serial:  serial,
				hasTask: msgID,
				from:    cloneUDPAddr(from),
			}
			heartbeatCh <- job
		case CmdAck:
			// 忽略 ACK，命令结果通过 HTTP /udp/cmdcallback 返回
		}
	}
}

// DeliverResult 投递命令结果到等待的 channel（由 HTTP cmdcallback 调用）
func DeliverResult(msgID uint32, payload []byte) bool {
	if msgID == 0 {
		return false
	}
	ch, ok := pending.LoadAndDelete(msgID)
	if !ok {
		return false
	}
	select {
	case ch.(chan []byte) <- payload:
		return true
	default:
		return false
	}
}

// SendCommand 向指定序列号的设备发送 UDP 命令，通过 sync.Map + channel 等待结果
func SendCommand(serial string, cmdType uint32, payload []byte, userID uint) ([]byte, error) {
	var device model.Device
	err := database.DB.Preload("User").Where("serial = ?", serial).First(&device).Error
	if err != nil {
		return nil, fmt.Errorf("device %s not found", serial)
	}
	if device.ExpireAt == nil || device.ExpireAt.Before(time.Now()) {
		return nil, fmt.Errorf("device %s expired", serial)
	}
	msgID := NextMsgID()
	clientsMu.RLock()
	info, ok := clients[serial]
	var udpAddr *net.UDPAddr
	if ok && info != nil {
		udpAddr = info.Conn
	}
	clientsMu.RUnlock()
	if !ok || udpAddr == nil {
		return nil, fmt.Errorf("device %s not online", serial)
	}

	connMu.RLock()
	c := conn
	connMu.RUnlock()
	if c == nil {
		return nil, fmt.Errorf("UDP server not ready")
	}

	ch := make(chan []byte, 1)
	pending.Store(msgID, ch)
	defer pending.Delete(msgID)

	const respTimeout = 3 * time.Second
	const maxRetries = 4

	for attempt := 0; attempt < maxRetries; attempt++ {
		pkt := buildPacket(cmdType, msgID, payload)
		if _, err := c.WriteToUDP(pkt, udpAddr); err != nil {
			return nil, err
		}

		select {
		case result := <-ch:
			return result, nil
		case <-time.After(respTimeout):
			if attempt < maxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("timeout after %d retries", maxRetries)
		}
	}

	return nil, fmt.Errorf("timeout")
}
