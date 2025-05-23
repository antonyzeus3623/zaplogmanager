package zaplogmanager

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

// 文件操作和压缩处理模块

// 使用细粒度锁替代全局锁
type dirLockMap struct {
	locks map[string]*sync.Mutex
	mu    sync.Mutex
}

var (
	dirLocks = &dirLockMap{
		locks: make(map[string]*sync.Mutex),
	}
	processingDirs = make(map[string]bool)
	processingMu   sync.Mutex
	// 添加文件处理状态跟踪
	processingFiles = make(map[string]bool)
	filesMu         sync.Mutex
)

// getLock 获取目录级别的锁
func (dm *dirLockMap) getLock(dir string) *sync.Mutex {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if lock, exists := dm.locks[dir]; exists {
		return lock
	}

	lock := &sync.Mutex{}
	dm.locks[dir] = lock
	return lock
}

// runCompressionJob 优化后的压缩任务
func runCompressionJob(logDirs []string, compressMaxSave time.Duration) {
	zap.S().Debugf("开始处理目录: %v", logDirs)

	// 检查是否有正在处理的目录
	processingMu.Lock()
	// 清理过期的处理状态（超过5分钟未完成的任务）
	for dir, _ := range processingDirs {
		if time.Since(lastRunTime) > time.Minute*5 {
			delete(processingDirs, dir)
		}
	}

	// 检查并标记要处理的目录
	dirsToProcess := make([]string, 0)
	for _, dir := range logDirs {
		if processingDirs[dir] {
			zap.S().Debugf("目录正在处理中，跳过: %v", dir)
			continue
		}
		processingDirs[dir] = true
		dirsToProcess = append(dirsToProcess, dir)
	}
	processingMu.Unlock()

	if len(dirsToProcess) == 0 {
		zap.S().Debugf("没有需要处理的目录")
		return
	}

	var wg sync.WaitGroup
	for _, rawDir := range dirsToProcess {
		absDir, err := filepath.Abs(rawDir)
		if err != nil {
			zap.S().Errorf("路径转换失败: %v", err)
			continue
		}

		if fi, err := os.Stat(absDir); err != nil || !fi.IsDir() {
			zap.S().Warnf("目录不存在: %v", absDir)
			continue
		}

		wg.Add(1)
		go func(d string) {
			defer wg.Done()
			defer func() {
				processingMu.Lock()
				delete(processingDirs, d)
				processingMu.Unlock()
			}()

			dirLock := dirLocks.getLock(d)
			dirLock.Lock()
			defer dirLock.Unlock()

			if err := processDirectory(d, compressMaxSave); err != nil {
				zap.S().Errorf("目录处理失败: %v", err)
			}
		}(absDir)
	}
	wg.Wait()
}

// processDirectory 处理单个目录
func processDirectory(dir string, compressMaxSave time.Duration) error {
	// 清理过期的文件处理状态
	filesMu.Lock()
	for file, _ := range processingFiles {
		if time.Since(lastRunTime) > time.Minute*5 {
			delete(processingFiles, file)
		}
	}
	filesMu.Unlock()

	// 处理历史日志压缩
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		if !info.IsDir() {
			// 检查文件是否正在处理
			filesMu.Lock()
			if processingFiles[path] {
				filesMu.Unlock()
				return nil
			}
			processingFiles[path] = true
			filesMu.Unlock()

			// 确保在处理完成后清理状态
			defer func() {
				filesMu.Lock()
				delete(processingFiles, path)
				filesMu.Unlock()
			}()

			if err := processFile(path); err != nil {
				zap.S().Errorf("文件处理失败: %v", err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// 清理过期压缩文件
	return cleanExpiredGzLogs(dir, compressMaxSave)
}

// processFile 处理单个文件
func processFile(path string) error {
	// 当日大文件检测逻辑
	if currentLogRegex.MatchString(path) && !isOldLogFile(path) {
		if checkAndCompressCurrentLog(path) {
			return nil
		}
	}

	// 旧日志压缩逻辑
	if logExtRegex.MatchString(path) && isOldLogFile(path) && !isFileLocked(path) {
		zap.S().Debugf("发现可压缩旧文件: %v", path)
		if err := safeCompress(path); err != nil {
			return fmt.Errorf("旧文件压缩失败: %v", err)
		}
	}
	return nil
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

// cleanExpiredGzLogs 清理过期压缩日志（包含原始.log和压缩的.gz）
func cleanExpiredGzLogs(logDir string, maxSaveTime time.Duration) error {
	cutoffDate := time.Now().Add(-maxSaveTime)
	zap.S().Debugf("开始清理过期日志，截止日期：%s", cutoffDate.Format("2006-01-02"))

	return filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 处理所有带日期的.gz文件
		if filepath.Ext(path) != ".gz" || !dateRegex.MatchString(path) {
			return nil
		}

		// 解析文件名中的日期
		fileDate, err := parseDateFromFileName(path)
		if err != nil {
			zap.S().Debugf("无法解析文件日期：%s, 错误：%v", path, err)
			return nil
		}

		if fileDate.Before(cutoffDate) {
			zap.S().Infof("清理过期文件：%s (创建时间：%s)", path, fileDate.Format("2006-01-02"))
			if err := os.Remove(path); err != nil {
				zap.S().Errorf("删除过期文件失败：%v", err)
			}
		}

		return nil
	})
}

// parseDateFromFileName 从文件名解析日期
func parseDateFromFileName(path string) (time.Time, error) {
	// 匹配格式示例:
	// - log-20250507.1.gz
	// - log-20250508.log.1.gz
	// - applog_2025-04-22.log.5.gz

	re := regexp.MustCompile(
		`(?:^|[-_./])(20\d{2})(0[1-9]|1[0-2])(0[1-9]|[12][0-9]|3[01])(?:[-_.]|$)`,
	)
	matches := re.FindStringSubmatch(filepath.Base(path))
	if len(matches) < 4 {
		return time.Time{}, fmt.Errorf("invalid filename format")
	}

	// 提取年月日
	year := matches[1]
	month := matches[2]
	day := matches[3]

	// 尝试解析日期
	dateStr := fmt.Sprintf("%s%s%s", year, month, day)
	return time.Parse("20060102", dateStr)
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

// checkAndCompressCurrentLog 检查并压缩当前日志文件
func checkAndCompressCurrentLog(path string) bool {
	// 双重校验机制防止误判
	if isOldLogFile(path) || isFileLocked(path) {
		return false
	}

	// 获取文件大小
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < maxCurrentSize {
		return false
	}

	// 执行带序号的压缩
	for i := 0; i < 3; i++ {
		if err := compressCurrentLogWithIndex(path); err == nil {
			return true
		}
		time.Sleep(time.Second * 1)
	}

	zap.S().Errorf("当日日志压缩失败: %s -> %v", path, err)
	return false
}

// gzipLogFileWithIndex 带序号压缩实现
func gzipLogFileWithIndex(src, dst string) error {
	// 打开源文件
	inFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %w", err)
	}
	defer inFile.Close()

	// 创建临时文件
	tmpFile := dst + ".tmp"
	outFile, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer func() {
		outFile.Close()
		os.Remove(tmpFile) // 清理临时文件
	}()

	// 创建gzip写入器
	gzWriter := gzip.NewWriter(outFile)
	defer gzWriter.Close()

	// 设置压缩头信息
	gzWriter.Name = filepath.Base(src)
	gzWriter.ModTime = time.Now()

	// 执行压缩
	if _, err = io.Copy(gzWriter, inFile); err != nil {
		return fmt.Errorf("压缩写入失败: %w", err)
	}

	// 确保所有数据都写入
	if err = gzWriter.Close(); err != nil {
		return fmt.Errorf("关闭压缩写入器失败: %w", err)
	}
	if err = outFile.Close(); err != nil {
		return fmt.Errorf("关闭输出文件失败: %w", err)
	}

	// 原子重命名临时文件为目标文件
	if err = os.Rename(tmpFile, dst); err != nil {
		return fmt.Errorf("重命名临时文件失败: %w", err)
	}

	zap.S().Infof("成功压缩大文件: %s -> %s", src, dst)
	return nil
}

// compressCurrentLogWithIndex 带序号的当日日志压缩
func compressCurrentLogWithIndex(src string) error {
	baseName := src
	existingFiles, _ := filepath.Glob(baseName + ".*.gz")

	// 原子化序号生成
	maxIndex := 0
	for _, f := range existingFiles {
		if idx := existingIndex(f); idx > maxIndex {
			maxIndex = idx
		}
	}
	nextIndex := maxIndex + 1

	// 带时间戳的压缩文件名
	compressedName := fmt.Sprintf("%s.%d.gz", baseName, nextIndex)
	return gzipLogFileWithIndex(src, compressedName)
}

func existingIndex(f string) int {
	re := regexp.MustCompile(`\.log\.(\d+)\.gz$`)
	matches := re.FindStringSubmatch(f)
	if len(matches) < 2 {
		return 0
	}

	idx, _ := strconv.Atoi(matches[1])

	return idx
}
