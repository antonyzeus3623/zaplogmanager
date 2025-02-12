package zaplogmanager

import (
	"compress/gzip"
	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	// 精确匹配带分隔符或纯数字的日期（包括但不限于：20231231、2023-12-31、2023_12_31）
	dateRegex   = regexp.MustCompile(`(?:^|[-_])(20\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12][0-9]|3[01]))(?:[-_.]|$)`)
	logExtRegex = regexp.MustCompile(`\.log$`)
	gzExtRegex  = regexp.MustCompile(`\.log\.gz$`)
	fileLock    sync.Mutex
)

// InitLogger 日志初始化结束后启动压缩任务
// 参数说明: warnFile、infoFile、debugFile--表示三种等级日志包含路径的文件名，路径为空时不写入该类日志
// newName: 表示替换文件名，建议设置为"-%Y%m%d.log" maxSaveTime: 原始日志最长保留时间，设置示例time.Hour*24*7
// rotationTime: 日志切割时间，设置示例time.Hour*24 compressMaxSave: 压缩日志最长保留时间，设置示例time.Hour*24*30
func InitLogger(warnFile, infoFile, debugFile, newName string, maxSaveTime, rotationTime, compressMaxSave time.Duration) {
	var cores []zapcore.Core

	if warnFile != "" {
		warnWriter := SetRotateRule(warnFile, newName, maxSaveTime, rotationTime) // "./log/warn/warn.log"
		warnCore := zapcore.NewCore(GetConfig(), warnWriter, zap.WarnLevel)
		cores = append(cores, warnCore)
	}

	if infoFile != "" {
		infoWriter := SetRotateRule(infoFile, newName, maxSaveTime, rotationTime) // "./log/info/info.log"
		infoCore := zapcore.NewCore(GetConfig(), infoWriter, zap.InfoLevel)
		cores = append(cores, infoCore)
	}

	if debugFile != "" {
		debugWriter := SetRotateRule(debugFile, newName, maxSaveTime, rotationTime) // "./log/debug/debug.log"
		debugCore := zapcore.NewCore(GetConfig(), debugWriter, zap.DebugLevel)
		cores = append(cores, debugCore)
	}

	consoleCore := zapcore.NewCore(GetConfig(), zapcore.Lock(zapcore.AddSync(zapcore.AddSync(os.Stdout))), zap.DebugLevel)
	cores = append(cores, consoleCore)

	core := zapcore.NewTee(cores...)

	_logger := zap.New(core, zap.AddCaller()) // 将调用函数信息记录到日志

	zap.ReplaceGlobals(_logger)

	// 启动定时压缩任务
	go StartLogCompression(1, 0, 0, compressMaxSave, filepath.Dir(warnFile), filepath.Dir(infoFile), filepath.Dir(debugFile))
}

func GetConfig() zapcore.Encoder {
	config := zap.NewDevelopmentEncoderConfig()

	config.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncodeLevel = zapcore.CapitalLevelEncoder // 让日志信息级别以大写输出

	return zapcore.NewConsoleEncoder(config)
}

// runCompressionJob 立即执行压缩任务
func runCompressionJob(logDirs []string, compressMaxSave time.Duration) {
	zap.S().Debugf("开始处理目录: %v", logDirs)
	fileLock.Lock()
	defer fileLock.Unlock()

	var wg sync.WaitGroup
	for _, rawDir := range logDirs {
		// 统一转换为绝对路径
		absDir, err := filepath.Abs(rawDir)
		if err != nil {
			zap.S().Errorf("路径转换失败: %v", err)
			continue
		}

		// 目录存在性检查
		if fi, err := os.Stat(absDir); err != nil || !fi.IsDir() {
			zap.S().Warnf("目录不存在: %v", absDir)
			continue
		}

		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			if err := filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
				// 关键修复点：增强错误处理
				if err != nil {
					if os.IsNotExist(err) {
						return nil
					}
					return err
				}

				// 压缩处理逻辑
				if !info.IsDir() && logExtRegex.MatchString(path) && isOldLogFile(path) && !isFileLocked(path) {
					zap.S().Debugf("发现可压缩文件: %v", path)
					if err := safeCompress(path); err != nil {
						zap.S().Errorf("压缩失败: %v", err)
					}
				}
				return nil
			}); err != nil {
				zap.S().Errorf("目录遍历失败: %v", err)
			}
			// 清理操作
			if err := cleanExpiredGzLogs(d, compressMaxSave); err != nil {
				zap.S().Errorf("日志清理失败: %v", err)
			}
		}(absDir)
	}
	wg.Wait()
}

// SetRotateRule 设置日志切割规则
// 参数说明: fileName: 包含路径的文件名，如"./cim.log" newName: 替换文件名，建议设置为"-%Y%m%d.log"或"-%Y%m%d%H%M"；
// maxSaveTime: 最长保存时间，建议设置为 time.Hour*24*30  rotationTime: 日志切割时间，建议设置为 time.Hour*24
func SetRotateRule(fileName, newName string, maxSaveTime, rotationTime time.Duration) zapcore.WriteSyncer {
	hook, err := rotatelogs.New(
		strings.Replace(fileName, ".log", "", -1)+newName,
		rotatelogs.WithMaxAge(maxSaveTime),
		rotatelogs.WithRotationTime(rotationTime),
	)

	if err != nil {
		zap.S().Panic(err)
	}

	return zapcore.AddSync(hook)
}

// StartLogCompression 启动日志压缩调度
// 参数说明：hour(小时) minute(分钟) second(秒) - 每天运行时间
// compressMaxSave: 压缩文件保留时间 logDirs: 需要监控的日志目录
func StartLogCompression(hour, minute, second int, compressMaxSave time.Duration, logDirs ...string) {
	// 程序启动时立即执行一次日志压缩和清理
	zap.S().Debugf("开始启动首次日志压缩和清理...")
	runCompressionJob(logDirs, compressMaxSave)

	ticker := time.NewTicker(1 * time.Hour)
	go func() {
		for range ticker.C {
			if time.Now().Hour() == hour {
				runCompressionJob(logDirs, compressMaxSave)
				time.Sleep(23 * time.Hour) // 保证24小时间隔
			}
		}
	}()
}

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

// gzipLogFile 压缩单个日志文件为.gz格式
func gzipLogFile(src string) error {
	// 打开原文件
	inFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if err := inFile.Close(); err != nil {
			zap.S().Error(err)
		}
	}()

	// 创建压缩文件（同名加.gz）
	dst := src + ".gz"
	outFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		if err := outFile.Close(); err != nil {
			zap.S().Error(err)
		}
	}()

	// 使用gzip写入器
	gzWriter := gzip.NewWriter(outFile)
	defer func() {
		if err := gzWriter.Close(); err != nil {
			zap.S().Error(err)
		}
	}()

	// 设置压缩头信息
	gzWriter.Name = filepath.Base(src)
	gzWriter.ModTime = time.Now()

	// 执行压缩
	if _, err = io.Copy(gzWriter, inFile); err != nil {
		return err
	}

	return nil
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
	now := time.Now().UTC()
	currentDate := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	// 文件日期是否是当日
	return fileDate.Before(currentDate)
}

// cleanExpiredGzLogs 清理过期压缩日志（包含原始.log和压缩的.gz）
func cleanExpiredGzLogs(logDir string, maxSaveTime time.Duration) error {
	return filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳过目录
		if info.IsDir() {
			return nil
		}

		// 仅处理.gz压缩文件
		if gzExtRegex.MatchString(path) {
			if isGzExpired(path, maxSaveTime) && !isFileLocked(path) {
				if err := os.Remove(path); err != nil {
					return err
				}
				zap.S().Infof("已删除过期压缩文件: %s", path)
			}
		}
		return nil
	})
}

// isGzExpired 判断压缩文件是否过期
func isGzExpired(path string, maxSaveTime time.Duration) bool {
	filename := filepath.Base(path)
	match := dateRegex.FindStringSubmatch(filename)
	if len(match) == 0 {
		return false
	}

	fileDate, err := time.Parse("20060102", match[1])
	if err != nil {
		return false
	}

	expirationTime := time.Now().Add(-maxSaveTime)
	return fileDate.Before(expirationTime)
}

// safeCompress 安全压缩函数
func safeCompress(path string) error {
	// 双重检查文件存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		zap.S().Debugf("文件不存在，跳过压缩：%s", path)
		return nil
	}

	zap.S().Debugf("开始压缩文件：%s", path)

	if err := gzipLogFile(path); err != nil {
		return err
	}

	// 压缩后二次确认删除
	if _, err := os.Stat(path); err == nil {
		return os.Remove(path)
	}
	return nil
}
