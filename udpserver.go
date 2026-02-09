package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"sync"
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
			resp := buildPacket(cmdHeartbeat, 0, nil)
			conn.WriteToUDP(resp, from)
			if hasTask == 0 {

			}

		}
	}
}

// SendUDPCommand 向指定序列号的设备发送 UDP 命令，并等待响应
// msgID 由调用者传递，心跳命令传 0，非心跳命令传非 0 值
func SendUDPCommand(serial string, msgID uint32, cmdType uint32, payload []byte) ([]byte, error) {
	udpClientsMu.RLock()
	addr, ok := udpClients[serial]
	udpClientsMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("device %s not online", serial)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	const ackTimeout = 3 * time.Second
	const maxRetries = 3
	var ackReceived bool
	var respPayload []byte

	for attempt := 0; attempt < maxRetries; attempt++ {
		pkt := buildPacket(cmdType, msgID, payload)
		if _, err := conn.Write(pkt); err != nil {
			return nil, err
		}

		conn.SetReadDeadline(time.Now().Add(ackTimeout))
		respBuf := make([]byte, 65536)
		n, _, err := conn.ReadFromUDP(respBuf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return nil, err
		}
		if n < udpHeader {
			continue
		}

		_, _, respCmd, respMsgID, pl, ok := parsePacket(respBuf[:n])
		if !ok {
			continue
		}

		if respCmd == cmdAck && respMsgID == msgID {
			ackReceived = true
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))
			n, _, err = conn.ReadFromUDP(respBuf)
			if err != nil {
				return nil, err
			}
			if n >= udpHeader {
				_, _, _, _, respPayload, ok = parsePacket(respBuf[:n])
				if ok {
					return respPayload, nil
				}
			}
			return nil, fmt.Errorf("no response after ack")
		}

		if respCmd == cmdType && respMsgID == msgID {
			return pl, nil
		}
	}

	if !ackReceived {
		return nil, fmt.Errorf("no ack after %d retries", maxRetries)
	}
	if respPayload != nil {
		return respPayload, nil
	}
	return nil, fmt.Errorf("timeout")
}
