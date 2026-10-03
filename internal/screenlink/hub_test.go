package screenlink

import (
	"strings"
	"testing"
)

type chanBox struct {
	ch chan string
}

func newChanBox() *chanBox {
	return &chanBox{ch: make(chan string, 64)}
}

func (b *chanBox) Send(text string) bool {
	b.ch <- text
	return true
}

func (b *chanBox) tryRecv() (string, bool) {
	select {
	case text := <-b.ch:
		return text, true
	default:
		return "", false
	}
}

func TestPairsAndForwardsOffer(t *testing.T) {
	hub := NewHub()
	phoneBox := newChanBox()
	browserBox := newChanBox()
	phone, err := hub.Register("ab12cd", "Pixel", phoneBox)
	if err != nil {
		t.Fatal(err)
	}
	registered, ok := phoneBox.tryRecv()
	if !ok || !contains(registered, "registered") {
		t.Fatalf("registered = %q", registered)
	}
	browser, err := hub.Join("ab12cd", browserBox)
	if err != nil {
		t.Fatal(err)
	}
	joined, ok := browserBox.tryRecv()
	if !ok || !contains(joined, "Pixel") {
		t.Fatalf("joined = %q", joined)
	}
	peer, ok := phoneBox.tryRecv()
	if !ok || !contains(peer, "peer") {
		t.Fatalf("peer = %q", peer)
	}
	hub.Forward(phone, `{"type":"offer","sdp":"v=0"}`)
	offer, ok := browserBox.tryRecv()
	if !ok || offer != `{"type":"offer","sdp":"v=0"}` {
		t.Fatalf("offer = %q", offer)
	}
	hub.Forward(browser, `{"type":"answer","sdp":"v=0"}`)
	answer, ok := phoneBox.tryRecv()
	if !ok || !contains(answer, "answer") {
		t.Fatalf("answer = %q", answer)
	}
}

func TestQueuesUntilTheBrowserJoins(t *testing.T) {
	hub := NewHub()
	phoneBox := newChanBox()
	phone, err := hub.Register("ZXCVBN", "Phone", phoneBox)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := phoneBox.tryRecv(); !ok {
		t.Fatal("missing registered")
	}
	hub.Forward(phone, `{"type":"offer","sdp":"early"}`)
	browserBox := newChanBox()
	if _, err := hub.Join("ZXCVBN", browserBox); err != nil {
		t.Fatal(err)
	}
	joined, ok := browserBox.tryRecv()
	if !ok || !contains(joined, "joined") {
		t.Fatalf("joined = %q", joined)
	}
	offer, ok := browserBox.tryRecv()
	if !ok || offer != `{"type":"offer","sdp":"early"}` {
		t.Fatalf("offer = %q", offer)
	}
}

func TestRejectsEmptySerial(t *testing.T) {
	hub := NewHub()
	_, err := hub.Join("   ", newChanBox())
	if err == nil {
		t.Fatal("expected empty serial to fail")
	}
}

func contains(text, part string) bool {
	return strings.Contains(text, part)
}
