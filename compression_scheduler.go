package zaplogmanager

import (
	"go.uber.org/zap"
	"time"
)

// 定时任务调度模块

// StartLogCompression 启动日志压缩调度
// 参数说明：hour(小时) minute(分钟) second(秒) - 每天运行时间
// compressMaxSave: 压缩文件保留时间 logDirs: 需要监控的日志目录
func StartLogCompression(hour, minute, second int, compressMaxSave time.Duration, logDirs ...string) {
	// 程序启动时立即执行一次日志压缩和清理
	zap.S().Debugf("开始启动首次日志压缩和清理...")
	safeRunCompressionJob(logDirs, compressMaxSave)

	// 启动定时任务
	go scheduleDailyJob(hour, minute, second, compressMaxSave, logDirs...)
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
	}
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
