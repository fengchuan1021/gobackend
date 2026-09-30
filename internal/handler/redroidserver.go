package handler

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/middleware"
	"gobackend/internal/model"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

const redroidImage = "127.0.0.1:5000/redroid:latest"

var containerNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type containerActionReq struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type saveRedroidServerReq struct {
	ID         uint   `json:"id"`
	MachinedId string `json:"machined_id"`
	ExpireAt   string `json:"expire_at"`
	Note       string `json:"note"`
	Ip         string `json:"ip"`
}

type redroidContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	Running bool   `json:"running"`
}

type deleteRedroidServerReq struct {
	ID uint `json:"id" binding:"required"`
}

func currentUserID(c *gin.Context) (uint, bool) {
	value, exists := c.Get(middleware.UserIDKey)
	if !exists {
		return 0, false
	}
	uid, ok := value.(uint)
	return uid, ok
}

func parseExpireAt(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return &parsed, nil
		}
	}
	return nil, errors.New("invalid expire_at")
}

func isDuplicateKey(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "duplicate")
}

func ownedRedroidServer(uid, id uint) (model.RedroidServer, error) {
	var row model.RedroidServer
	err := database.DB.Where("id = ? AND user_id = ?", id, uid).First(&row).Error
	return row, err
}

// ListRedroidServers 当前用户的服务器列表
// GET /api/redroid_server/list
func ListRedroidServers(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	list := make([]model.RedroidServer, 0)
	if err := database.DB.Where("user_id = ?", uid).Order("id DESC").Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

// GetRedroidServer 查询单台服务器
// GET /api/redroid_server/get?id=
func GetRedroidServer(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	id, err := strconv.ParseUint(c.Query("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	row, err := ownedRedroidServer(uid, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "服务器不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": row})
}

// CreateRedroidServer 创建服务器
// POST /api/redroid_server/create
func CreateRedroidServer(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req saveRedroidServerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	machinedID := strings.TrimSpace(req.MachinedId)
	if machinedID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写机器ID"})
		return
	}
	expireAt, err := parseExpireAt(req.ExpireAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "到期时间格式错误"})
		return
	}

	ip, err := normalizeServerIP(req.Ip)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	row := model.RedroidServer{
		MachinedId: machinedID,
		UserID:     uid,
		ExpireAt:   expireAt,
		Note:       strings.TrimSpace(req.Note),
		Ip:         ip,
	}
	if err := database.DB.Create(&row).Error; err != nil {
		if isDuplicateKey(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "机器ID已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": row})
}

// UpdateRedroidServer 更新服务器
// POST /api/redroid_server/update
func UpdateRedroidServer(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req saveRedroidServerReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	machinedID := strings.TrimSpace(req.MachinedId)
	if machinedID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写机器ID"})
		return
	}
	expireAt, err := parseExpireAt(req.ExpireAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "到期时间格式错误"})
		return
	}
	ip, err := normalizeServerIP(req.Ip)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if _, err := ownedRedroidServer(uid, req.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "服务器不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}

	err = database.DB.Model(&model.RedroidServer{}).
		Where("id = ? AND user_id = ?", req.ID, uid).
		Updates(map[string]any{
			"machined_id": machinedID,
			"expire_at":   expireAt,
			"note":        strings.TrimSpace(req.Note),
			"ip":          ip,
		}).Error
	if err != nil {
		if isDuplicateKey(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "机器ID已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新失败"})
		return
	}

	row, err := ownedRedroidServer(uid, req.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": row})
}

// DeleteRedroidServer 删除服务器
// POST /api/redroid_server/delete
func DeleteRedroidServer(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req deleteRedroidServerReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	result := database.DB.Where("id = ? AND user_id = ?", req.ID, uid).Delete(&model.RedroidServer{})
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除失败"})
		return
	}
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "服务器不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0})
}

// ListRedroidServerContainers 通过 SSH 查询服务器上 redroid 镜像的容器
// GET /api/redroid_server/containers?id=
func ListRedroidServerContainers(c *gin.Context) {
	uid, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	id, err := strconv.ParseUint(c.Query("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}

	row, err := ownedRedroidServer(uid, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "服务器不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	if strings.TrimSpace(row.Ip) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未设置 IP"})
		return
	}

	list, err := listRedroidContainers(row.Ip)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": []redroidContainer{}, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

// StartRedroidContainer 在对应服务器上执行 redroid-start.sh
// POST /api/redroid_server/container/start
func StartRedroidContainer(c *gin.Context) {
	runRedroidContainerAction(c, "start")
}

// StopRedroidContainer 在对应服务器上执行 docker stop
// POST /api/redroid_server/container/stop
func StopRedroidContainer(c *gin.Context) {
	runRedroidContainerAction(c, "stop")
}

func runRedroidContainerAction(c *gin.Context, action string) {
	uid, ok := currentUserID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req containerActionReq
	if err := c.ShouldBindJSON(&req); err != nil || req.ID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	name := strings.TrimSpace(req.Name)
	if !containerNamePattern.MatchString(name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "容器名称无效"})
		return
	}

	row, err := ownedRedroidServer(uid, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "服务器不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败"})
		return
	}
	if strings.TrimSpace(row.Ip) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未设置 IP"})
		return
	}

	if err := execRedroidContainerAction(row.Ip, name, action); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0})
}

func normalizeServerIP(raw string) (string, error) {
	ip := strings.TrimSpace(raw)
	if ip == "" {
		return "", nil
	}
	if strings.ContainsAny(ip, " \t\r\n") || len(ip) > 128 {
		return "", errors.New("IP 格式错误")
	}
	return ip, nil
}

func sshDialAddr(ip string) (string, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "", errors.New("未设置 IP")
	}
	if host, port, err := net.SplitHostPort(ip); err == nil && host != "" && port != "" {
		return ip, nil
	}
	return net.JoinHostPort(ip, "22"), nil
}

func redroidSSHAuthMethods() ([]ssh.AuthMethod, error) {
	methods := make([]ssh.AuthMethod, 0, 2)
	keyPath := os.Getenv("REDROID_SSH_KEY")
	if keyPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			keyPath = filepath.Join(home, ".ssh", "id_rsa")
		}
	}
	if keyPath != "" {
		if raw, err := os.ReadFile(keyPath); err == nil {
			if signer, err := ssh.ParsePrivateKey(raw); err == nil {
				methods = append(methods, ssh.PublicKeys(signer))
			}
		}
	}
	if password := os.Getenv("REDROID_SSH_PASSWORD"); password != "" {
		methods = append(methods, ssh.Password(password))
	}
	if len(methods) == 0 {
		return nil, errors.New("未配置可用的 SSH 登录方式")
	}
	return methods, nil
}

func dialRedroidSSH(ip string) (*ssh.Client, error) {
	addr, err := sshDialAddr(ip)
	if err != nil {
		return nil, err
	}
	auth, err := redroidSSHAuthMethods()
	if err != nil {
		return nil, err
	}
	user := os.Getenv("REDROID_SSH_USER")
	if user == "" {
		user = "root"
	}
	client, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            user,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         8 * time.Second,
	})
	if err != nil {
		return nil, errors.New("SSH 连接失败")
	}
	return client, nil
}

func runSSH(client *ssh.Client, cmd string, timeout time.Duration) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", errors.New("SSH 会话创建失败")
	}
	defer session.Close()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()
	select {
	case err = <-done:
	case <-time.After(timeout):
		_ = session.Close()
		return "", errors.New("命令执行超时")
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = "命令执行失败"
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return "", errors.New(msg)
	}
	return stdout.String(), nil
}

func listRedroidContainers(ip string) ([]redroidContainer, error) {
	client, err := dialRedroidSSH(ip)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	cmd := "docker ps -a --filter 'ancestor=" + redroidImage + "' --format '{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.State}}'"
	output, err := runSSH(client, cmd, 15*time.Second)
	if err != nil {
		return nil, err
	}
	return parseRedroidContainers(output), nil
}

func execRedroidContainerAction(ip, name, action string) error {
	client, err := dialRedroidSSH(ip)
	if err != nil {
		return err
	}
	defer client.Close()

	listCmd := "docker ps -a --filter 'ancestor=" + redroidImage + "' --format '{{.Names}}'"
	output, err := runSSH(client, listCmd, 15*time.Second)
	if err != nil {
		return err
	}
	if !containerNameListed(output, name) {
		return errors.New("容器不存在")
	}

	quoted := "'" + name + "'"
	cmd := "docker stop " + quoted
	timeout := 60 * time.Second
	if action == "start" {
		cmd = "/opt/redroid/redroid-start.sh " + quoted
		timeout = 10 * time.Minute
	}
	if _, err := runSSH(client, cmd, timeout); err != nil {
		return err
	}
	return nil
}

func containerNameListed(output, name string) bool {
	for _, line := range strings.Split(output, "\n") {
		for _, item := range strings.Split(line, ",") {
			if strings.TrimSpace(item) == name {
				return true
			}
		}
	}
	return false
}

func parseRedroidContainers(output string) []redroidContainer {
	list := make([]redroidContainer, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 5 {
			continue
		}
		item := redroidContainer{
			ID:      parts[0],
			Name:    parts[1],
			Image:   parts[2],
			Status:  parts[3],
			Running: parts[4] == "running",
		}
		list = append(list, item)
		if len(list) >= 200 {
			break
		}
	}
	return list
}
