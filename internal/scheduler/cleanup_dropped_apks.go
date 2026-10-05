package scheduler

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gobackend/config"
)

const cleanupDroppedApkHour = 3
const cleanupDroppedApkMinute = 0

func cleanupDroppedApkLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*60*60)
	}
	return loc
}

// nextCleanupDroppedApkTime 返回下一次每日清理时刻（北京时间 03:00）。
func nextCleanupDroppedApkTime(now time.Time) time.Time {
	loc := cleanupDroppedApkLocation()
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), cleanupDroppedApkHour, cleanupDroppedApkMinute, 0, 0, loc)
	if now.Before(today) {
		return today
	}
	return today.Add(24 * time.Hour)
}

// CleanupDroppedApks 删除上传目录中修改时间早于一天前的 apk。
func CleanupDroppedApks() error {
	if config.Cfg.SOLUTION_DIR == "" {
		return nil
	}
	dir := filepath.Join(config.Cfg.SOLUTION_DIR, "antares_assets", "files")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	deleted := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".apk") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			log.Printf("cleanup dropped apk stat %s: %v", entry.Name(), err)
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil {
			log.Printf("cleanup dropped apk remove %s: %v", path, err)
			continue
		}
		deleted++
	}
	log.Printf("cleanup dropped apk: deleted %d files (mtime < %s)", deleted, cutoff.Format(time.RFC3339))
	return nil
}

// StartCleanupDroppedApkLoop 每天 03:00 删除一天前上传的 apk。
func StartCleanupDroppedApkLoop(ctx context.Context) {
	for {
		next := nextCleanupDroppedApkTime(time.Now())
		wait := time.Until(next)
		log.Printf("cleanup dropped apk scheduled at %s (in %s)", next.Format(time.RFC3339), wait)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
			if err := CleanupDroppedApks(); err != nil {
				log.Printf("cleanup dropped apk failed: %v", err)
			}
		}
	}
}
