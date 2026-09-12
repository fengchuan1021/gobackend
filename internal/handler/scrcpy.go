package handler

import (
	"log"
	"net/http"
	"time"

	"gobackend/internal/scrcpyudp"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var scrcpyUpgrader = websocket.Upgrader{
	ReadBufferSize:  4 * 1024,
	WriteBufferSize: 256 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ScrcpyWs 将指定设备的 scrcpy UDP 视频帧以二进制 WebSocket 转发给网页
// GET /api/dev/scrcpyWs?serial=xxx
func ScrcpyWs(c *gin.Context) {
	serial := c.Query("serial")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serial 参数必填"})
		return
	}

	conn, err := scrcpyUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("scrcpy ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	conn.SetReadLimit(64 * 1024)
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	})

	for _, pkt := range scrcpyudp.Cached(serial) {
		if err := writeBinary(conn, pkt); err != nil {
			return
		}
	}

	sub := scrcpyudp.Subscribe(serial)
	defer scrcpyudp.Unsubscribe(sub)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-done:
			return
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		case data, ok := <-sub.C():
			if !ok {
				return
			}
			if err := writeBinary(conn, data); err != nil {
				return
			}
		}
	}
}

func writeBinary(conn *websocket.Conn, data []byte) error {
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return conn.WriteMessage(websocket.BinaryMessage, data)
}
