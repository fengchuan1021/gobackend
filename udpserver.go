package main

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
	udpMagic          = 0x53434F52 // "SCOR"
	udpHeader         = 16
	cmdHeartbeat      = 0
	cmdGetScreenshot  = 1
	cmdGetXmlLayout   = 2
	cmdSetToken       = 3
	cmdExecuteCommand = 4
	cmdAck            = 5
)

var (
	udpClients   = make(map[string]*net.UDPAddr) // serial -> addr
	udpClientsMu sync.RWMutex
	udpConn      *net.UDPConn
	udpConnMu    sync.RWMutex
	udpPending   sync.Map // msgID (uint32) -> chan []byte
	udpNextMsgID uint32   = 1
)

func parsePacket(buf []byte) (magic uint32, length uint32, cmdType uint32, messageID uint32, payload []byte, ok bool) {
	if len(buf) < udpHeader {
		return 0, 0, 0, 0, nil, false
	}
	magic = binary.LittleEndian.Uint32(buf[0:4])
	length = binary.LittleEndian.Uint32(buf[4:8])
	cmdType = binary.LittleEndian.Uint32(buf[8:12])
	messageID = binary.LittleEndian.Uint32(buf[12:16])
	if magic != udpMagic || length < udpHeader || int(length) > len(buf) {
		return 0, 0, 0, 0, nil, false
	}
	if length > udpHeader {
		payload = buf[udpHeader:length]
	}
	return magic, length, cmdType, messageID, payload, true
}

func buildPacket(cmdType uint32, messageID uint32, payload []byte) []byte {
	plen := len(payload)
	buf := make([]byte, udpHeader+plen)
	binary.LittleEndian.PutUint32(buf[0:4], udpMagic)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(udpHeader+plen))
	binary.LittleEndian.PutUint32(buf[8:12], cmdType)
	binary.LittleEndian.PutUint32(buf[12:16], messageID)
	if plen > 0 {
		copy(buf[udpHeader:], payload)
	}
	return buf
}

// NextMsgID 获取下一个消息 ID
func NextMsgID() uint32 {
	return atomic.AddUint32(&udpNextMsgID, 1)
}

func runUDPServer(port int) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Printf("UDP resolve failed: %v", err)
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("UDP listen failed: %v", err)
		return
	}
	defer conn.Close()

	udpConnMu.Lock()
	udpConn = conn
	udpConnMu.Unlock()
	defer func() {
		udpConnMu.Lock()
		udpConn = nil
		udpConnMu.Unlock()
	}()

	log.Printf("UDP server listening on :%d", port)

	buf := make([]byte, 65536)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("UDP read error: %v", err)
			continue
		}
		if n < udpHeader {
			continue
		}

		_, _, cmdType, msgID, payload, ok := parsePacket(buf[:n])
		if !ok {
			continue
		}

		switch cmdType {
		case cmdHeartbeat:
			serial := string(payload)
			if serial != "" {
				udpClientsMu.Lock()
				udpClients[serial] = from
				udpClientsMu.Unlock()
			}
			hasTask := msgID
			if hasTask == 0 {
				//check unstarted task
			}
			resp := buildPacket(cmdHeartbeat, 0, nil)
			conn.WriteToUDP(resp, from)
		case cmdAck:
			// 忽略 ACK
		default:
			// 命令响应：按 msgID 投递到 channel
			if msgID != 0 {
				if ch, ok := udpPending.LoadAndDelete(msgID); ok {
					select {
					case ch.(chan []byte) <- payload:
					default:
					}
				}
			}
		}
	}
}

// SendUDPCommand 向指定序列号的设备发送 UDP 命令，通过 sync.Map + channel 等待结果
// msgID 由调用者传递（可用 NextMsgID() 获取），心跳命令传 0
func SendUDPCommand(serial string, msgID uint32, cmdType uint32, payload []byte) ([]byte, error) {
	udpClientsMu.RLock()
	addr, ok := udpClients[serial]
	udpClientsMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("device %s not online", serial)
	}

	udpConnMu.RLock()
	conn := udpConn
	udpConnMu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("UDP server not ready")
	}

	ch := make(chan []byte, 1)
	udpPending.Store(msgID, ch)
	defer udpPending.Delete(msgID)

	const respTimeout = 30 * time.Second
	const ackRetryInterval = 3 * time.Second
	const maxRetries = 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		pkt := buildPacket(cmdType, msgID, payload)
		if _, err := conn.WriteToUDP(pkt, addr); err != nil {
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
func CmdCallback() {

}
