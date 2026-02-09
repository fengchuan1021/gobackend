package devicelog

import (
	"sync"
)

const (
	MaxEntriesPerDevice = 5000
	DiscardCount        = 2500
)

// Entry 单条日志
type Entry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	At      string `json:"at"`
}

// Store 按 serial 存储设备日志，使用 map[string]chan Entry，每设备 channel 容量 5000
// 超过则丢弃最早 2500 条
type Store struct {
	mu   sync.RWMutex
	logs map[string]chan Entry
}

// NewStore 创建新 Store
func NewStore() *Store {
	return &Store{
		logs: make(map[string]chan Entry),
	}
}

func (s *Store) getOrCreate(serial string) chan Entry {
	if ch, ok := s.logs[serial]; ok {
		return ch
	}
	ch := make(chan Entry, MaxEntriesPerDevice)
	s.logs[serial] = ch
	return ch
}

// Append 追加日志
func (s *Store) Append(serial string, level, message, at string) {
	if serial == "" {
		return
	}
	s.mu.Lock()
	ch := s.getOrCreate(serial)
	s.mu.Unlock()

	entry := Entry{Level: level, Message: message, At: at}
	select {
	case ch <- entry:
		return
	default:
		// channel 已满，丢弃最早 2500 条
		for i := 0; i < DiscardCount; i++ {
			<-ch
		}
		ch <- entry
	}
}

// Get 获取某设备的日志（会清空该设备的 channel）
func (s *Store) Get(serial string) []Entry {
	s.mu.Lock()
	ch, ok := s.logs[serial]
	s.mu.Unlock()
	if !ok {
		return nil
	}

	var out []Entry
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

// Store 实例（供 handler 使用）
var DefaultStore = NewStore()
