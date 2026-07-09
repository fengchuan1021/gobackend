package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type DeepSeekReq struct {
	Prompt string `json:"prompt"`
	APIKey string `json:"apikey"`
}

func DeepSeek(c *gin.Context) {
	var req DeepSeekReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "prompt is required"})
		return
	}

	apikey := strings.TrimSpace(req.APIKey)
	if apikey == "" {
		apikey = strings.TrimSpace(c.GetHeader("apikey"))
	}

	if apikey == "" {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "apikey is required"})
		return
	}

	content, err := callDeepSeekChat(apikey, req.Prompt, 1.0)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": content,
	})
}

func callDeepSeekChat(apiKey, prompt string, temperature float64) (string, error) {
	body, err := json.Marshal(deepSeekChatRequest{
		Model: "deepseek-v4-flash",
		Messages: []deepSeekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: temperature,
	})
	if err != nil {
		return "", fmt.Errorf("构建请求失败")
	}

	httpReq, err := http.NewRequest(http.MethodPost, "https://api.deepseek.com/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("创建请求失败")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("调用 DeepSeek 失败")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取 DeepSeek 响应失败")
	}

	var chatResp deepSeekChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", fmt.Errorf("解析 DeepSeek 响应失败")
	}
	if chatResp.Error != nil {
		return "", fmt.Errorf("DeepSeek 错误: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("DeepSeek 未返回内容")
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("DeepSeek 返回内容为空")
	}
	return content, nil
}
