package zaplogmanager

import (
	"compress/gzip"
	"go.uber.org/zap"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 文件操作和压缩处理模块

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
