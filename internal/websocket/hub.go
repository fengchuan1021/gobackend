package websocket

import (
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// Client 表示一个 WebSocket 连接
type Client struct {
	hub           *Hub
	conn          *websocket.Conn
	send          chan []byte
	monitorSerial string
	monitorMu     sync.RWMutex
}

// Hub 管理所有 WebSocket 连接
type Hub struct {
	clients    map[*Client]struct{}
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	mu         sync.RWMutex
}

// NewHub 创建新的 Hub
func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]struct{}),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan []byte, 256),
	}
}

// Run 启动 Hub 主循环
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = struct{}{}
			h.mu.Unlock()
			log.Printf("WebSocket 客户端连接，当前连接数: %d", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

// SetMonitorSerial 设置客户端要监控的设备 serial
func (c *Client) SetMonitorSerial(serial string) {
	c.monitorMu.Lock()
	defer c.monitorMu.Unlock()
	c.monitorSerial = serial
}

// GetMonitorSerial 获取客户端监控的 serial
func (c *Client) GetMonitorSerial() string {
	c.monitorMu.RLock()
	defer c.monitorMu.RUnlock()
	return c.monitorSerial
}

// DefaultHub 默认 Hub 实例，main 中设置
var DefaultHub *Hub

// BroadcastToMonitor 向所有监控指定 serial 的客户端发送消息
func (h *Hub) BroadcastToMonitor(serial string, data []byte) {
	if serial == "" {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		if client.GetMonitorSerial() == serial {
			select {
			case client.send <- data:
			default:
				// 发送缓冲区满，跳过
			}
		}
	}
}
