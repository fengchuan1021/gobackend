package screenlink

import (
	"encoding/binary"
	"hash/crc32"
	"net"
)

const (
	stunMagic        = 0x2112A442
	bindingRequest   = 0x0001
	bindingSuccess   = 0x0101
	xorMappedAddress = 0x0020
	fingerprintAttr  = 0x8028
	fingerprintXOR   = 0x5354554E
	stunHeader       = 20
)

// Reply 回应 STUN Binding 请求，返回带 XOR-MAPPED-ADDRESS 和 FINGERPRINT 的成功包。
func Reply(request []byte, source net.Addr) []byte {
	parsed, ok := parseSTUN(request)
	if !ok || parsed.method != bindingRequest {
		return nil
	}
	if !fingerprintOK(request) {
		return nil
	}
	udp, ok := source.(*net.UDPAddr)
	if !ok || udp.IP == nil {
		return nil
	}
	body := writeXORMapped(udp, parsed.transactionID)
	message := make([]byte, 0, stunHeader+len(body)+8)
	message = binary.BigEndian.AppendUint16(message, bindingSuccess)
	message = binary.BigEndian.AppendUint16(message, uint16(len(body)+8))
	message = binary.BigEndian.AppendUint32(message, stunMagic)
	message = append(message, parsed.transactionID[:]...)
	message = append(message, body...)
	crc := crc32.ChecksumIEEE(message) ^ fingerprintXOR
	message = binary.BigEndian.AppendUint16(message, fingerprintAttr)
	message = binary.BigEndian.AppendUint16(message, 4)
	message = binary.BigEndian.AppendUint32(message, crc)
	return message
}

type stunMessage struct {
	method        uint16
	transactionID [12]byte
}

func parseSTUN(packet []byte) (stunMessage, bool) {
	if len(packet) < stunHeader {
		return stunMessage{}, false
	}
	method := binary.BigEndian.Uint16(packet[0:2])
	length := int(binary.BigEndian.Uint16(packet[2:4]))
	cookie := binary.BigEndian.Uint32(packet[4:8])
	if cookie != stunMagic || stunHeader+length > len(packet) {
		return stunMessage{}, false
	}
	var parsed stunMessage
	parsed.method = method
	copy(parsed.transactionID[:], packet[8:20])
	return parsed, true
}

func fingerprintOK(packet []byte) bool {
	declared := int(binary.BigEndian.Uint16(packet[2:4]))
	end := stunHeader + declared
	offset := stunHeader
	for offset+4 <= end {
		attrType := binary.BigEndian.Uint16(packet[offset : offset+2])
		attrLen := int(binary.BigEndian.Uint16(packet[offset+2 : offset+4]))
		value := offset + 4
		padded := (attrLen + 3) &^ 3
		if value+padded > end {
			return false
		}
		if attrType == fingerprintAttr {
			if attrLen != 4 || value+4 > len(packet) {
				return false
			}
			stated := binary.BigEndian.Uint32(packet[value : value+4])
			actual := crc32.ChecksumIEEE(packet[:offset]) ^ fingerprintXOR
			return stated == actual
		}
		offset = value + padded
	}
	return true
}

func writeXORMapped(source *net.UDPAddr, transactionID [12]byte) []byte {
	if ip4 := source.IP.To4(); ip4 != nil {
		body := make([]byte, 0, 12)
		body = binary.BigEndian.AppendUint16(body, xorMappedAddress)
		body = binary.BigEndian.AppendUint16(body, 8)
		body = append(body, 0, 0x01)
		port := source.Port ^ int(stunMagic>>16)
		body = binary.BigEndian.AppendUint16(body, uint16(port))
		raw := binary.BigEndian.Uint32(ip4) ^ stunMagic
		body = binary.BigEndian.AppendUint32(body, raw)
		return body
	}
	ip6 := source.IP.To16()
	body := make([]byte, 0, 24)
	body = binary.BigEndian.AppendUint16(body, xorMappedAddress)
	body = binary.BigEndian.AppendUint16(body, 20)
	body = append(body, 0, 0x02)
	port := source.Port ^ int(stunMagic>>16)
	body = binary.BigEndian.AppendUint16(body, uint16(port))
	var mask [16]byte
	binary.BigEndian.PutUint32(mask[:4], stunMagic)
	copy(mask[4:], transactionID[:])
	for index := 0; index < 16; index++ {
		body = append(body, ip6[index]^mask[index])
	}
	return body
}

func decodeXORMapped(packet []byte) *net.UDPAddr {
	parsed, ok := parseSTUN(packet)
	if !ok {
		return nil
	}
	declared := int(binary.BigEndian.Uint16(packet[2:4]))
	end := stunHeader + declared
	if end > len(packet) {
		end = len(packet)
	}
	offset := stunHeader
	for offset+4 <= end {
		attrType := binary.BigEndian.Uint16(packet[offset : offset+2])
		attrLen := int(binary.BigEndian.Uint16(packet[offset+2 : offset+4]))
		value := offset + 4
		if attrType == xorMappedAddress && value+attrLen <= len(packet) {
			family := packet[value+1]
			port := binary.BigEndian.Uint16(packet[value+2:value+4]) ^ uint16(stunMagic>>16)
			if family == 0x01 && attrLen >= 8 {
				raw := binary.BigEndian.Uint32(packet[value+4:value+8]) ^ stunMagic
				ip := make(net.IP, 4)
				binary.BigEndian.PutUint32(ip, raw)
				return &net.UDPAddr{IP: ip, Port: int(port)}
			}
			if family == 0x02 && attrLen >= 20 {
				var mask [16]byte
				binary.BigEndian.PutUint32(mask[:4], stunMagic)
				copy(mask[4:], parsed.transactionID[:])
				ip := make(net.IP, 16)
				for index := 0; index < 16; index++ {
					ip[index] = packet[value+4+index] ^ mask[index]
				}
				return &net.UDPAddr{IP: ip, Port: int(port)}
			}
		}
		offset = value + ((attrLen + 3) &^ 3)
	}
	return nil
}
