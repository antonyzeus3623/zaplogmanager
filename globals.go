package zaplogmanager

import (
	"regexp"
	"sync"
)

// 全局变量声明

var (
	// dateRegex 精确匹配带分隔符或纯数字的日期（包括但不限于：20231231、2023-12-31、2023_12_31）
	dateRegex   = regexp.MustCompile(`(?:^|[-_])(20\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12][0-9]|3[01]))(?:[-_.]|$)`)
	logExtRegex = regexp.MustCompile(`\.log$`)
	gzExtRegex  = regexp.MustCompile(`\.log\.gz$`)
	fileLock    sync.Mutex
)
