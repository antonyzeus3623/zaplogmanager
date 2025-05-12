package zaplogmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// 添加全局任务锁和状态控制
var (
	taskLock    sync.Mutex
	lastRunTime time.Time
	minInterval = time.Second * 5 // 最小任务间隔
)

// 定时任务调度模块

// StartLogCompression 启动日志压缩调度
// 参数说明：hour(小时) minute(分钟) second(秒) - 每天运行时间
// compressMaxSave: 压缩文件保留时间 logDirs: 需要监控的日志目录
func StartLogCompression(hour, minute, second int, compressMaxSave time.Duration, logDirs ...string) {
	zap.S().Debugf("开始启动首次日志压缩和清理...")
	safeRunCompressionJob(logDirs, compressMaxSave)

	// 启动定时任务
	go scheduleDailyJob(hour, minute, second, compressMaxSave, logDirs...)

	// 启动一个独立goroutine来执行定时监控任务
	// 启动大小监控任务
	go func() {
		ticker := time.NewTicker(sizeCheckInterval)
		defer ticker.Stop()

		for range ticker.C {
			zap.S().Debugf("启动小时级日志大小监控...")
			safeRunCompressionJob(logDirs, compressMaxSave)
		}
	}()
}

// scheduleDailyJob 核心调度逻辑
func scheduleDailyJob(hour, minute, second int, compressMaxSave time.Duration, logDirs ...string) {
	for {
		next := nextRunTime(hour, minute, second)
		timer := time.NewTimer(time.Until(next))
		<-timer.C

		if isTargetHour(next, 1) {
			// 执行压缩任务
			safeRunCompressionJob(logDirs, compressMaxSave)

			// 执行跨天压缩
			fileLock.Lock()
			processOvernightLogs(logDirs, compressMaxSave)
			fileLock.Unlock()
		}

		timer.Stop()
	}
}

func processOvernightLogs(logDirs []string, compressMaxSave time.Duration) {
	yesterday := time.Now().AddDate(0, 0, -1).Format(dateFormat)

	for _, dir := range logDirs {
		// 处理跨天日志
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if currentLogRegex.MatchString(path) && isYesterdayLog(path, yesterday) {
				zap.S().Infof("检测到跨天日志：%s", path)
				if err := forceCompressOvernightLog(path); err != nil {
					zap.S().Errorf("跨天压缩失败：%v", err)
				}
			}
			return nil
		})
		if err != nil {
			zap.S().Error(err)
			return
		}

		// 清理过期压缩日志
		if err := cleanExpiredGzLogs(dir, compressMaxSave); err != nil {
			zap.S().Errorf("清理过期日志失败：%v", err)
		}
	}
}

// isTargetHour 判断是否是目标小时
func isTargetHour(t time.Time, targetHour int) bool {
	return t.Hour() == targetHour
}

// forceCompressOvernightLog 跨天压缩
func forceCompressOvernightLog(src string) error {
	// 保持原始文件名格式，只添加序号和.gz后缀
	baseName := src
	compressedName := fmt.Sprintf("%s.1.gz", baseName)

	// 检查是否已存在压缩文件
	if _, err := os.Stat(compressedName); err == nil {
		return compressCurrentLogWithIndex(src)
	}

	// 执行压缩
	if err := gzipLogFileWithIndex(src, compressedName); err != nil {
		return fmt.Errorf("跨天压缩失败: %w", err)
	}

	// 压缩成功后删除原文件
	return os.Remove(src)
}

func isYesterdayLog(path string, yesterday string) bool {
	baseName := filepath.Base(path)
	return strings.Contains(baseName, yesterday) && !gzExtRegex.MatchString(baseName)
}

// 计算下一个执行时刻（精确到秒）
func nextRunTime(targetHour, targetMin, targetSec int) time.Time {
	now := time.Now()

	// 构造目标时间
	next := time.Date(
		now.Year(), now.Month(), now.Day(),
		targetHour, targetMin, targetSec, 0,
		now.Location(),
	)

	// 如果今天的时间已过，则设置为明天
	if next.Before(now) {
		next = next.Add(24 * time.Hour)
	}

	return next
}
