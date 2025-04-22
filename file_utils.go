package zaplogmanager

import (
	"go.uber.org/zap"
	"os"
	"path/filepath"
	"time"
)

// 文件检测工具模块

// isFileLocked 检查文件是否被锁定
func isFileLocked(path string) bool {
	// 检查文件是否存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}

	// 尝试以独占模式打开文件
	f, err := os.OpenFile(path, os.O_RDWR|os.O_EXCL, 0)
	switch {
	case os.IsPermission(err):
		return true
	case err == nil:
		_ = f.Close()
		return false
	default:
		return false
	}
}

// isOldLogFile 判断日志文件是否非当日日志文件
func isOldLogFile(path string) bool {
	filename := filepath.Base(path)

	match := dateRegex.FindStringSubmatch(filename)
	if len(match) < 2 {
		zap.S().Debugf("未匹配到日期：%s", filename)
		return false
	}

	// 获取文件日期
	fileDate, err := time.Parse("20060102", match[1])
	if err != nil {
		zap.S().Debugf("日期解析失败：%s", match[1])
		return false
	}

	// 计算当日零点（UTC时间）
	now := time.Now().Local()
	currentDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// 文件日期是否是当日
	return fileDate.Before(currentDate)
}
