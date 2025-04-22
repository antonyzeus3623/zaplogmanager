package zaplogmanager

import (
	"regexp"
	"sync"
	"time"
)

// 全局变量声明

var (
	// dateRegex 精确匹配带分隔符或纯数字的日期（包括但不限于：20231231、2023-12-31、2023_12_31）
	dateRegex       = regexp.MustCompile(`(?:^|[-_])(20\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12][0-9]|3[01]))(?:[-_.]|$)`)
	logExtRegex     = regexp.MustCompile(`\.log$`)
	gzExtRegex      = regexp.MustCompile(`\.log\.gz$`)
	fileLock        sync.Mutex
	currentLogRegex = regexp.MustCompile(`\.log$`) // 匹配当前正在写入的日志文件

	maxCurrentSize    = int64(1 << 30) // 1GB阈值
	sizeCheckInterval = time.Hour      // 每小时检查一次
	dateFormat        = "20060102"     // 日期格式
)
