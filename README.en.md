# browserpm

[English](./README.en.md) | 中文


A production-ready browser page pool manager with auto-recovery.

## Features

- **Page Pooling**: Pre-warmed page pool with configurable min/max pages
- **Round-Robin Scheduling**: Fair distribution of operations across pages
- **Health Checking**: Automatic detection and replacement of unhealthy pages
- **TTL-based Recycling**: Pages are recycled after configurable TTL
- **Context Recovery**: Automatic rebuild of dead browser contexts
- **Graceful Shutdown**: Drain active operations before closing
- **Process Monitoring**: CPU/Memory tracking via CDP
- **Concurrent Safe**: All operations safe for concurrent use

## Installation

```bash
go get github.com/souloss/browserpm
```

## Quick Start

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
    // Create a manager with options
    manager, err := browserpm.New(
        browserpm.WithHeadless(true),
        browserpm.WithAutoInstall(true),
        browserpm.WithMinPages(3),
        browserpm.WithMaxPages(10),
        browserpm.WithPoolTTL(30*time.Minute),
    )
    if err != nil {
        log.Fatalf("failed to create manager: %v", err)
    }
    defer manager.Close()

    // Define providers
    contextProvider := browserpm.NewContextProvider(
        playwright.BrowserNewContextOptions{
            UserAgent: playwright.String("browserpm-example/1.0"),
        },
        func(ctx context.Context, bCtx playwright.BrowserContext) error {
            return nil // context setup
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

    // Create a session
    session, err := manager.CreateSession("example", contextProvider, pageProvider)
    if err != nil {
        log.Fatalf("failed to create session: %v", err)
    }

    ctx := context.Background()

    // Execute operation on exclusive (single-use) page
    err = session.Do(ctx, func(page playwright.Page) error {
        title, err := page.Title()
        if err != nil {
            return err
        }
        fmt.Printf("Title: %s\n", title)
        return nil
    })

    // Execute operation on shared (pooled) page
    err = session.DoShare(ctx, func(page playwright.Page) error {
        result, err := page.Evaluate(`() => document.title`)
        if err != nil {
            return err
        }
        fmt.Printf("Title: %v\n", result)
        return nil
    })
}
```

## Architecture

```
BrowserManager
├── Browser (playwright.Browser)
├── CDPSession (process monitoring)
└── Sessions (map[string]*Session)
    └── Session
        ├── BrowserContext (playwright.BrowserContext)
        └── PagePool
            ├── Scheduler (Round-Robin)
            ├── HealthChecker (background)
            └── Reaper (TTL-based)
```

## Configuration

### Manager Options

```go
manager, err := browserpm.New(
    // Browser options
    browserpm.WithHeadless(true),
    browserpm.WithBrowserArgs("--no-sandbox", "--disable-gpu"),
    browserpm.WithBrowserTimeout(60*time.Second),

    // Install options
    browserpm.WithInstallPath("./playwright-driver"),
    browserpm.WithAutoInstall(true),
    browserpm.WithDeps(true),

    // Pool options
    browserpm.WithMinPages(1),
    browserpm.WithMaxPages(10),
    browserpm.WithPoolTTL(30*time.Minute),
    browserpm.WithGracePeriod(10*time.Second),
    browserpm.WithOperationTimeout(30*time.Second),
    browserpm.WithInitTimeout(30*time.Second),
    browserpm.WithHealthCheckInterval(30*time.Second),
    browserpm.WithScheduleStrategy("round-robin"),

    // Logger
    browserpm.WithLogger(logger),
)
```

### Session Options (Per-Session Overrides)

```go
session, err := manager.CreateSession("my-session", cp, pp,
    browserpm.WithSessionMinPages(5),
    browserpm.WithSessionMaxPages(20),
    browserpm.WithSessionTTL(1*time.Hour),
)
```

### Default Configuration

| Option | Default | Description |
|--------|---------|-------------|
| `Headless` | `true` | Run browser in headless mode |
| `Browser.Timeout` | `60s` | Browser launch timeout |
| `Install.Path` | `./playwright-driver` | Playwright driver path |
| `Install.Auto` | `true` | Auto-install on startup |
| `Install.WithDeps` | `true` | Install system dependencies |
| `Pool.MinPages` | `1` | Minimum pre-warmed pages |
| `Pool.MaxPages` | `10` | Maximum pages allowed |
| `Pool.TTL` | `30m` | Page time-to-live |
| `Pool.GracePeriod` | `10s` | Grace period before force-close |
| `Pool.OperationTimeout` | `30s` | Operation timeout |
| `Pool.InitTimeout` | `30s` | Page init timeout |
| `Pool.HealthCheckInterval` | `30s` | Health check interval |
| `Pool.ScheduleStrategy` | `round-robin` | Scheduling strategy |

## Providers

### ContextProvider

Controls how `BrowserContext` is created and configured:

```go
type ContextProvider interface {
    Options() playwright.BrowserNewContextOptions
    Setup(ctx context.Context, bCtx playwright.BrowserContext) error
}
```

### PageProvider

Controls page initialization and health checking:

```go
type PageProvider interface {
    Init(ctx context.Context, page playwright.Page) error
    Check(ctx context.Context, page playwright.Page) bool
}
```

## Operations

### Do (Exclusive Page)

Creates a new page, runs the operation, and closes the page. Automatically retries on context/page failures.

```go
err := session.Do(ctx, func(page playwright.Page) error {
    return page.Goto("https://example.com")
})
```

### Manager.Do (One-off Operation)

Execute a one-off operation directly on the Manager without creating a Session. Each call creates a temporary BrowserContext and Page, which are cleaned up after the operation.

Use cases:
- Different storageState per operation (e.g., different accounts)
- One-off operations that don't need page reuse
- Fully isolated context configuration

```go
// Simple: no configuration
err := manager.Do(ctx, playwright.BrowserNewContextOptions{
    UserAgent: playwright.String("browserpm-test/1.0"),
}, func(page playwright.Page) error {
    _, err := page.Goto("https://example.com")
    return err
})
```

**Full configuration:**

```go
err := manager.Do(ctx, playwright.BrowserNewContextOptions{
    StorageState: storageState, // obtained from elsewhere
    UserAgent:    playwright.String("my-agent"),
    Viewport:     &playwright.Size{Width: 1920, Height: 1080},
}, func(page playwright.Page) error {
    _, err := page.Goto("https://example.com")
    return err
})
```

### DoShare (Pooled Page)

Uses a page from the shared pool. Page remains in pool after operation. Automatically retries and replaces unhealthy pages.

```go
err := session.DoShare(ctx, func(page playwright.Page) error {
    result, _ := page.Evaluate(`() => document.title`)
    return nil
})
```

## Error Handling

The library uses structured errors with error codes:

```go
import "errors"

err := session.Do(ctx, op)
if errors.Is(err, browserpm.ErrClosedErr) {
    // session is closed
}
if errors.Is(err, browserpm.ErrContextDeadErr) {
    // context is dead (automatic recovery attempted)
}
if errors.Is(err, browserpm.ErrPoolExhaustedErr) {
    // pool at max capacity, no available pages
}
```

### Error Codes

| Code | Description |
|------|-------------|
| `ErrSessionNotFound` | Session does not exist |
| `ErrSessionExists` | Session already exists |
| `ErrPoolExhausted` | Pool at max capacity |
| `ErrContextDead` | Browser context is dead |
| `ErrPageUnavailable` | Failed to create/access page |
| `ErrTimeout` | Operation timed out |
| `ErrClosed` | Manager/session is closed |
| `ErrInvalidState` | Invalid internal state |
| `ErrInternal` | Internal error |

## Global Singleton

For simple use cases, use the global singleton:

```go
// Configure (must be before first Global() call)
browserpm.SetGlobalOptions(
    browserpm.WithHeadless(true),
    browserpm.WithMinPages(3),
)

// Use global functions
session, err := browserpm.GCreateSession("my-session", cp, pp)
err = browserpm.GCloseSession("my-session")
infos, _ := browserpm.GListSessions()

// Shutdown when done
browserpm.Shutdown()
```

## Process Monitoring

Get CPU/Memory usage for all browser processes:

```go
infos, err := manager.GetProcessInfos(ctx)
for _, pi := range infos {
    fmt.Printf("PID %d (%s): RSS=%dMB, CPU=%.2f\n",
        pi.ID, pi.Type, pi.RSS/1024/1024, pi.CPU)
}
```

## Session Status

```go
info := session.Status()
fmt.Printf("Session: %s, State: %s, Pages: %d, ActiveOps: %d\n",
    info.Name, info.State, info.PageCount, info.ActiveOps)

// List all sessions
for _, s := range manager.ListSessions() {
    fmt.Printf("- %s (%s)\n", s.Name, s.State)
}
```

## Logging

The library uses a structured logging interface. Default is Zap logger.

```go
// Custom logger
logger := browserpm.NewZapLoggerWithConfig(true) // debug mode
manager, _ := browserpm.New(browserpm.WithLogger(logger))

// No-op logger (disable logging)
manager, _ := browserpm.New(browserpm.WithLogger(browserpm.NewNopLogger()))
```

## Thread Safety

All exported methods are safe for concurrent use:

- `BrowserManager` uses `sync.Map` for sessions
- `Session` uses `sync.RWMutex` for state protection
- `PagePool` uses atomic operations on the hot path
- `Scheduler` uses atomic counter for round-robin

## Lifecycle

1. **Manager Creation**: `New()` installs driver, launches browser, establishes CDP
2. **Session Creation**: `CreateSession()` registers session (context/pool created lazily)
3. **First Operation**: Context and pool are created on first `Do`/`DoShare`
4. **Health Checking**: Background goroutine checks page health
5. **TTL Reaping**: Pages are recycled after TTL expires
6. **Recovery**: Dead contexts/pages are automatically rebuilt
7. **Shutdown**: `Close()` drains active ops, closes pages, contexts, browser

## License

MIT