package udpserver

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
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
)

var (
	clients   = make(map[string]*net.UDPAddr)
	clientsMu sync.RWMutex
	conn      *net.UDPConn
	connMu    sync.RWMutex
	pending   sync.Map // msgID (uint32) -> chan []byte
	nextMsgID uint32   = 1
)

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

// NextMsgID 获取下一个消息 ID
func NextMsgID() uint32 {
	return atomic.AddUint32(&nextMsgID, 1)
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
			fmt.Println(time.Now().Format("2006-01-02 15:04:05"), "heartbeat", from)
			serial := string(payload)
			if serial != "" {
				clientsMu.Lock()
				clients[serial] = from
				clientsMu.Unlock()
			}
			hasTask := msgID
			if hasTask == 0 {
				//check unstarted task
			}
			resp := buildPacket(CmdHeartbeat, 0, nil)
			c.WriteToUDP(resp, from)
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
func SendCommand(serial string, cmdType uint32, payload []byte) ([]byte, error) {
	msgID := NextMsgID()
	clientsMu.RLock()
	addr, ok := clients[serial]
	clientsMu.RUnlock()
	if !ok {
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

	const respTimeout = 30 * time.Second
	const maxRetries = 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		pkt := buildPacket(cmdType, msgID, payload)
		if _, err := c.WriteToUDP(pkt, addr); err != nil {
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
