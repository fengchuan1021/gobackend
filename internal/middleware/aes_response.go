package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"gobackend/internal/aes_utils"

	"github.com/gin-gonic/gin"
)

// aesRequestBody 请求体格式：{"data": encrypted_data}
type aesRequestBody struct {
	Data string `json:"data"`
}

// AesRequest 中间件：读取请求体 {"data": encrypted_data}，解密后替换为明文 JSON，供后续 handler 直接 Bind
func AesRequest(c *gin.Context) {
	if c.Request.Body == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "body required"})
		c.Abort()
		return
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "read body failed"})
		c.Abort()
		return
	}
	_ = c.Request.Body.Close()

	var wrap aesRequestBody
	if err := json.Unmarshal(raw, &wrap); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "invalid request format"})
		c.Abort()
		return
	}
	plain, err := aes_utils.Decrypt(wrap.Data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "decrypt failed"})
		c.Abort()
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewBufferString(plain))
	c.Next()
}

// aesResponseWriter 包装 gin.ResponseWriter，缓冲 body，便于在 Next 后整体 AES 加密再写出
type aesResponseWriter struct {
	gin.ResponseWriter
	body   *bytes.Buffer
	status int
}

func (w *aesResponseWriter) WriteHeader(code int) {
	w.status = code
}

func (w *aesResponseWriter) Write(b []byte) (int, error) {
	return w.body.Write(b)
}

// AesResponse 中间件：将 handler 返回的 JSON 体整体 AES 加密后，以 {"code":200,"data": encrypted_data} 返回
func AesResponse(c *gin.Context) {
	blw := &aesResponseWriter{
		ResponseWriter: c.Writer,
		body:           &bytes.Buffer{},
		status:         http.StatusOK,
	}
	c.Writer = blw
	c.Next()

	if blw.body.Len() == 0 {
		return
	}
	//fmt.Println(blw.body.String())
	encrypted, err := aes_utils.Encrypt(blw.body.String())
	if err != nil {
		c.Writer = blw.ResponseWriter
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "encrypt failed"})
		return
	}

	out := map[string]interface{}{
		"code": 200,
		"data": encrypted,
	}
	payload, _ := json.Marshal(out)
	blw.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
	blw.ResponseWriter.WriteHeader(http.StatusOK)
	_, _ = blw.ResponseWriter.Write(payload)
}
