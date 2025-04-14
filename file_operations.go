package zaplogmanager

import (
	"compress/gzip"
	"fmt"
	"go.uber.org/zap"
	"io"
	"os"
	"path/filepath"
	"strings"
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
			// 处理历史日志压缩
			if err := filepath.Walk(d, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					if os.IsNotExist(err) {
						return nil
					}
					return err
				}

				// 当日大文件检测逻辑
				if currentLogRegex.MatchString(path) && !isOldLogFile(path) {
					if checkAndCompressCurrentLog(path) {
						return nil
					}
				}

				// 旧日志压缩逻辑
				if !info.IsDir() && logExtRegex.MatchString(path) && isOldLogFile(path) && !isFileLocked(path) {
					zap.S().Debugf("发现可压缩旧文件: %v", path)
					if err := safeCompress(path); err != nil {
						zap.S().Errorf("旧文件压缩失败: %v", err)
					}
				}
				return nil
			}); err != nil {
				zap.S().Errorf("目录遍历失败: %v", err)
			}

			// 清理过期压缩文件（原有逻辑）
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

// checkAndCompressCurrentLog 检查并压缩当前日志文件
func checkAndCompressCurrentLog(path string) bool {
	// 获取文件大小
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < maxCurrentSize {
		return false
	}

	// 执行带序号的压缩
	if err := compressCurrentLogWithIndex(path); err != nil {
		zap.S().Errorf("当日日志压缩失败: %s -> %v", path, err)
		return false
	}
	return true
}

// compressCurrentLogWithIndex 带序号的当日日志压缩
func compressCurrentLogWithIndex(src string) error {
	// 获取基础文件名（不含扩展名）
	baseName := strings.TrimSuffix(src, filepath.Ext(src))

	// 查找可用序号
	index := 1
	for {
		dst := fmt.Sprintf("%s.%d.gz", baseName, index)
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			return gzipLogFileWithIndex(src, dst)
		}
		index++
	}
}

// gzipLogFileWithIndex 带序号压缩实现
func gzipLogFileWithIndex(src, dst string) error {
	inFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %w", err)
	}
	defer func() {
		if err := inFile.Close(); err != nil {
			zap.S().Error(err)
		}
	}()

	outFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建压缩文件失败: %w", err)
	}
	defer func() {
		if err := outFile.Close(); err != nil {
			zap.S().Error(err)
		}
	}()

	gzWriter := gzip.NewWriter(outFile)
	defer func() {
		if err := gzWriter.Close(); err != nil {
			zap.S().Error(err)
		}
	}()

	gzWriter.Name = filepath.Base(src)
	gzWriter.ModTime = time.Now()

	if _, err = io.Copy(gzWriter, inFile); err != nil {
		return fmt.Errorf("压缩写入失败: %w", err)
	}

	// 清空原文件（而不是删除）
	if err := os.Truncate(src, 0); err != nil {
		return fmt.Errorf("清空原文件失败: %w", err)
	}

	zap.S().Infof("成功压缩大文件: %s -> %s", src, dst)
	return nil
}
