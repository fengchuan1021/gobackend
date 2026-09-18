package shellstream

import (
	"sync"
)

type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan string]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan string]struct{})}
}

func subKey(serial, session string) string {
	return serial + "|" + session
}

func (h *Hub) Subscribe(serial, session string) chan string {
	ch := make(chan string, 256)
	key := subKey(serial, session)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.subs[key] == nil {
		h.subs[key] = make(map[chan string]struct{})
	}
	h.subs[key][ch] = struct{}{}
	return ch
}

func (h *Hub) Unsubscribe(serial, session string, ch chan string) {
	key := subKey(serial, session)
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.subs[key]
	if set == nil {
		return
	}
	if _, ok := set[ch]; ok {
		delete(set, ch)
		close(ch)
	}
	if len(set) == 0 {
		delete(h.subs, key)
	}
}

func (h *Hub) Publish(serial, session, msg string) {
	if serial == "" || session == "" || msg == "" {
		return
	}
	key := subKey(serial, session)
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[key] {
		select {
		case ch <- msg:
		default:
		}
	}
}

var Default = NewHub()
