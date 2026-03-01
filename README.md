[English](./README.en.md) | 中文

# browserpm

一个生产就绪的浏览器页面池管理器，支持自动恢复。

## 特性

- **页面池化**: 可配置最小/最大页面数的预热页面池
- **轮询调度**: 在页面间公平分配操作
- **健康检查**: 自动检测并替换不健康的页面
- **TTL 回收**: 页面在可配置的 TTL 后被回收
- **上下文恢复**: 自动重建死亡的浏览器上下文
- **优雅关闭**: 关闭前排空活跃操作
- **进程监控**: 通过 CDP 跟踪 CPU/内存使用
- **并发安全**: 所有操作可安全并发使用

## 安装

```bash
go get github.com/souloss/browserpm
```

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/playwright-community/playwright-go"
    "github.com/souloss/browserpm"
)

func main() {
    // 使用选项创建管理器
    manager, err := browserpm.New(
        browserpm.WithHeadless(true),
        browserpm.WithAutoInstall(true),
        browserpm.WithMinPages(3),
        browserpm.WithMaxPages(10),
        browserpm.WithPoolTTL(30*time.Minute),
    )
    if err != nil {
        log.Fatalf("创建管理器失败: %v", err)
    }
    defer manager.Close()

    // 定义提供者
    contextProvider := browserpm.NewContextProvider(
        playwright.BrowserNewContextOptions{
            UserAgent: playwright.String("browserpm-example/1.0"),
        },
        func(ctx context.Context, bCtx playwright.BrowserContext) error {
            return nil // 上下文设置
        },
    )

    pageProvider := browserpm.NewPageProvider(
        func(ctx context.Context, page playwright.Page) error {
            _, err := page.Goto("https://example.com")
            return err
        },
        func(ctx context.Context, page playwright.Page) bool {
            return !page.IsClosed()
        },
    )

    // 创建会话
    session, err := manager.CreateSession("example", contextProvider, pageProvider)
    if err != nil {
        log.Fatalf("创建会话失败: %v", err)
    }

    ctx := context.Background()

    // 在独占（单次使用）页面上执行操作
    err = session.Do(ctx, func(page playwright.Page) error {
        title, err := page.Title()
        if err != nil {
            return err
        }
        fmt.Printf("标题: %s\n", title)
        return nil
    })

    // 在共享（池化）页面上执行操作
    err = session.DoShare(ctx, func(page playwright.Page) error {
        result, err := page.Evaluate(`() => document.title`)
        if err != nil {
            return err
        }
        fmt.Printf("标题: %v\n", result)
        return nil
    })
}
```

## 架构

```
BrowserManager
├── Browser (playwright.Browser)
├── CDPSession (进程监控)
└── Sessions (map[string]*Session)
    └── Session
        ├── BrowserContext (playwright.BrowserContext)
        └── PagePool
            ├── Scheduler (轮询)
            ├── HealthChecker (后台)
            └── Reaper (基于TTL)
```

## 配置

### 管理器选项

```go
manager, err := browserpm.New(
    // 浏览器选项
    browserpm.WithHeadless(true),
    browserpm.WithBrowserArgs("--no-sandbox", "--disable-gpu"),
    browserpm.WithBrowserTimeout(60*time.Second),

    // 安装选项
    browserpm.WithInstallPath("./playwright-driver"),
    browserpm.WithAutoInstall(true),
    browserpm.WithDeps(true),

    // 池选项
    browserpm.WithMinPages(1),
    browserpm.WithMaxPages(10),
    browserpm.WithPoolTTL(30*time.Minute),
    browserpm.WithGracePeriod(10*time.Second),
    browserpm.WithOperationTimeout(30*time.Second),
    browserpm.WithInitTimeout(30*time.Second),
    browserpm.WithHealthCheckInterval(30*time.Second),
    browserpm.WithScheduleStrategy("round-robin"),

    // 日志
    browserpm.WithLogger(logger),
)
```

### 会话选项（每个会话覆盖）

```go
session, err := manager.CreateSession("my-session", cp, pp,
    browserpm.WithSessionMinPages(5),
    browserpm.WithSessionMaxPages(20),
    browserpm.WithSessionTTL(1*time.Hour),
)
```

### 默认配置

| 选项 | 默认值 | 描述 |
|------|--------|------|
| `Headless` | `true` | 无头模式运行浏览器 |
| `Browser.Timeout` | `60s` | 浏览器启动超时 |
| `Install.Path` | `./playwright-driver` | Playwright 驱动路径 |
| `Install.Auto` | `true` | 启动时自动安装 |
| `Install.WithDeps` | `true` | 安装系统依赖 |
| `Pool.MinPages` | `1` | 最小预热页面数 |
| `Pool.MaxPages` | `10` | 最大页面数 |
| `Pool.TTL` | `30m` | 页面存活时间 |
| `Pool.GracePeriod` | `10s` | 强制关闭前的等待期 |
| `Pool.OperationTimeout` | `30s` | 操作超时 |
| `Pool.InitTimeout` | `30s` | 页面初始化超时 |
| `Pool.HealthCheckInterval` | `30s` | 健康检查间隔 |
| `Pool.ScheduleStrategy` | `round-robin` | 调度策略 |

## 提供者

### ContextProvider

控制 `BrowserContext` 的创建和配置：

```go
type ContextProvider interface {
    Options() playwright.BrowserNewContextOptions
    Setup(ctx context.Context, bCtx playwright.BrowserContext) error
}
```

### PageProvider

控制页面初始化和健康检查：

```go
type PageProvider interface {
    Init(ctx context.Context, page playwright.Page) error
    Check(ctx context.Context, page playwright.Page) bool
}
```

## 操作

### Do（独占页面）

创建新页面，运行操作，然后关闭页面。在上下文/页面失败时自动重试。

```go
err := session.Do(ctx, func(page playwright.Page) error {
    return page.Goto("https://example.com")
})
```

### DoShare（池化页面）

使用共享池中的页面。操作后页面保留在池中。自动重试并替换不健康的页面。

```go
err := session.DoShare(ctx, func(page playwright.Page) error {
    result, _ := page.Evaluate(`() => document.title`)
    return nil
})
```

## 错误处理

库使用带有错误码的结构化错误：

```go
import "errors"

err := session.Do(ctx, op)
if errors.Is(err, browserpm.ErrClosedErr) {
    // 会话已关闭
}
if errors.Is(err, browserpm.ErrContextDeadErr) {
    // 上下文已死亡（尝试自动恢复）
}
if errors.Is(err, browserpm.ErrPoolExhaustedErr) {
    // 池已达最大容量，无可用页面
}
```

### 错误码

| 错误码 | 描述 |
|--------|------|
| `ErrSessionNotFound` | 会话不存在 |
| `ErrSessionExists` | 会话已存在 |
| `ErrPoolExhausted` | 池已达最大容量 |
| `ErrContextDead` | 浏览器上下文已死亡 |
| `ErrPageUnavailable` | 创建/访问页面失败 |
| `ErrTimeout` | 操作超时 |
| `ErrClosed` | 管理器/会话已关闭 |
| `ErrInvalidState` | 无效的内部状态 |
| `ErrInternal` | 内部错误 |

## 全局单例

对于简单用例，使用全局单例：

```go
// 配置（必须在第一次 Global() 调用之前）
browserpm.SetGlobalOptions(
    browserpm.WithHeadless(true),
    browserpm.WithMinPages(3),
)

// 使用全局函数
session, err := browserpm.GCreateSession("my-session", cp, pp)
err = browserpm.GCloseSession("my-session")
infos, _ := browserpm.GListSessions()

// 完成后关闭
browserpm.Shutdown()
```

## 进程监控

获取所有浏览器进程的 CPU/内存使用情况：

```go
infos, err := manager.GetProcessInfos(ctx)
for _, pi := range infos {
    fmt.Printf("PID %d (%s): RSS=%dMB, CPU=%.2f\n",
        pi.ID, pi.Type, pi.RSS/1024/1024, pi.CPU)
}
```

## 会话状态

```go
info := session.Status()
fmt.Printf("会话: %s, 状态: %s, 页面数: %d, 活跃操作: %d\n",
    info.Name, info.State, info.PageCount, info.ActiveOps)

// 列出所有会话
for _, s := range manager.ListSessions() {
    fmt.Printf("- %s (%s)\n", s.Name, s.State)
}
```

## 日志

库使用结构化日志接口。默认使用 Zap 日志。

```go
// 自定义日志
logger := browserpm.NewZapLoggerWithConfig(true) // 调试模式
manager, _ := browserpm.New(browserpm.WithLogger(logger))

// 空操作日志（禁用日志）
manager, _ := browserpm.New(browserpm.WithLogger(browserpm.NewNopLogger()))
```

## 线程安全

所有导出方法都可安全并发使用：

- `BrowserManager` 使用 `sync.Map` 管理会话
- `Session` 使用 `sync.RWMutex` 保护状态
- `PagePool` 在热路径上使用原子操作
- `Scheduler` 使用原子计数器进行轮询

## 生命周期

1. **管理器创建**: `New()` 安装驱动、启动浏览器、建立 CDP
2. **会话创建**: `CreateSession()` 注册会话（上下文/池延迟创建）
3. **首次操作**: 在第一次 `Do`/`DoShare` 时创建上下文和池
4. **健康检查**: 后台 goroutine 检查页面健康状态
5. **TTL 回收**: TTL 过期后页面被回收
6. **恢复**: 死亡的上下文/页面自动重建
7. **关闭**: `Close()` 排空活跃操作，关闭页面、上下文、浏览器

## 许可证

MIT