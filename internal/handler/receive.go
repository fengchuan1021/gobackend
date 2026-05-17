package handler

import (
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Receive 接收 Slugger mirror 等客户端上报的 POST /receive
func Receive(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("[receive] read body failed: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "read body failed"})
		return
	}

	headers := map[string]string{
		"X-Slugger-Url":    c.GetHeader("X-Slugger-Url"),
		"X-Slugger-Method": c.GetHeader("X-Slugger-Method"),
		"X-Slugger-Code":   c.GetHeader("X-Slugger-Code"),
		"X-Slugger-Ts":     c.GetHeader("X-Slugger-Ts"),
		"Content-Type":     c.GetHeader("Content-Type"),
	}
	if headersJSON, err := json.Marshal(headers); err == nil {
		log.Printf("[receive] headers: %s", string(headersJSON))
	}

	if len(body) == 0 {
		log.Printf("[receive] json body: (empty)")
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Printf("[receive] body is not json (%d bytes): %s", len(body), string(body))
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	pretty, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		log.Printf("[receive] json body: %s", string(body))
	} else {
		log.Printf("[receive] json body:\n%s", string(pretty))
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
