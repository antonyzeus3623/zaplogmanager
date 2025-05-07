package zaplogmanager

import (
	"time"

	"go.uber.org/zap"
)

// 安全机制和错误处理模块

// safeRunCompressionJob 带异常保护和任务去重的压缩任务执行
func safeRunCompressionJob(logDirs []string, compressMaxSave time.Duration) {
	// 检查任务执行间隔
	taskLock.Lock()
	if time.Since(lastRunTime) < minInterval {
		taskLock.Unlock()
		zap.S().Debugf("任务执行过于频繁，跳过本次执行")
		return
	}
	lastRunTime = time.Now()
	taskLock.Unlock()

	defer func() {
		if err := recover(); err != nil {
			zap.S().Errorf("日志压缩任务异常: %v", err)
		}
	}()

	runCompressionJob(logDirs, compressMaxSave)
}
