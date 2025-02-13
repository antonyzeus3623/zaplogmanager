package zaplogmanager

import (
	"go.uber.org/zap"
	"time"
)

// 安全机制和错误处理模块

// safeRunCompressionJob 带异常保护的压缩任务执行
func safeRunCompressionJob(logDirs []string, compressMaxSave time.Duration) {
	defer func() {
		if err := recover(); err != nil {
			zap.S().Errorf("日志压缩任务异常: %v", err)
		}
	}()

	runCompressionJob(logDirs, compressMaxSave)
}
