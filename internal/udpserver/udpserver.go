package udpserver

import (
	"context"
	"encoding/binary"
	"fmt"
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
	CmdReboot           = 11
	CmdPauseTask        = 12
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

// planTaskItemDedupeTTL 同一设备同一脚本计划任务入队的最小间隔
const planTaskItemDedupeTTL = 30 * time.Minute

const planTaskItemDedupeKeyFmt = "udpserver:plantask:dedupe:%d_%d_%d"

func planTaskItemDedupeKey(deviceID, scriptID, deviceUserID uint) string {
	return fmt.Sprintf(planTaskItemDedupeKeyFmt, deviceID, scriptID, deviceUserID)
}

const scriptLeftRoundKeyFmt = "device_script_left_round:%d_%d_%d"

func DeviceScriptLeftRoundKey(deviceID, scriptID, deviceUserID uint) string {
	return fmt.Sprintf(scriptLeftRoundKeyFmt, deviceID, scriptID, deviceUserID)
}

const (
	deviceLastDeviceUserIdEndTaskTimeKeyFmt = "DeviceLastDeviceUserIdEndTaskTime:%s:%s"
	deviceLastDeviceUserIdEndTaskTimeTTL    = 40 * time.Minute
)

func deviceLastDeviceUserIdEndTaskTimeKey(serial, deviceUserID string) string {
	return fmt.Sprintf(deviceLastDeviceUserIdEndTaskTimeKeyFmt, serial, deviceUserID)
}

// UpdateLastDeviceUserIdEndTaskTime 记录设备某 Android 用户最近一次任务结束时间，40 分钟有效。
func UpdateLastDeviceUserIdEndTaskTime(ctx context.Context, serial, deviceUserID string) error {
	if database.RDB == nil {
		return nil
	}
	if serial == "" {
		return nil
	}
	key := deviceLastDeviceUserIdEndTaskTimeKey(serial, deviceUserID)
	return database.RDB.Set(ctx, key, time.Now().Format(time.RFC3339), deviceLastDeviceUserIdEndTaskTimeTTL).Err()
}

// GetLastDeviceUserIdEndTaskTime 返回距该设备用户最近一次任务结束的秒数。
// key 不存在或解析失败时视为足够空闲，返回一个很大的秒数。
func GetLastDeviceUserIdEndTaskTime(ctx context.Context, serial string, deviceUserID uint) int {
	const idleEnoughSeconds = 365 * 24 * 3600
	if database.RDB == nil || serial == "" {
		return idleEnoughSeconds
	}
	key := deviceLastDeviceUserIdEndTaskTimeKey(serial, strconv.FormatUint(uint64(deviceUserID), 10))
	val, err := database.RDB.Get(ctx, key).Result()
	if err != nil || val == "" {
		return idleEnoughSeconds
	}
	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return idleEnoughSeconds
	}
	sec := int(time.Since(t).Seconds())
	if sec < 0 {
		return 0
	}
	return sec
}

type scriptUserKey struct {
	ScriptID     uint
	DeviceUserID uint
}

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
	uid         uint
	serial      string
	hasTask     uint32
	scriptID    uint
	idleSeconds int
	from        *net.UDPAddr
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

	//fmt.Printf("registerHeartbeatClient serial=%s ip=%s\n", job.serial, job.from.IP.String())
	ctx := context.Background()
	if err := database.RDB.Set(ctx, OnlineDevicePrefix+job.serial, job.from.IP.String(), clientStaleTimeout).Err(); err != nil {
		fmt.Printf("set online device ttl failed serial=%s err=%v", job.serial, err)
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
		fmt.Printf("set max devices per ip failed userID=%d limit=%d err=%v", userID, limit, err)
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
func ReleaseScriptLockSlot(ctx context.Context, scriptID uint, lockSlot int, serial string, ip string) {
	key := fmt.Sprintf("%s:%d:%d", ip, scriptID, lockSlot)
	database.RDB.Del(ctx, key)
}
func getScriptLockSlot(ctx context.Context, scriptID uint, maxDevicesPerIp int, ip string, serial string, lockminutes int) int {
	lockTTL := time.Duration(lockminutes) * time.Minute

	for n := 1; n <= maxDevicesPerIp; n++ {
		key := fmt.Sprintf("%s:%d:%d", ip, scriptID, n)
		ok, err := database.RDB.SetNX(ctx, key, serial, lockTTL).Result()
		if err != nil {
			return 0
		}
		if ok {
			return n
		}
	}
	return 0
}

// checkPlanTask 在设备空闲时为它生成今日还未达到额度的计划任务对应的 model.Task 行；
// 实际下发由后续 maybeRunPendingTaskFromHeartbeat 中的 SendCommand 处理。
func checkPlanTask(device *model.Device, idleSeconds int, ip string) {
	//fmt.Printf("checkPlanTask device.Serial=%s idleSeconds=%d ip=%s device.id=%d device.user_id=%d\n", device.Serial, idleSeconds, ip, device.ID, device.UserID)
	// if config.Cfg.IS_DEBUG {
	// 	return
	// }
	if device == nil || device.ID == 0 {
		fmt.Printf("checkPlanTask device is nil or id is 0\n")
		return
	}

	// 已有未完成的任务排队/运行，跳过本次生成，避免重复堆积
	// var pendingCount int64
	// if err := database.DB.Model(&model.Task{}).
	// 	Where("device_id = ? AND status = ? ",
	// 		device.ID,
	// 		model.TaskStatusNotStarted,
	// 	).
	// 	Count(&pendingCount).Error; err != nil {
	// 	return
	// }
	// if pendingCount > 0 {
	// 	fmt.Printf("checkPlanTask pendingCount=%d\n", pendingCount)
	// 	return
	// }

	// 1. 该设备绑定的计划任务
	var devicePlanTasks []model.DevicePlanTask
	if err := database.DB.
		Where("device_id = ? AND user_id = ?", device.ID, device.UserID).
		Find(&devicePlanTasks).Error; err != nil {
		//fmt.Printf("checkPlanTask get devicePlanTasks failed err=%v\n", err)
		return
	}
	fmt.Println("device.id=%d,user.id=%d,device.serial=%s", device.ID, device.UserID, device.Serial)
	if len(devicePlanTasks) == 0 {
		fmt.Printf("checkPlanTask device.serial=%s devicePlanTasks is empty\n", device.Serial)
		return
	}
	planTaskIDs := make([]uint, 0, len(devicePlanTasks))
	for _, dpt := range devicePlanTasks {
		planTaskIDs = append(planTaskIDs, dpt.PlanTaskID)
	}

	var planTasks []model.PlanTask
	if err := database.DB.
		Where("id IN (?) AND user_id = ?", planTaskIDs, device.UserID).
		Order("is_timed_trigger DESC").
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
	if err := database.DB.Preload("Script").
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

	// 3. 加载设备上的 Android 用户；今天 0 点起按 script_id + device_user_id 累加已执行分钟
	var profiles []model.DeviceUserProfile
	if err := database.DB.Where("device_serial = ?", device.Serial).Find(&profiles).Error; err != nil {
		fmt.Printf("checkPlanTask get profiles failed err=%v\n", err)
		return
	}
	deviceUserIDs := make([]uint, 0, len(profiles))
	for _, p := range profiles {
		deviceUserIDs = append(deviceUserIDs, p.UserID)
	}
	if len(deviceUserIDs) == 0 {
		deviceUserIDs = []uint{0}
	}

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	type execRow struct {
		ScriptID                   uint
		DeviceUserID               uint
		ExecutedMinutes            int
		TimerTrigedExecutedMinutes int
	}
	var execRows []execRow
	if err := database.DB.Model(&model.Task{}).
		Select("script_id AS script_id, device_user_id AS device_user_id, COALESCE(SUM(total_minutes), 0) AS executed_minutes, COALESCE(SUM(CASE WHEN TASK_TYPE = 'time_shot' THEN total_minutes ELSE 0 END), 0) AS timer_triged_executed_minutes").
		Where("device_id = ? AND ( status=2 OR status=7 ) AND created_at >= ?", device.ID, todayStart).
		Group("script_id, device_user_id").
		Scan(&execRows).Error; err != nil {
		fmt.Printf("checkPlanTask get execRows failed err=%v\n", err)
		return
	}

	executedByScriptUser := make(map[scriptUserKey]int, len(execRows))
	timerTrigedExecutedByScriptUser := make(map[scriptUserKey]int, len(execRows))
	for _, r := range execRows {
		k := scriptUserKey{ScriptID: r.ScriptID, DeviceUserID: r.DeviceUserID}
		executedByScriptUser[k] = r.ExecutedMinutes
		timerTrigedExecutedByScriptUser[k] = r.TimerTrigedExecutedMinutes
	}

	// 4. 遍历每个计划任务及其条目，按设备用户分别判断额度并创建 Task 行
	for _, pt := range planTasks {
		items := itemsByPlan[pt.ID]
		if len(items) == 0 {
			fmt.Printf("checkPlanTask items is empty planTask.ID=%d\n", pt.ID)
			continue
		}

		if pt.ExecutionOrder == model.PlanTaskExecutionOrderRandom {
			rand.Shuffle(len(items), func(i, j int) {
				items[i], items[j] = items[j], items[i]
			})
		}
		for _, deviceUserID := range deviceUserIDs {
			if pt.IdleMinutes > 0 {
				deviceUserIdIdleSeconds := GetLastDeviceUserIdEndTaskTime(context.Background(), device.Serial, deviceUserID)
				fmt.Printf("checkPlanTask deviceUserIdIdleSeconds=%d planTask.IdleMinutes=%d key=%s\n", deviceUserIdIdleSeconds, pt.IdleMinutes, deviceLastDeviceUserIdEndTaskTimeKey(device.Serial, strconv.FormatUint(uint64(deviceUserID), 10)))
				if deviceUserIdIdleSeconds < pt.IdleMinutes*60 {
					fmt.Printf("checkPlanTask idleSeconds < planTask.IdleMinutes*60 idleSeconds=%d planTask.IdleMinutes=%d\n", idleSeconds, pt.IdleMinutes)
					continue
				}
			}
			for _, item := range items {
				duration := item.DurationMinute
				if duration <= 0 {
					duration = 40
				}
				round := item.TotalRound
				if round <= 0 {
					round = 1
				}
				required := round * duration

				execKey := scriptUserKey{ScriptID: item.ScriptID, DeviceUserID: deviceUserID}
				executed := executedByScriptUser[execKey]
				if pt.IsTimedTrigger {
					if timerTrigedExecutedByScriptUser[execKey] >= required {
						continue
					}
				} else {
					if executed >= required {
						continue
					}
				}

				task_type := ""
				if pt.IsTimedTrigger {
					task_type = "time_shot"
					parsed, err := time.ParseInLocation("15:04", strings.TrimSpace(item.StartTime), now.Location())
					if err != nil {
						continue
					}
					startMoment := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
					if now.Before(startMoment) {
						continue
					}
				}
				leftRoundKey := DeviceScriptLeftRoundKey(device.ID, item.ScriptID, deviceUserID)
				leftRound := 0
				if database.RDB != nil {
					ctx := context.Background()
					val, err := database.RDB.Get(ctx, leftRoundKey).Int()
					if err == redis.Nil {
						leftRound = item.TotalRound
						if err := database.RDB.Set(ctx, leftRoundKey, leftRound, 0).Err(); err != nil {
							fmt.Printf("set device script left round failed device=%d script=%d deviceUser=%d err=%v", device.ID, item.ScriptID, deviceUserID, err)
						}
					} else if err != nil {
						fmt.Printf("get device script left round failed device=%d script=%d deviceUser=%d err=%v", device.ID, item.ScriptID, deviceUserID, err)
					} else {
						leftRound = val
					}

					if leftRound <= 0 {
						fmt.Printf("checkPlanTask leftRound <= 0 leftRound=%d planTask.ID=%d plantaskitem.ID=%d device.Id=%d device.serial=%s deviceUser=%d\n", leftRound, pt.ID, item.ID, device.ID, device.Serial, deviceUserID)
						continue
					}
				}

				dedupeKey := planTaskItemDedupeKey(device.ID, item.ScriptID, deviceUserID)
				if database.RDB != nil {
					ok, err := database.RDB.SetNX(context.Background(), dedupeKey, "1", time.Duration(pt.IdleMinutes+1)*time.Minute).Result()
					if err != nil {
						fmt.Printf("set plan task dedupe key failed device=%d script=%d deviceUser=%d err=%v", device.ID, item.ScriptID, deviceUserID, err)
					} else if !ok {
						fmt.Printf("exists continue")
						continue
					}
				}
				lock_slot := 0
				if item.Script.MaxDevicesPerIp > 0 {
					lock_slot = getScriptLockSlot(context.Background(), item.Script.ID, item.Script.MaxDevicesPerIp, ip, device.Serial, duration)
					if lock_slot <= 0 {
						_ = database.RDB.Del(context.Background(), dedupeKey).Err()
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
					TotalRound:     1,
					LeftRound:      1,
					LeftMinute:     duration,
					Status:         model.TaskStatusNotStarted,
					PlanTaskID:     int(pt.ID),
					PlanTaskItemID: int(item.ID),
					DeviceUserID:   deviceUserID,
					CreatedAt:      now,
					UpdatedAt:      now,
					LockSlot:       lock_slot,
					TASK_TYPE:      task_type,
				}
				if err := database.DB.Create(&task).Error; err != nil {
					fmt.Printf("create plan task row failed device=%s script=%d deviceUser=%d err=%v", device.Serial, item.ScriptID, deviceUserID, err)
					if database.RDB != nil {
						_ = database.RDB.Del(context.Background(), dedupeKey).Err()
						if lock_slot > 0 {
							ReleaseScriptLockSlot(context.Background(), item.ScriptID, lock_slot, device.Serial, ip)
						}
					}
					continue
				}
				if database.RDB != nil {
					_ = database.RDB.Set(context.Background(), leftRoundKey, leftRound-1, 0).Err()
				}
				// 把刚刚入队的执行时长计入，避免同一脚本+用户被本轮循环重复入队
				executedByScriptUser[execKey] = executed + duration
				return
			}
		}
	}
}

// maybeRunPendingTaskFromHeartbeat 在设备空闲心跳时检查是否有待运行任务并下发
func maybeRunPendingTaskFromHeartbeat(job *heartbeatJob) {
	ctx := context.Background()
	if job.hasTask != 0 {
		if err := upsertRunningDeviceInRedis(ctx, job.uid, job.from.IP.String(), job.serial); err != nil {
			fmt.Printf("upsert running device failed uid=%d serial=%s ip=%s err=%v", job.uid, job.serial, job.from.IP.String(), err)
		}
		return
	} else {
		// if err := removeRunningDeviceFromRedis(ctx, job.uid, job.from.IP.String(), job.serial); err != nil {
		// 	fmt.Printf("remove running device failed uid=%d serial=%s ip=%s err=%v", job.uid, job.serial, job.from.IP.String(), err)
		// }
	}

	n := getMaxDevicesPerIp(job.uid)
	if n > 0 {
		count, err := RunningTaskDeviceCount(ctx, job.uid, job.from.IP.String())
		if err != nil {
			fmt.Printf("get running task device count failed uid=%d ip=%s err=%v", job.uid, job.from.IP.String(), err)
		}
		if count >= int64(n) {
			fmt.Printf("maybeRunPendingTaskFromHeartbeat count>=n count=%d n=%d\n", count, n)
			return
		}
	}

	//check plan task（同一设备 20s 内只判断一次）
	if shouldRunCheckPlanTask(ctx, job.serial) {
		var device model.Device
		if err := database.DB.Where("serial = ?", job.serial).First(&device).Error; err != nil {

			fmt.Printf("get device failed serial=%s err=%v", job.serial, err)
			return
		}
		if device.ExpireAt != nil && device.ExpireAt.Before(time.Now()) {
			fmt.Printf("device expired serial=%s\n", job.serial)
			return
		}
		//fmt.Printf("checkPlanTask Job.serial=%s idleSeconds=%d\n", job.serial, job.idleSeconds)
		checkPlanTask(&device, job.idleSeconds, job.from.IP.String())
		var newTask model.Task

		if err := database.DB.Where(
			"device_serial = ? and status=0",
			job.serial).First(&newTask).Error; err != nil {
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
		fmt.Printf("UDP heartbeat reply failed: %v", err)
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
		fmt.Printf("UDP resolve failed: %v", err)
		return
	}
	c, err := net.ListenUDP("udp", addr)
	if err != nil {
		fmt.Printf("UDP listen failed: %v", err)
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

	fmt.Printf("UDP server listening on :%d", port)

	buf := make([]byte, 65536)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			fmt.Printf("UDP read error: %v", err)
			continue
		}
		if n < HeaderSize {
			continue
		}

		_, _, cmdType, _, payload, ok := parsePacket(buf[:n])
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
			if len(serialAndUidAndScriptIdSplit) >= 3 {
				scriptID, err = strconv.ParseUint(serialAndUidAndScriptIdSplit[2], 10, 32)
			}
			idleSeconds := 0
			if len(serialAndUidAndScriptIdSplit) >= 4 {
				idleSeconds, err = strconv.Atoi(serialAndUidAndScriptIdSplit[3])
			}

			if err != nil {
				continue
			}
			hasTask := uint32(0)
			if scriptID > 0 {
				hasTask = 1
			}
			job := heartbeatJob{
				uid:         uint(uid),
				serial:      serial,
				hasTask:     hasTask,
				scriptID:    uint(scriptID),
				from:        cloneUDPAddr(from),
				idleSeconds: idleSeconds,
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
