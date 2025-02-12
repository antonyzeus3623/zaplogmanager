# ZapLogManager - 智能日志管理系统

 [![License: MIT](../../AA%20%E8%B5%84%E6%96%99/assets/README.assets/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT) 基于Zap的增强型日志管理解决方案，提供专业级的日志切割、压缩和清理功能。 

## 功能亮点 

### 核心特性 

- 分级日志管理：

  支持Warn/Info/Debug多级别日志分离存储 

- 智能切割机制： 

  按时间间隔自动切割（默认24小时，支持自定义）  

  支持自定义保留策略（默认保留5天） 

- 自动压缩优化：

  当日志文件超过当日范围时自动GZIP压缩 

  压缩文件保留时间可配置（默认30天） 

- 安全防护机制： 

  文件锁定检测（避免压缩正在写入的文件）  

  双重存在性校验

  完善的错误处理链路 

### 高级功能 

- 实时控制台日志输出 

- 支持绝对路径/相对路径 

- 异步压缩处理（基于goroutine） 

- 精确的日期匹配正则引擎 

- 详细的调试日志记录



## 快速开始 

### 安装依赖 

```bash 
go get -u github.com/antonyzeus3623/zaplogmanager
```

### 基础用法示例

```go
package main

import (
	"time"
	"github.com/antonyzeus3623/zaplogmanager"
)

func main() {
	// 初始化日志系统
	zaplogmanager.InitLogger(
		"./logs/warn.log",   // 错误日志路径
		"./logs/info.log",   // 信息日志路径
		"./logs/debug.log",  // 调试日志路径
		"-%Y%m%d.log",       // 时间格式模板
		120*time.Hour,       // 原始日志保留5天
		24*time.Hour,        // 每日切割
		720*time.Hour,       // 压缩文件保留30天
	)

	// 使用示例
	zaplogmanager.GetLogger().Warn("系统警告示例")
	zaplogmanager.GetLogger().Info("服务启动成功")
	zaplogmanager.GetLogger().Debug("调试信息示例")
}
```

### 高级配置示例

```go
// 自定义日志编码配置
func customEncoder() zapcore.Encoder {
	return zaplogmanager.GetConfig().
		WithColorLevels().
		WithStacktrace(zapcore.ErrorLevel)
}

// 初始化带自定义配置的日志器
zaplogmanager.InitLoggerWithConfig(
	"./logs/audit.log",
	"",
	"./logs/trace.log",
	"-%Y%m%d%H.log",       // 每小时切割
	48*time.Hour,          // 保留2天原始日志
	1*time.Hour,           // 每小时切割
	240*time.Hour,         // 压缩保留10天
	customEncoder,
)
```



## 文件命名规范

支持的日志格式示例：

- `service-20230228.log`
- `debug_20230228.log.gz`
- `app.2023-02-28.log`



## 最佳实践建议

1.  **生产环境配置**：

```go
// 推荐生产环境配置参数
zaplogmanager.InitLogger(
	"/var/log/app/error.log",
	"/var/log/app/info.log",
	"",  // 关闭调试日志
	"-%Y%m%d.log",
	168*time.Hour,   // 保留7天
	24*time.Hour,
	720*time.Hour,   // 压缩保留30天
)
```

2.  **开发环境配置**：

```go
// 开发环境推荐配置
zaplogmanager.InitLogger(
	"",
	"./dev_info.log",
	"./dev_debug.log",
	"-%Y%m%d.log",
	24*time.Hour,    // 保留1天
	12*time.Hour,    // 每12小时切割
	168*time.Hour,   // 保留7天
)
```



## 技术参数说明

| 配置项          | 类型          | 说明             | 默认值      |
| --------------- | ------------- | ---------------- | ----------- |
| maxSaveTime     | time.Duration | 原始日志保留时间 | 120h (5天)  |
| rotationTime    | time.Duration | 日志切割间隔     | 24h         |
| compressMaxSave | time.Duration | 压缩文件保留时间 | 720h (30天) |
| newName         | string        | 时间格式模板     | -%Y%m%d.log |



## 维护策略

- **压缩执行时间**：每日凌晨1点（可通过StartLogCompression参数调整）
- **文件检查频率**：每小时扫描目录
- **并发控制**：使用sync.Mutex保证文件操作原子性



## 问题排查指南

```bash
# 启用调试模式
export ZAPLOG_DEBUG=1

# 查看典型日志输出
[2023-03-01T01:00:00Z] DEBUG 开始处理目录: [/var/log/app]
[2023-03-01T01:00:03Z] INFO  已压缩: /var/log/app/access-20230228.log → 8.2MB
[2023-03-01T01:00:05Z] INFO  清理过期文件: removed 3 files (total 42MB)
```



## 贡献指南

欢迎通过Issue提交改进建议或通过PR贡献代码：

1. Fork仓库
2. 创建特性分支（git checkout -b feature/improvement）
3. 提交修改（git commit -am 'Add some feature'）
4. 推送分支（git push origin feature/improvement）
5. 创建Pull Request



***v0.4.0功能***

- 不同级别的日志输出到不同的日志文件 

- 日志文件可按照文件大小或日期进行切割存储

- 一次定义全局使用

***v0.4.0 使用示例：***

```go
package main

import (
	"time"
	"github.com/antonyzeus3623/zaplogmanager"
)

func main() {
    warnFile := "./log/warn/warn.log"
    infoFile := "./log/info/info.log"
    debugFile := "./log/cim.log"
    newName := "-%Y%m%d.log"
    maxSaveTime := time.Hour*24*30
    rotationTime := time.Hour*24

    zaplogmanager.InitLogger(warnFile, infoFile, debugFile, newName, maxSaveTime, rotationTime)
}
```
