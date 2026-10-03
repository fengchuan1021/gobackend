package screenlink

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	maxQueue    = 32
	maxMessage  = 256 * 1024
	sessionTTL  = 5 * time.Minute
	maxSerial   = 320
	maxNameRune = 64
)

// Sender 把信令文本送到一侧连接。Send 不得阻塞。
type Sender interface {
	Send(text string) bool
}

// Client 是已经登记或加入的一侧。
type Client struct {
	Code   string
	Phone  bool
	sender Sender
}

type session struct {
	name      string
	phone     Sender
	browser   Sender
	toPhone   []string
	toBrowser []string
	touched   time.Time
}

// Hub 按设备序列号配对手机和浏览器，并转发 WebRTC 信令。
type Hub struct {
	mu       sync.Mutex
	sessions map[string]*session
}

func NewHub() *Hub {
	return &Hub{sessions: map[string]*session{}}
}

func (h *Hub) RunSweeper() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for now := range tick.C {
		h.Expire(now)
	}
}

func (h *Hub) Accept(text string, tx Sender) (Client, error) {
	kind, code, name, ok := classify(text)
	if !ok {
		return Client{}, errors.New("第一条消息必须是 register 或 join")
	}
	if kind == "register" {
		return h.Register(code, name, tx)
	}
	return h.Join(code, tx)
}

func (h *Hub) Register(code, name string, tx Sender) (Client, error) {
	serial, err := normalizeSerial(code)
	if err != nil {
		return Client{}, err
	}
	name = sanitizeName(name)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropSide(serial, true)
	current := h.session(serial)
	current.name = name
	current.phone = tx
	current.touched = time.Now()
	browserReady := current.browser != nil
	tx.Send(mustJSON(map[string]string{"type": "registered", "code": serial}))
	if browserReady {
		h.pair(serial)
	}
	return Client{Code: serial, Phone: true, sender: tx}, nil
}

func (h *Hub) Join(code string, tx Sender) (Client, error) {
	serial, err := normalizeSerial(code)
	if err != nil {
		return Client{}, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropSide(serial, false)
	current := h.session(serial)
	current.browser = tx
	current.touched = time.Now()
	if current.phone != nil {
		h.pair(serial)
	} else {
		tx.Send(mustJSON(map[string]string{"type": "waiting", "code": serial}))
	}
	return Client{Code: serial, Phone: false, sender: tx}, nil
}

func (h *Hub) Forward(client Client, text string) {
	if len(text) > maxMessage {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.sessions[client.Code]
	if current == nil {
		return
	}
	current.touched = time.Now()
	if client.Phone {
		if current.phone != client.sender {
			return
		}
		if current.browser != nil {
			current.browser.Send(text)
		} else {
			current.toBrowser = pushQueue(current.toBrowser, text)
		}
		return
	}
	if current.browser != client.sender {
		return
	}
	if current.phone != nil {
		current.phone.Send(text)
	} else {
		current.toPhone = pushQueue(current.toPhone, text)
	}
}

func (h *Hub) Disconnect(client Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.sessions[client.Code]
	if current == nil {
		return
	}
	notice := mustJSON(map[string]string{"type": "peer-left"})
	if client.Phone {
		if current.phone != client.sender {
			return
		}
		current.phone = nil
		current.toPhone = nil
		if current.browser != nil {
			current.browser.Send(notice)
		}
	} else {
		if current.browser != client.sender {
			return
		}
		current.browser = nil
		current.toBrowser = nil
		if current.phone != nil {
			current.phone.Send(notice)
		}
	}
	if current.phone == nil && current.browser == nil {
		delete(h.sessions, client.Code)
	}
}

func (h *Hub) Expire(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for code, current := range h.sessions {
		paired := current.phone != nil && current.browser != nil
		if paired || now.Sub(current.touched) < sessionTTL {
			continue
		}
		delete(h.sessions, code)
	}
}

func (h *Hub) session(code string) *session {
	current := h.sessions[code]
	if current == nil {
		current = &session{touched: time.Now()}
		h.sessions[code] = current
	}
	return current
}

func (h *Hub) pair(code string) {
	current := h.sessions[code]
	if current == nil {
		return
	}
	joined := mustJSON(map[string]string{"type": "joined", "code": code, "name": current.name})
	peer := mustJSON(map[string]string{"type": "peer", "code": code})
	if current.browser != nil {
		current.browser.Send(joined)
	}
	if current.phone != nil {
		current.phone.Send(peer)
	}
	toBrowser := current.toBrowser
	toPhone := current.toPhone
	current.toBrowser = nil
	current.toPhone = nil
	if current.browser != nil {
		for _, message := range toBrowser {
			current.browser.Send(message)
		}
	}
	if current.phone != nil {
		for _, message := range toPhone {
			current.phone.Send(message)
		}
	}
}

func (h *Hub) dropSide(code string, phone bool) {
	current := h.sessions[code]
	if current == nil {
		return
	}
	notice := mustJSON(map[string]string{"type": "peer-left"})
	if phone {
		if current.phone == nil {
			return
		}
		current.phone.Send(notice)
		current.phone = nil
		current.toPhone = nil
		if current.browser != nil {
			current.browser.Send(notice)
		}
		return
	}
	if current.browser == nil {
		return
	}
	current.browser.Send(notice)
	current.browser = nil
	current.toBrowser = nil
	if current.phone != nil {
		current.phone.Send(notice)
	}
}

func classify(text string) (kind, code, name string, ok bool) {
	var value map[string]any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		return "", "", "", false
	}
	kind, _ = value["type"].(string)
	code, _ = value["code"].(string)
	name, _ = value["name"].(string)
	if code == "" || (kind != "register" && kind != "join") {
		return "", "", "", false
	}
	return kind, code, name, true
}

func normalizeSerial(raw string) (string, error) {
	serial := strings.TrimSpace(raw)
	if serial == "" || len(serial) > maxSerial {
		return "", errors.New("设备序列号无效")
	}
	return serial, nil
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "phone"
	}
	runes := []rune(name)
	if len(runes) > maxNameRune {
		runes = runes[:maxNameRune]
	}
	return string(runes)
}

func pushQueue(queue []string, text string) []string {
	if len(queue) == maxQueue {
		queue = queue[1:]
	}
	return append(queue, text)
}

func mustJSON(value map[string]string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `{"type":"error"}`
	}
	return string(encoded)
}
