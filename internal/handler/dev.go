package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/model"
	"gobackend/internal/scrcpyudp"
	"gobackend/internal/shellstream"
	"gobackend/internal/udpserver"

	"github.com/gin-gonic/gin"
)

const devScriptKeyPrefix = "dev_script:"
const devScriptTTL = 20 * time.Second

// GetDevices 从数据库获取设备列表
// GET /api/dev/getDevices
func GetDevices(c *gin.Context) {
	var list []model.Device
	if err := database.DB.Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取设备列表失败: " + err.Error()})
		return
	}
	devices := make([]gin.H, 0, len(list))
	for _, d := range list {
		devices = append(devices, gin.H{"serial": d.Serial})
	}
	c.JSON(http.StatusOK, gin.H{"data": devices})
}

// GetScreenShot 根据设备序列号获取截图
// GET /api/dev/getScreenShot?serial=xxx
// 通过 UDP 向设备发送截图命令，返回 PNG 图片流
func GetScreenShot(c *gin.Context) {
	serial := c.Query("serial")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serial 参数必填"})
		return
	}

	data, err := udpserver.SendCommand(serial, udpserver.CmdGetScreenshot, nil, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截图获取失败: " + err.Error()})
		return
	}

	if len(data) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截图为空"})
		return
	}

	// 客户端返回 base64 编码的 PNG
	pngData, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截图解码失败"})
		return
	}

	if len(pngData) == 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "截图为空"})
		return
	}

	c.Data(http.StatusOK, "image/png", pngData)
}

// RunDevScriptReq 执行设备脚本请求
type RunDevScriptReq struct {
	Serial string `json:"serial" binding:"required"`
	Script string `json:"script" binding:"required"`
}

// RunDevScript 在指定设备上执行脚本：生成 script_id 存 Redis（20s 过期），UDP 下发 script_id；设备凭 script_id HTTP 拉取脚本内容后执行
// POST /api/dev/runDevScript
func RunDevScript(c *gin.Context) {
	var req RunDevScriptReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fmt.Println("RunDevScript error:", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误，需 serial 与 script"})
		return
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		fmt.Println("RunDevScript error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成 script_id 失败"})
		return
	}
	scriptID := hex.EncodeToString(b)
	key := devScriptKeyPrefix + scriptID
	ctx := context.Background()
	if err := database.RDB.Set(ctx, key, "enablelog();\n"+req.Script, devScriptTTL).Err(); err != nil {
		fmt.Println("RunDevScript error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入脚本缓存失败"})
		return
	}

	data, err := udpserver.SendCommand(req.Serial, udpserver.CmdExecuteDevScript, []byte(scriptID), 0)
	if err != nil {
		fmt.Println("RunDevScript error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "执行脚本失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": string(data)})
}

// GetDevScriptContent 根据 script_id 从 Redis 返回脚本内容（设备拉取后执行），未找到或已过期返回 404
// GET /api/dev/getDevScriptContent/:id（可不鉴权，script_id 不可猜测且 20s 过期）
func GetDevScriptContent(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id 参数必填"})
		return
	}
	key := devScriptKeyPrefix + id
	ctx := context.Background()
	content, err := database.RDB.Get(ctx, key).Result()
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "脚本不存在或已过期"})
		return
	}
	body := []byte(content)
	c.Header("Content-Length", strconv.Itoa(len(body)))
	c.Data(http.StatusOK, "text/plain; charset=utf-8", body)
}

// GetXmlLayout 根据设备序列号获取 XML 布局
// GET /api/dev/getXmlLayout?serial=xxx
func GetXmlLayout(c *gin.Context) {
	serial := c.Query("serial")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serial 参数必填"})
		return
	}
	data, err := udpserver.SendCommand(serial, udpserver.CmdGetXmlLayout, nil, 0)
	if err != nil {
		fmt.Println("GetXmlLayout error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取 XML 布局失败: " + err.Error()})
		return
	}
	c.String(http.StatusOK, string(data))
}

// RunShellReq 在设备上以 root 执行一条 shell
type RunShellReq struct {
	Serial  string `json:"serial" binding:"required"`
	Command string `json:"command" binding:"required"`
}

// RunShell 通过 UDP ExecuteCommand 让 antares 执行命令并返回输出
// POST /api/dev/runShell
func RunShell(c *gin.Context) {
	var req RunShellReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误，需 serial 与 command"})
		return
	}
	if req.Serial == "" || req.Command == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serial 与 command 必填"})
		return
	}
	data, err := udpserver.SendCommand(req.Serial, udpserver.CmdExecuteCommand, []byte(req.Command), 0)
	if err != nil {
		fmt.Println("RunShell error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "执行命令失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": string(data)})
}

type RunShellSessionReq struct {
	Serial  string `json:"serial" binding:"required"`
	Session string `json:"session" binding:"required"`
	Op      string `json:"op" binding:"required"`
	Data    string `json:"data"`
	Seq     uint32 `json:"seq"`
}

// RunShellSession 通过 UDP ExecuteShellCommand 操作设备 PTY 会话
// POST /api/dev/shell
func RunShellSession(c *gin.Context) {
	var req RunShellSessionReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误，需 serial、session、op"})
		return
	}
	if req.Serial == "" || req.Session == "" || req.Op == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serial、session、op 必填"})
		return
	}
	payload, err := json.Marshal(map[string]interface{}{
		"op":      req.Op,
		"session": req.Session,
		"data":    req.Data,
		"seq":     req.Seq,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "编码失败"})
		return
	}
	data, err := udpserver.SendCommand(req.Serial, udpserver.CmdExecuteShellCommand, payload, 0)
	if err != nil {
		fmt.Println("RunShellSession error:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "执行失败: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": string(data)})
}

// ShellStream 订阅设备 PTY 输出（SSE）
// GET /api/dev/shellStream?serial=&session=
func ShellStream(c *gin.Context) {
	serial := c.Query("serial")
	session := c.Query("session")
	if serial == "" || session == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serial 与 session 必填"})
		return
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "不支持流式响应"})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
	flusher.Flush()

	ch := shellstream.Default.Subscribe(serial, session)
	defer shellstream.Default.Unsubscribe(serial, session, ch)

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := io.WriteString(c.Writer, "data: "+msg+"\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := io.WriteString(c.Writer, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func SendScrcpyCmd(c *gin.Context) {
	serial := c.Query("serial")
	cmdtype := c.Query("cmdtype")
	if serial == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "serial 参数必填"})
		return
	}
	if cmdtype == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cmdtype 参数必填"})
		return
	}
	if cmdtype == "beginScrcpy" {
		scrcpyudp.ExpectSerial(serial)
		data, err := udpserver.SendCommand(serial, udpserver.CmdBeginScrcpy, nil, 0)
		if err != nil {
			fmt.Println("BeginScrcpy error:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "开始 Scrcpy 失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": string(data)})
	}
	if cmdtype == "endScrcpy" {
		data, err := udpserver.SendCommand(serial, udpserver.CmdEndScrcpy, nil, 0)
		if err != nil {
			fmt.Println("EndScrcpy error:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "结束 Scrcpy 失败: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": string(data)})
	}

}
