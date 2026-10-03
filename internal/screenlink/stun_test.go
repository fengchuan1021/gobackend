package screenlink

import (
	"encoding/binary"
	"hash/crc32"
	"net"
	"testing"
)

func sampleBindingRequest() []byte {
	message := make([]byte, stunHeader)
	message[0] = 0x00
	message[1] = 0x01
	message[2] = 0x00
	message[3] = 0x08
	binary.BigEndian.PutUint32(message[4:8], stunMagic)
	copy(message[8:20], []byte("transaction!"))
	crc := crc32.ChecksumIEEE(message) ^ fingerprintXOR
	message = binary.BigEndian.AppendUint16(message, fingerprintAttr)
	message = binary.BigEndian.AppendUint16(message, 4)
	message = binary.BigEndian.AppendUint32(message, crc)
	return message
}

func TestMapsIPv4Source(t *testing.T) {
	source := &net.UDPAddr{IP: net.ParseIP("203.0.113.5"), Port: 54320}
	response := Reply(sampleBindingRequest(), source)
	if response == nil {
		t.Fatal("missing response")
	}
	got := decodeXORMapped(response)
	if got == nil || got.Port != source.Port || !got.IP.Equal(source.IP) {
		t.Fatalf("mapped = %v", got)
	}
}

func TestMapsIPv6Source(t *testing.T) {
	source := &net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 54321}
	response := Reply(sampleBindingRequest(), source)
	if response == nil {
		t.Fatal("missing response")
	}
	got := decodeXORMapped(response)
	if got == nil || got.Port != source.Port || !got.IP.Equal(source.IP) {
		t.Fatalf("mapped = %v", got)
	}
}

func TestRejectsBadFingerprint(t *testing.T) {
	request := sampleBindingRequest()
	request[len(request)-1] ^= 0xff
	source := &net.UDPAddr{IP: net.ParseIP("203.0.113.5"), Port: 9}
	if Reply(request, source) != nil {
		t.Fatal("expected bad fingerprint to be rejected")
	}
}
