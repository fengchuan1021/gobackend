package handler

import (
	"fmt"
	"net/http"

	"gobackend/internal/udpserver"

	"github.com/gin-gonic/gin"
)

// CmdCallbackReq 命令回调请求体
type CmdCallbackReq struct {
	MsgID uint32 `json:"msg_id" binding:"required"`
	Data  string `json:"data"` // base64 或文本结果
}

// CmdCallback 客户端通过 HTTP 回调返回命令结果
// POST /api/udp/cmdcallback
func CmdCallback(c *gin.Context) {
	//var aes_req aes_utils.Aes_request
	var req CmdCallbackReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	// data, err := aes_utils.Decrypt(aes_req.Data)
	// if err != nil {
	// 	c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "decrypt failed"})
	// 	return
	// }
	// if err := json.Unmarshal([]byte(req.Data), &req); err != nil {
	// 	c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "unmarshal failed"})
	// 	return
	// }
	payload := []byte(req.Data)
	fmt.Println("req.MsgID", req.MsgID, "payload", string(payload))
	if udpserver.DeliverResult(req.MsgID, payload) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "ok"})
	} else {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "msg_id 无效或已超时"})
	}
}
