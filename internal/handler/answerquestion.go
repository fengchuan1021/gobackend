package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gobackend/config"
	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type AnswerQuestionReq struct {
	Question string   `json:"question"`
	Answers  []string `json:"answers"`
}

type deepSeekChatRequest struct {
	Model       string            `json:"model"`
	Messages    []deepSeekMessage `json:"messages"`
	Temperature float64           `json:"temperature"`
}

type deepSeekMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepSeekChatResponse struct {
	Choices []struct {
		Message deepSeekMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func AnswerQuestion(c *gin.Context) {
	var req AnswerQuestionReq
	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "参数错误"})
		return
	}
	_, exists := c.Get(middleware.UserIDKey)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	if req.Question == "" || len(req.Answers) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "题目或选项为空"})
		return
	}

	index, err := lookupCachedAnswer(req.Question, req.Answers)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	if index >= 0 {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"msg":  "ok",
			"data": gin.H{"index": index},
		})
		return
	}

	if config.Cfg.DeepSeek.APIKey == "" {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": "DeepSeek API Key 未配置"})
		return
	}

	index, err = askDeepSeek(req.Question, req.Answers)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	saveQuestionAnswer(req.Question, req.Answers, req.Answers[index])

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "ok",
		"data": gin.H{"index": index},
	})
}

func lookupCachedAnswer(question string, answers []string) (int, error) {
	var cached model.QuestionAnswer
	err := database.DB.Where("question = ?", question).First(&cached).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return -1, nil
	}
	if err != nil {
		return -1, fmt.Errorf("查询题库失败")
	}

	index := findAnswerIndex(answers, cached.RightAnswer)
	if index < 0 {
		return -1, nil
	}
	return index, nil
}

func findAnswerIndex(answers []string, rightAnswer string) int {
	for i, answer := range answers {
		if answer == rightAnswer {
			return i
		}
	}
	return -1
}

func saveQuestionAnswer(question string, answers []string, rightAnswer string) {
	record := model.QuestionAnswer{
		Question:    question,
		Answers:     answers,
		RightAnswer: rightAnswer,
	}
	database.DB.Where("question = ?", question).FirstOrCreate(&record)
}

func askDeepSeek(question string, answers []string) (int, error) {
	var options strings.Builder
	for i, answer := range answers {
		options.WriteString(fmt.Sprintf("%d. %s\n", i, answer))
	}

	prompt := fmt.Sprintf(`你是一个答题助手。根据题目和选项，给出正确答案的序号。
题目：%s
选项：
%s
请只回复正确选项的序号（从0开始），不要回复其他任何内容。`, question, options.String())

	body, err := json.Marshal(deepSeekChatRequest{
		Model: "deepseek-v4-flash",
		Messages: []deepSeekMessage{
			{Role: "user", Content: prompt},
		},
		Temperature: 0,
	})
	if err != nil {
		return 0, fmt.Errorf("构建请求失败")
	}

	httpReq, err := http.NewRequest(http.MethodPost, "https://api.deepseek.com/chat/completions", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("创建请求失败")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+config.Cfg.DeepSeek.APIKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("调用 DeepSeek 失败")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取 DeepSeek 响应失败")
	}

	var chatResp deepSeekChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return 0, fmt.Errorf("解析 DeepSeek 响应失败")
	}
	if chatResp.Error != nil {
		return 0, fmt.Errorf("DeepSeek 错误: %s", chatResp.Error.Message)
	}
	if len(chatResp.Choices) == 0 {
		return 0, fmt.Errorf("DeepSeek 未返回答案")
	}

	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	index, err := strconv.Atoi(content)
	if err != nil {
		for _, ch := range content {
			if ch >= '0' && ch <= '9' {
				index, err = strconv.Atoi(string(ch))
				if err == nil {
					break
				}
			}
		}
		if err != nil {
			return 0, fmt.Errorf("无法解析答案序号: %s", content)
		}
	}
	if index < 0 || index >= len(answers) {
		return 0, fmt.Errorf("答案序号超出范围: %d", index)
	}

	return index, nil
}
