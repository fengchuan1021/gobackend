package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type unifiedBodyWriter struct {
	gin.ResponseWriter
	body   *bytes.Buffer
	status int
}

func (w *unifiedBodyWriter) WriteHeader(code int) {
	w.status = code
}

func (w *unifiedBodyWriter) WriteHeaderNow() {}

func (w *unifiedBodyWriter) Write(data []byte) (int, error) {
	return w.body.Write(data)
}

func (w *unifiedBodyWriter) WriteString(s string) (int, error) {
	return w.body.WriteString(s)
}

// UnifiedAPIResponse 统一 API 返回规范：
// 1) 所有 HTTP 状态码固定 200
// 2) JSON 体中的 code 仅允许 200(成功) / 500(失败)
// 3) msg 为错误消息或成功消息
func UnifiedAPIResponse() gin.HandlerFunc {
	return func(c *gin.Context) {
		bw := &unifiedBodyWriter{
			ResponseWriter: c.Writer,
			body:           bytes.NewBuffer(nil),
			status:         http.StatusOK,
		}
		c.Writer = bw
		c.Next()

		raw := bytes.TrimSpace(bw.body.Bytes())
		origin := bw.ResponseWriter

		if len(raw) == 0 {
			resp := gin.H{"code": 200, "msg": "ok"}
			if bw.status >= 400 {
				resp["code"] = 500
				resp["msg"] = http.StatusText(bw.status)
			}
			origin.WriteHeader(http.StatusOK)
			enc := json.NewEncoder(origin)
			_ = enc.Encode(resp)
			return
		}

		var obj map[string]interface{}
		if err := json.Unmarshal(raw, &obj); err == nil {
			normalized := normalizeAPIBody(obj, bw.status)
			origin.WriteHeader(http.StatusOK)
			enc := json.NewEncoder(origin)
			_ = enc.Encode(normalized)
			return
		}

		// 非 JSON 也统一返回 200，正文保持不变（如文本/二进制等）
		origin.WriteHeader(http.StatusOK)
		_, _ = origin.Write(raw)
	}
}

func normalizeAPIBody(obj map[string]interface{}, status int) map[string]interface{} {
	msg := strings.TrimSpace(toString(obj["msg"]))
	errMsg := strings.TrimSpace(toString(obj["error"]))
	codeVal, hasCode := toInt(obj["code"])

	success := status < 400
	if hasCode {
		success = success && (codeVal == 0 || codeVal == 200)
	}
	if errMsg != "" {
		success = false
	}

	if success {
		obj["code"] = 200
		if msg == "" {
			msg = "ok"
		}
	} else {
		obj["code"] = 500
		if msg == "" {
			if errMsg != "" {
				msg = errMsg
			} else if status >= 400 {
				msg = http.StatusText(status)
			} else {
				msg = "request failed"
			}
		}
	}

	obj["msg"] = msg
	delete(obj, "error")
	return obj
}

func toInt(v interface{}) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case float32:
		return int(t), true
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i), true
		}
		return 0, false
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
			return i, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}
