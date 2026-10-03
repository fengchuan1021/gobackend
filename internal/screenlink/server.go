package screenlink

import (
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type inbox struct {
	mu     sync.Mutex
	queue  []string
	notify chan struct{}
	closed bool
}

func newInbox() *inbox {
	return &inbox{notify: make(chan struct{}, 1)}
}

func (in *inbox) Send(text string) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.closed {
		return false
	}
	in.queue = append(in.queue, text)
	select {
	case in.notify <- struct{}{}:
	default:
	}
	return true
}

func (in *inbox) Close() {
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.closed {
		return
	}
	in.closed = true
	select {
	case in.notify <- struct{}{}:
	default:
	}
}

func (in *inbox) recv() (string, bool) {
	for {
		in.mu.Lock()
		if len(in.queue) > 0 {
			text := in.queue[0]
			in.queue = in.queue[1:]
			in.mu.Unlock()
			return text, true
		}
		closed := in.closed
		in.mu.Unlock()
		if closed {
			return "", false
		}
		<-in.notify
	}
}

// Handle 接受手机 register 和浏览器 join。
func Handle(hub *Hub) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("screenlink 升级失败: %v", err)
			return
		}
		defer conn.Close()

		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Time{})
		if messageType != websocket.TextMessage {
			return
		}

		box := newInbox()
		client, err := hub.Accept(string(payload), box)
		if err != nil {
			log.Printf("screenlink %s %v", c.ClientIP(), err)
			return
		}
		side := "join"
		if client.Phone {
			side = "register"
		}
		log.Printf("screenlink %s %s %s", c.ClientIP(), side, client.Code)

		done := make(chan struct{})
		go func() {
			defer close(done)
			defer conn.Close()
			for {
				text, ok := box.recv()
				if !ok {
					return
				}
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if writeErr := conn.WriteMessage(websocket.TextMessage, []byte(text)); writeErr != nil {
					return
				}
			}
		}()
		defer func() {
			hub.Disconnect(client)
			box.Close()
			<-done
			log.Printf("screenlink %s left %s", c.ClientIP(), client.Code)
		}()

		for {
			messageType, payload, err = conn.ReadMessage()
			if err != nil {
				return
			}
			if messageType == websocket.TextMessage {
				hub.Forward(client, string(payload))
			}
		}
	}
}

// RunSTUN 在 SCREENLINK_STUN_ADDR（默认 0.0.0.0:3478）上应答 Binding 请求。
func RunSTUN() {
	addr := os.Getenv("SCREENLINK_STUN_ADDR")
	if addr == "" {
		addr = "0.0.0.0:3478"
	}
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Fatalf("screenlink stun 绑定失败: %v", err)
	}
	defer conn.Close()
	log.Printf("screenlink stun %s", addr)
	buf := make([]byte, 1500)
	for {
		n, src, err := conn.ReadFrom(buf)
		if err != nil {
			continue
		}
		packet := append([]byte(nil), buf[:n]...)
		udp, ok := src.(*net.UDPAddr)
		if !ok {
			continue
		}
		if response := Reply(packet, udp); response != nil {
			_, _ = conn.WriteTo(response, src)
		}
	}
}
