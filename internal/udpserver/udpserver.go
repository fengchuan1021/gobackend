package udpserver

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand/v2"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gobackend/internal/database"
	"gobackend/internal/model"

	"github.com/redis/go-redis/v9"
)

const (
	heartbeatWorkerCount = 8
	heartbeatQueueSize   = 256

	clientStaleTimeout       = 40 * time.Second
	clientStaleSweepInterval = 1 * time.Minute
)

const (
	Magic               = 0x53434F52 // "SCOR"
	HeaderSize          = 16
	CmdHeartbeat        = 0
	CmdGetScreenshot    = 1
	CmdGetXmlLayout     = 2
	CmdSetToken         = 3
	CmdExecuteCommand   = 4
	CmdAck              = 5
	CmdExecuteDevScript = 6
	CmdRunTaskScript    = 7
	CmdStopTask         = 8
	CmdBackupApps       = 9
	CmdResetDevice      = 10
)

type ConnInfo struct {
	Conn          *net.UDPAddr
	Ip            string
	LastHeartbeat time.Time
}

const (
	OnlineDevicePrefix = "onlinedevice:"
	UserIpDeviceHash   = "user:"
	SerialIPKeyPrefix  = "serial-ip:user:"

	maxDevicesPerIPCacheKeyFmt = "udpserver:maxdevicesperip:%d"
	planTaskCheckCacheKeyFmt   = "udpserver:plantaskcheck:%d"
)

// planTaskCheckTTL checkPlanTask 同一设备的最小重判间隔
const planTaskCheckTTL = 20 * time.Second

func maxDevicesPerIpCacheTTL(limit int) time.Duration {
	if limit == 0 {
		return 5 * time.Minute
	}
	return 10 * time.Minute
}

func redisRunningKey(userID uint, ip string) string {
	return fmt.Sprintf("%s%d:ip:%s", UserIpDeviceHash, userID, ip)
}

func redisSerialIPKey(userID uint) string {
	return fmt.Sprintf("%s%d", SerialIPKeyPrefix, userID)
}

func upsertRunningDeviceInRedis(ctx context.Context, userID uint, ip, serial string) error {
	if userID == 0 || ip == "" || serial == "" {
		return nil
	}
	ipMapKey := redisSerialIPKey(userID)
	// oldIP, err := database.RDB.HGet(ctx, ipMapKey, serial).Result()
	// if err != nil && !errors.Is(err, redis.Nil) {
	// 	return err
	// }
	// if oldIP != "" && oldIP != ip {
	// 	if err := database.RDB.ZRem(ctx, redisRunningKey(userID, oldIP), serial).Err(); err != nil {
	// 		return err
	// 	}
	// }
	if err := database.RDB.HSetEXWithArgs(ctx, ipMapKey, &redis.HSetEXOptions{
		ExpirationType: redis.HSetEXExpirationEX,
		ExpirationVal:  int64(clientStaleTimeout / time.Second),
	}, serial, ip).Err(); err != nil {
		return err
	}
	expireAt := time.Now().Add(clientStaleTimeout).Unix()
	if err := database.RDB.ZAdd(ctx, redisRunningKey(userID, ip), redis.Z{
		Score:  float64(expireAt),
		Member: serial,
	}).Err(); err != nil {
		return err
	}
	return database.RDB.Expire(ctx, redisRunningKey(userID, ip), clientStaleTimeout).Err()
}

func removeRunningDeviceFromRedis(ctx context.Context, userID uint, ip, serial string) error {
	return nil
	// if userID == 0 || serial == "" {
	// 	return nil
	// }
	// ipMapKey := redisSerialIPKey(userID)
	// oldIP, err := database.RDB.HGet(ctx, ipMapKey, serial).Result()
	// if err != nil && !errors.Is(err, redis.Nil) {
	// 	return err
	// }
	// targetIP := ip
	// if oldIP != "" {
	// 	targetIP = oldIP
	// }
	// if targetIP != "" {
	// 	if err := database.RDB.ZRem(ctx, redisRunningKey(userID, targetIP), serial).Err(); err != nil {
	// 		return err
	// 	}
	// }
	// return database.RDB.HDel(ctx, ipMapKey, serial).Err()
}

// RunningTaskDeviceCount 返回指定 userID + ip 下、未过期的运行中设备数量。
func RunningTaskDeviceCount(ctx context.Context, userID uint, ip string) (int64, error) {
	if userID == 0 || ip == "" {
		return 0, nil
	}
	key := redisRunningKey(userID, ip)
	nowUnix := time.Now().Unix()
	if err := database.RDB.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(nowUnix, 10)).Err(); err != nil {
		return 0, err
	}
	return database.RDB.ZCard(ctx, key).Result()
}

var (
	clients   = make(map[string]*ConnInfo)
	clientsMu sync.RWMutex
	conn      *net.UDPConn
	connMu    sync.RWMutex
	pending   sync.Map // msgID (uint32) -> chan []byte
	nextMsgID uint32   = 1

	heartbeatCh chan heartbeatJob
	heartbeatWG sync.WaitGroup
)

type heartbeatJob struct {
	uid      uint
	serial   string
	hasTask  uint32
	scriptID uint
	from     *net.UDPAddr
}

func parsePacket(buf []byte) (magic uint32, length uint32, cmdType uint32, messageID uint32, payload []byte, ok bool) {
	if len(buf) < HeaderSize {
		return 0, 0, 0, 0, nil, false
	}
	magic = binary.LittleEndian.Uint32(buf[0:4])
	length = binary.LittleEndian.Uint32(buf[4:8])
	cmdType = binary.LittleEndian.Uint32(buf[8:12])
	messageID = binary.LittleEndian.Uint32(buf[12:16])
	if magic != Magic || length < HeaderSize || int(length) > len(buf) {
		return 0, 0, 0, 0, nil, false
	}
	if length > HeaderSize {
		payload = buf[HeaderSize:length]
	}
	return magic, length, cmdType, messageID, payload, true
}

func buildPacket(cmdType uint32, messageID uint32, payload []byte) []byte {
	plen := len(payload)
	buf := make([]byte, HeaderSize+plen)
	binary.LittleEndian.PutUint32(buf[0:4], Magic)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(HeaderSize+plen))
	binary.LittleEndian.PutUint32(buf[8:12], cmdType)
	binary.LittleEndian.PutUint32(buf[12:16], messageID)
	if plen > 0 {
		copy(buf[HeaderSize:], payload)
	}
	return buf
}

// heartbeatAckPacket 即 buildPacket(CmdHeartbeat, 0, nil)，只分配一次供心跳回复复用
var heartbeatAckPacket = buildPacket(CmdHeartbeat, 0, nil)

// NextMsgID 获取下一个消息 ID
func NextMsgID() uint32 {
	return atomic.AddUint32(&nextMsgID, 1)
}

func cloneUDPAddr(a *net.UDPAddr) *net.UDPAddr {
	if a == nil {
		return nil
	}
	b := *a
	if len(a.IP) > 0 {
		b.IP = append(net.IP(nil), a.IP...)
	}
	return &b
}

func registerHeartbeatClient(job *heartbeatJob) {
	if job.serial == "" {
		return
	}
	fmt.Printf("registerHeartbeatClient serial=%s ip=%s\n", job.serial, job.from.IP.String())
	ctx := context.Background()
	if err := database.RDB.Set(ctx, OnlineDevicePrefix+job.serial, job.from.IP.String(), clientStaleTimeout).Err(); err != nil {
		log.Printf("set online device ttl failed serial=%s err=%v", job.serial, err)
	}
	clientsMu.Lock()
	if ci, ok := clients[job.serial]; ok {
		ci.Conn = job.from
		ci.Ip = job.from.IP.String()
		ci.LastHeartbeat = time.Now()
	} else {
		clients[job.serial] = &ConnInfo{
			Conn:          job.from,
			Ip:            job.from.IP.String(),
			LastHeartbeat: time.Now(),
		}
	}
	clientsMu.Unlock()
}

// pruneStaleClients 删除 LastHeartbeat 早于 clientStaleTimeout 的在线记录
func pruneStaleClients() {
	now := time.Now()
	clientsMu.Lock()
	for serial, ci := range clients {
		if ci == nil || now.Sub(ci.LastHeartbeat) > clientStaleTimeout {
			delete(clients, serial)
		}
	}
	clientsMu.Unlock()
}

func staleClientCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(clientStaleSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pruneStaleClients()
		}
	}
}
func UpdateMaxDevicesPerIp(userID uint, limit int) {
	if userID == 0 {
		return
	}
	ctx := context.Background()
	cacheKey := fmt.Sprintf(maxDevicesPerIPCacheKeyFmt, userID)
	database.RDB.Set(ctx, cacheKey, strconv.Itoa(limit), maxDevicesPerIpCacheTTL(limit))
	if err := database.RDB.Set(ctx, cacheKey, strconv.Itoa(limit), maxDevicesPerIpCacheTTL(limit)).Err(); err != nil {
		log.Printf("set max devices per ip failed userID=%d limit=%d err=%v", userID, limit, err)
	}

}
func getMaxDevicesPerIp(userID uint) int {
	if userID == 0 {
		return 0
	}
	ctx := context.Background()
	cacheKey := fmt.Sprintf(maxDevicesPerIPCacheKeyFmt, userID)
	if database.RDB != nil {
		if s, err := database.RDB.Get(ctx, cacheKey).Result(); err == nil && s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				return n
			}
		}
	}
	var user model.User
	if err := database.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		return 0
	}
	limit := user.MaxDevicesPerIp
	if database.RDB != nil {
		_ = database.RDB.Set(ctx, cacheKey, strconv.Itoa(limit), maxDevicesPerIpCacheTTL(limit)).Err()
	}
	return limit
}

// shouldRunCheckPlanTask 判断本次心跳是否需要再跑一次 checkPlanTask；
// 同一设备 planTaskCheckTTL 内只判断一次（基于 redis SetNX 去重）。
// 当 redis 不可用或调用出错时，回退为允许执行，避免影响主流程。
func shouldRunCheckPlanTask(ctx context.Context, serial string) bool {
	if serial == "" {
		return true
	}
	if database.RDB == nil {
		return true
	}
	//key := fmt.Sprintf(planTaskCheckCacheKeyFmt, serial)
	ok, err := database.RDB.SetNX(ctx, serial, "1", planTaskCheckTTL).Result()
	if err != nil {
		return true
	}
	return ok
}

// checkPlanTask 在设备空闲时为它生成今日还未达到额度的计划任务对应的 model.Task 行；
// 实际下发由后续 maybeRunPendingTaskFromHeartbeat 中的 SendCommand 处理。
func checkPlanTask(device *model.Device) {

	if device == nil || device.ID == 0 {
		fmt.Printf("checkPlanTask device is nil or id is 0\n")
		return
	}

	// 已有未完成的任务排队/运行，跳过本次生成，避免重复堆积
	var pendingCount int64
	if err := database.DB.Model(&model.Task{}).
		Where("device_id = ? AND status = ? ",
			device.ID,
			model.TaskStatusNotStarted,
		).
		Count(&pendingCount).Error; err != nil {
		return
	}
	if pendingCount > 0 {
		fmt.Printf("checkPlanTask pendingCount=%d\n", pendingCount)
		return
	}

	// 1. 该设备绑定的计划任务
	var devicePlanTasks []model.DevicePlanTask
	if err := database.DB.
		Where("device_id = ? AND user_id = ?", device.ID, device.UserID).
		Find(&devicePlanTasks).Error; err != nil {
		fmt.Printf("checkPlanTask get devicePlanTasks failed err=%v\n", err)
		return
	}
	if len(devicePlanTasks) == 0 {
		fmt.Printf("checkPlanTask devicePlanTasks is empty\n")
		return
	}
	planTaskIDs := make([]uint, 0, len(devicePlanTasks))
	for _, dpt := range devicePlanTasks {
		planTaskIDs = append(planTaskIDs, dpt.PlanTaskID)
	}

	var planTasks []model.PlanTask
	if err := database.DB.
		Where("id IN (?) AND user_id = ?", planTaskIDs, device.UserID).
		Find(&planTasks).Error; err != nil {
		fmt.Printf("checkPlanTask get planTasks failed err=%v\n", err)
		return
	}
	if len(planTasks) == 0 {
		fmt.Printf("checkPlanTask planTasks is empty\n")
		return
	}

	// 2. 一次拉取所有相关条目，按 plan_task_id 归类
	var allItems []model.PlanTaskItem
	if err := database.DB.
		Where("plan_task_id IN (?)", planTaskIDs).
		Order("id ASC").
		Find(&allItems).Error; err != nil {
		fmt.Printf("checkPlanTask get allItems failed err=%v\n", err)
		return
	}
	itemsByPlan := make(map[uint][]model.PlanTaskItem, len(planTasks))
	for _, it := range allItems {
		itemsByPlan[it.PlanTaskID] = append(itemsByPlan[it.PlanTaskID], it)
	}

	// 3. 今天 0 点起，该设备已执行任务按 script_id 累加分钟数
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	type execRow struct {
		ScriptID        uint
		ExecutedMinutes int
	}
	var execRows []execRow
	if err := database.DB.Model(&model.Task{}).
		Select("script_id AS script_id, COALESCE(SUM(total_minutes), 0) AS executed_minutes").
		Where("device_id = ? AND status!=4 AND created_at >= ?", device.ID, todayStart).
		Group("script_id").
		Scan(&execRows).Error; err != nil {
		fmt.Printf("checkPlanTask get execRows failed err=%v\n", err)
		return
	}

	executedByScript := make(map[uint]int, len(execRows))
	for _, r := range execRows {
		executedByScript[r.ScriptID] = r.ExecutedMinutes
	}

	// 4. 遍历每个计划任务及其条目，按需创建 Task 行
	for _, pt := range planTasks {
		items := itemsByPlan[pt.ID]
		if len(items) == 0 {
			continue
		}
		if pt.ExecutionOrder == model.PlanTaskExecutionOrderRandom {
			rand.Shuffle(len(items), func(i, j int) {
				items[i], items[j] = items[j], items[i]
			})
		}
		for _, item := range items {
			if item.ScriptID == 0 {
				continue
			}
			duration := item.DurationMinute
			if duration <= 0 {
				duration = 40
			}
			round := item.TotalRound
			if round <= 0 {
				round = 1
			}
			required := round * duration
			executed := executedByScript[item.ScriptID]
			if executed >= required {
				continue
			}
			if pt.IsTimedTrigger {
				parsed, err := time.ParseInLocation("15:04", strings.TrimSpace(item.StartTime), now.Location())
				if err != nil {
					continue
				}
				startMoment := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
				if now.Before(startMoment) {
					continue
				}
			}
			task := model.Task{
				UserID:         device.UserID,
				DeviceID:       device.ID,
				DeviceSerial:   device.Serial,
				ScriptID:       item.ScriptID,
				Args:           item.Args,
				StartTime:      nil,
				EndTime:        nil,
				TotalMinutes:   duration,
				TotalRound:     round,
				LeftRound:      round,
				LeftMinute:     duration,
				Status:         model.TaskStatusNotStarted,
				PlanTaskID:     int(pt.ID),
				PlanTaskItemID: int(item.ID),
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if err := database.DB.Create(&task).Error; err != nil {
				log.Printf("create plan task row failed device=%s script=%d err=%v", device.Serial, item.ScriptID, err)
				continue
			}
			// 把刚刚入队的执行时长计入，避免同一脚本被本轮循环重复入队
			executedByScript[item.ScriptID] = executed + duration
		}
	}
}

// maybeRunPendingTaskFromHeartbeat 在设备空闲心跳时检查是否有待运行任务并下发
func maybeRunPendingTaskFromHeartbeat(job *heartbeatJob) {
	ctx := context.Background()
	if job.hasTask != 0 {
		if err := upsertRunningDeviceInRedis(ctx, job.uid, job.from.IP.String(), job.serial); err != nil {
			log.Printf("upsert running device failed uid=%d serial=%s ip=%s err=%v", job.uid, job.serial, job.from.IP.String(), err)
		}
		return
	} else {
		// if err := removeRunningDeviceFromRedis(ctx, job.uid, job.from.IP.String(), job.serial); err != nil {
		// 	log.Printf("remove running device failed uid=%d serial=%s ip=%s err=%v", job.uid, job.serial, job.from.IP.String(), err)
		// }
	}

	n := getMaxDevicesPerIp(job.uid)
	if n > 0 {
		count, err := RunningTaskDeviceCount(ctx, job.uid, job.from.IP.String())
		if err != nil {
			log.Printf("get running task device count failed uid=%d ip=%s err=%v", job.uid, job.from.IP.String(), err)
		}
		if count >= int64(n) {
			return
		}
	}

	//check plan task（同一设备 20s 内只判断一次）
	if shouldRunCheckPlanTask(ctx, job.serial) {
		var device model.Device
		if err := database.DB.Where("serial = ?", job.serial).First(&device).Error; err != nil {

			log.Printf("get device failed serial=%s err=%v", job.serial, err)
			return
		}
		if device.ExpireAt != nil && device.ExpireAt.Before(time.Now()) {
			return
		}
		checkPlanTask(&device)
		var newTask model.Task
		now := time.Now()
		if err := database.DB.Where(
			"device_serial = ? and (status=0 or status=6) and (on_hold_end_time IS NULL OR on_hold_end_time > ?)",
			job.serial, now,
		).First(&newTask).Error; err != nil {
			return
		}
		if newTask.ID != 0 {
			go SendCommand(job.serial, CmdRunTaskScript, []byte(strconv.Itoa(int(newTask.ID))), job.uid)
		}
	}

}

func handleHeartbeatJob(c *net.UDPConn, job heartbeatJob) {
	registerHeartbeatClient(&job)
	maybeRunPendingTaskFromHeartbeat(&job)
	if _, err := c.WriteToUDP(heartbeatAckPacket, job.from); err != nil {
		log.Printf("UDP heartbeat reply failed: %v", err)
	}
}

func startHeartbeatWorkers(c *net.UDPConn) {
	heartbeatCh = make(chan heartbeatJob, heartbeatQueueSize)
	for i := 0; i < heartbeatWorkerCount; i++ {
		heartbeatWG.Add(1)
		go func() {
			defer heartbeatWG.Done()
			for job := range heartbeatCh {
				handleHeartbeatJob(c, job)
			}
		}()
	}
}

func stopHeartbeatWorkers() {
	if heartbeatCh == nil {
		return
	}
	close(heartbeatCh)
	heartbeatWG.Wait()
	heartbeatCh = nil
}

// Run 启动 UDP 服务
func Run(port int) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Printf("UDP resolve failed: %v", err)
		return
	}
	c, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Printf("UDP listen failed: %v", err)
		return
	}
	defer c.Close()

	connMu.Lock()
	conn = c
	connMu.Unlock()
	defer func() {
		connMu.Lock()
		conn = nil
		connMu.Unlock()
	}()

	startHeartbeatWorkers(c)
	defer stopHeartbeatWorkers()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go staleClientCleanupLoop(ctx)

	log.Printf("UDP server listening on :%d", port)

	buf := make([]byte, 65536)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			log.Printf("UDP read error: %v", err)
			continue
		}
		if n < HeaderSize {
			continue
		}

		_, _, cmdType, msgID, payload, ok := parsePacket(buf[:n])
		if !ok {
			continue
		}

		switch cmdType {
		case CmdHeartbeat:
			serialAndUid := string(append([]byte(nil), payload...))
			serialAndUidAndScriptIdSplit := strings.Split(serialAndUid, ",")

			if len(serialAndUidAndScriptIdSplit) < 2 {
				continue
			}
			serial := serialAndUidAndScriptIdSplit[0]
			uid, err := strconv.ParseUint(serialAndUidAndScriptIdSplit[1], 10, 32)
			var scriptID uint64 = 0
			if len(serialAndUidAndScriptIdSplit) == 3 {
				scriptID, err = strconv.ParseUint(serialAndUidAndScriptIdSplit[2], 10, 32)
			}

			if err != nil {
				continue
			}
			job := heartbeatJob{
				uid:      uint(uid),
				serial:   serial,
				hasTask:  msgID,
				scriptID: uint(scriptID),
				from:     cloneUDPAddr(from),
			}
			heartbeatCh <- job
		case CmdAck:
			// 忽略 ACK，命令结果通过 HTTP /udp/cmdcallback 返回
		}
	}
}

// DeliverResult 投递命令结果到等待的 channel（由 HTTP cmdcallback 调用）
func DeliverResult(msgID uint32, payload []byte) bool {
	if msgID == 0 {
		return false
	}
	ch, ok := pending.LoadAndDelete(msgID)
	if !ok {
		return false
	}
	select {
	case ch.(chan []byte) <- payload:
		return true
	default:
		return false
	}
}

// SendCommand 向指定序列号的设备发送 UDP 命令，通过 sync.Map + channel 等待结果
func SendCommand(serial string, cmdType uint32, payload []byte, userID uint) ([]byte, error) {
	var device model.Device
	fmt.Printf("SendCommand serial=%s cmdType=%d payload=%s userID=%d\n", serial, cmdType, string(payload), userID)
	err := database.DB.Preload("User").Where("serial = ?", serial).First(&device).Error
	if err != nil {
		fmt.Printf("device %s not found err=%v\n", serial, err)
		return nil, fmt.Errorf("device %s not found", serial)
	}
	if device.ExpireAt == nil || device.ExpireAt.Before(time.Now()) {
		fmt.Printf("device %s expired\n", serial)
		return nil, fmt.Errorf("device %s expired", serial)
	}
	msgID := NextMsgID()
	clientsMu.RLock()
	info, ok := clients[serial]
	var udpAddr *net.UDPAddr
	if ok && info != nil {
		udpAddr = info.Conn
	}
	clientsMu.RUnlock()
	if !ok || udpAddr == nil {
		fmt.Printf("device %s not online\n", serial)
		return nil, fmt.Errorf("device %s not online", serial)
	}

	connMu.RLock()
	c := conn
	connMu.RUnlock()
	if c == nil {
		fmt.Printf("UDP server not ready\n")
		return nil, fmt.Errorf("UDP server not ready")
	}
	if cmdType == CmdRunTaskScript {
		count, err := RunningTaskDeviceCount(context.Background(), userID, udpAddr.IP.String())
		if err != nil {
			fmt.Printf("get running task device count failed uid=%d ip=%s err=%v\n", userID, udpAddr.IP.String(), err)
			return nil, fmt.Errorf("get running task device count failed uid=%d ip=%s err=%v", userID, udpAddr.IP.String(), err)
		}

		if device.User.MaxDevicesPerIp > 0 && count >= int64(device.User.MaxDevicesPerIp) {
			fmt.Printf("running task device count is too many uid=%d ip=%s count=%d max=%d\n", userID, udpAddr.IP.String(), count, device.User.MaxDevicesPerIp)
			return nil, fmt.Errorf("running task device count is too many uid=%d ip=%s count=%d max=%d", userID, udpAddr.IP.String(), count, device.User.MaxDevicesPerIp)
		}
		if err := upsertRunningDeviceInRedis(context.Background(), userID, udpAddr.IP.String(), serial); err != nil {
			fmt.Printf("upsert running device failed uid=%d ip=%s serial=%s err=%v\n", userID, udpAddr.IP.String(), serial, err)
			return nil, fmt.Errorf("upsert running device failed uid=%d ip=%s serial=%s err=%v", userID, udpAddr.IP.String(), serial, err)
		}

	}
	ch := make(chan []byte, 1)
	pending.Store(msgID, ch)
	defer pending.Delete(msgID)

	const respTimeout = 6 * time.Second
	const maxRetries = 4

	for attempt := 0; attempt < maxRetries; attempt++ {
		pkt := buildPacket(cmdType, msgID, payload)
		if _, err := c.WriteToUDP(pkt, udpAddr); err != nil {
			return nil, err
		}

		select {
		case result := <-ch:
			return result, nil
		case <-time.After(respTimeout):
			if attempt < maxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("timeout after %d retries", maxRetries)
		}
	}

	return nil, fmt.Errorf("timeout")
}
