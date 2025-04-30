package zaplogmanager

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 日志初始化配置模块

// InitLogger 日志初始化结束后启动压缩任务
// 参数说明: warnFile、infoFile、debugFile--表示三种等级日志包含路径的文件名，路径为空时不写入该类日志
// newName: 表示替换文件名，建议设置为"-%Y%m%d.log" maxSaveTime: 原始日志最长保留时间，设置示例time.Hour*24*7
// rotationTime: 日志切割时间，设置示例time.Hour*24 compressMaxSave: 压缩日志最长保留时间，设置示例time.Hour*24*30
func InitLogger(warnFile, infoFile, debugFile, newName string, maxSaveTime, rotationTime, compressMaxSave time.Duration) {
	var cores []zapcore.Core

	if warnFile != "" {
		warnWriter := SetRotateRule(warnFile, newName, maxSaveTime, rotationTime)
		bufferedWarnWriter := NewBufferedWriteSyncer(warnWriter)
		warnCore := zapcore.NewCore(GetConfig(), bufferedWarnWriter, zap.WarnLevel)
		cores = append(cores, warnCore)
	}

	if infoFile != "" {
		infoWriter := SetRotateRule(infoFile, newName, maxSaveTime, rotationTime)
		bufferedInfoWriter := NewBufferedWriteSyncer(infoWriter)
		infoCore := zapcore.NewCore(GetConfig(), bufferedInfoWriter, zap.InfoLevel)
		cores = append(cores, infoCore)
	}

	if debugFile != "" {
		debugWriter := SetRotateRule(debugFile, newName, maxSaveTime, rotationTime)
		bufferedDebugWriter := NewBufferedWriteSyncer(debugWriter)
		debugCore := zapcore.NewCore(GetConfig(), bufferedDebugWriter, zap.DebugLevel)
		cores = append(cores, debugCore)
	}

	// 控制台输出使用无缓冲的写入器
	consoleCore := zapcore.NewCore(GetConfig(), zapcore.Lock(zapcore.AddSync(os.Stdout)), zap.DebugLevel)
	cores = append(cores, consoleCore)

	core := zapcore.NewTee(cores...)
	_logger := zap.New(core, zap.AddCaller())
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

const (
	bufferSize = 256 * 1024 // 256KB buffer
)

// BufferedWriteSyncer 带缓冲的写入器
type BufferedWriteSyncer struct {
	buffer chan []byte
	writer zapcore.WriteSyncer
}

// NewBufferedWriteSyncer 创建新的带缓冲的写入器
func NewBufferedWriteSyncer(writer zapcore.WriteSyncer) *BufferedWriteSyncer {
	ws := &BufferedWriteSyncer{
		buffer: make(chan []byte, bufferSize),
		writer: writer,
	}
	go ws.flushRoutine()
	return ws
}

// Write 实现 io.Writer
func (ws *BufferedWriteSyncer) Write(p []byte) (n int, err error) {
	// 复制数据以避免竞态条件
	data := make([]byte, len(p))
	copy(data, p)

	select {
	case ws.buffer <- data:
		return len(p), nil
	default:
		// 如果缓冲区满，直接写入
		return ws.writer.Write(p)
	}
}

// Sync 实现 zapcore.WriteSyncer
func (ws *BufferedWriteSyncer) Sync() error {
	return ws.writer.Sync()
}

// flushRoutine 异步刷新缓冲区
func (ws *BufferedWriteSyncer) flushRoutine() {
	for data := range ws.buffer {
		_, err := ws.writer.Write(data)
		if err != nil {
			zap.L().Error("Failed to write log", zap.Error(err))
		}
	}
}
