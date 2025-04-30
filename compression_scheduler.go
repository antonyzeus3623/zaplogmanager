package zaplogmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
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

	// 启动一个独立的goroutine来执行定时监控任务
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
	// 保证只在一个循环中触发
	for {
		// 计算到下次执行时间的延迟
		next := nextRunTime(hour, minute, second)
		waitDuration := time.Until(next)

		// 创建精准定时器
		timer := time.NewTimer(waitDuration)
		<-timer.C // 等待到目标时间

		// 执行压缩任务
		safeRunCompressionJob(logDirs, compressMaxSave)

		// 重置定时器为24小时循环
		timer.Stop() // 清除已到期定时器

		// 	执行跨天压缩
		fileLock.Lock()
		processOvernightLogs(logDirs)
		fileLock.Unlock()
	}
}

func processOvernightLogs(logDirs []string) {
	yesterday := time.Now().AddDate(0, 0, -1).Format(dateFormat)

	for _, dir := range logDirs {
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
	}
}

// forceCompressOvernightLog 跨天压缩
func forceCompressOvernightLog(src string) error {
	baseName := strings.TrimSuffix(src, filepath.Ext(src))
	compressedName := fmt.Sprintf("%s.1.gz", baseName)

	// 	存在则递增序号
	if _, err := os.Stat(compressedName); err == nil {
		return compressCurrentLogWithIndex(src)
	}

	return gzipLogFileWithIndex(src, compressedName)
}

func isYesterdayLog(path string, yesterday string) bool {
	baseName := filepath.Base(path)
	return strings.Contains(baseName, yesterday) && !gzExtRegex.MatchString(baseName)
}

// 计算下一个执行时刻（精确到秒）
func nextRunTime(targetHour, targetMin, targetSec int) time.Time {
	now := time.Now().Truncate(time.Second) // 去掉纳秒级时间

	// 构造目标时间
	next := time.Date(
		now.Year(), now.Month(), now.Day(),
		targetHour, targetMin, targetSec, 0,
		now.Location(),
	)

	// 如果今天的时间已过，则设置为明天
	if next.Before(now) {
		next = next.AddDate(0, 0, 1)
	}

	return next
}
