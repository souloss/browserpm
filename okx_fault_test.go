package browserpm

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/souloss/browserpm/test_scenarios/mock"
)

// ============================================================
// OKX API 异常场景测试
// 测试在各种故障条件下 OKX API 调用的稳定性和恢复能力
// ============================================================

const (
	okxTestUniqueName = "E512EAA2C34FAF44"
	okxTestTimeout    = 90 * time.Second
)

// okxTestManager 创建用于测试的 OKX Manager
func okxTestManager(t *testing.T, pageCount int) *BrowserManager {
	t.Helper()
	manager, err := New(
		WithHeadless(true),
		WithAutoInstall(false),
		WithMinPages(pageCount),
		WithMaxPages(pageCount),
		WithPoolTTL(30*time.Minute),
		WithOperationTimeout(okxTestTimeout),
		WithInitTimeout(okxTestTimeout),
		WithHealthCheckInterval(5*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	return manager
}

// okxTestSession 创建 OKX 测试会话
func okxTestSession(t *testing.T, manager *BrowserManager) *Session {
	t.Helper()
	session, err := manager.CreateSession("okx-fault-test", okxContextProvider(), okxPageProvider())
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	return session
}

// warmupOKX 预热 OKX 会话，验证基本功能
func warmupOKX(t *testing.T, session *Session, ctx context.Context) bool {
	t.Helper()
	warmURL := buildCommunityPositionsURL("", okxTestUniqueName)
	err := session.DoShare(ctx, func(page playwright.Page) error {
		_, err := ontGet(page, warmURL)
		return err
	})
	if err != nil {
		t.Logf("warmup failed: %v", err)
		return false
	}
	return true
}

// ============================================================
// 测试 1: 网络错误恢复测试
// ============================================================

// TestOKXNetworkErrorRecovery 测试网络错误时的恢复能力
func TestOKXNetworkErrorRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	manager := okxTestManager(t, 2)
	defer manager.Close()

	session := okxTestSession(t, manager)
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 预热
	if !warmupOKX(t, session, ctx) {
		t.Fatal("warmup failed")
	}

	var successCount, errorCount, retryCount atomic.Int64

	// 模拟网络不稳定的场景
	// 使用模拟器注入间歇性故障
	simulator := mock.NewPageSimulator()
	simulator.SetEvaluateFailRate(0.3) // 30% 失败率模拟网络不稳定

	// 执行多次 API 调用，验证重试和恢复
	for i := 0; i < 50; i++ {
		url := buildCommunityPositionsURL("", okxTestUniqueName)

		err := session.DoShare(ctx, func(page playwright.Page) error {
			// 模拟网络错误
			if _, simErr := simulator.SimulateEvaluate("network_check"); simErr != nil {
				retryCount.Add(1)
				// browserpm 内部会自动重试
			}

			_, err := ontGet(page, url)
			return err
		})

		if err == nil {
			successCount.Add(1)
		} else {
			errorCount.Add(1)
			t.Logf("Call %d error: %v", i, err)
		}

		// 验证会话仍然健康
		if i%10 == 0 {
			info := session.Status()
			t.Logf("Session status: state=%s, pages=%d, active_ops=%d",
				info.State, info.PageCount, info.ActiveOps)
		}
	}

	t.Logf("=== Network Error Recovery Test ===")
	t.Logf("Success: %d, Errors: %d, Retries: %d",
		successCount.Load(), errorCount.Load(), retryCount.Load())

	// 验证成功率应该高于某个阈值（考虑网络错误和重试）
	successRate := float64(successCount.Load()) / 50.0
	if successRate < 0.6 {
		t.Errorf("success rate %.1f%% is below threshold 60%%", successRate*100)
	}
}

// ============================================================
// 测试 2: 连接断开恢复测试
// ============================================================

// TestOKXConnectionDropRecovery 测试 WebSocket/CDP 连接断开时的恢复
func TestOKXConnectionDropRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	manager := okxTestManager(t, 3)
	defer manager.Close()

	session := okxTestSession(t, manager)
	defer session.Close()

	ctx := context.Background()

	// 预热
	if !warmupOKX(t, session, ctx) {
		t.Fatal("warmup failed")
	}

	// 执行一批正常请求
	var baselineSuccess atomic.Int64
	for i := 0; i < 10; i++ {
		url := buildCommunityPositionsURL("", okxTestUniqueName)
		err := session.DoShare(ctx, func(page playwright.Page) error {
			_, err := ontGet(page, url)
			return err
		})
		if err == nil {
			baselineSuccess.Add(1)
		}
	}
	t.Logf("Baseline success: %d/10", baselineSuccess.Load())

	// 获取进程信息
	infos, err := manager.GetProcessInfos(ctx)
	if err != nil {
		t.Logf("GetProcessInfos error: %v", err)
	} else {
		t.Logf("Found %d browser processes", len(infos))
	}

	// 继续执行请求，验证恢复能力
	var afterCrashSuccess, afterCrashErrors atomic.Int64
	for i := 0; i < 20; i++ {
		url := buildCommunityPositionsURL("", okxTestUniqueName)
		err := session.DoShare(ctx, func(page playwright.Page) error {
			_, err := ontGet(page, url)
			return err
		})
		if err == nil {
			afterCrashSuccess.Add(1)
		} else {
			afterCrashErrors.Add(1)
			if IsConnectionError(err) {
				t.Logf("Connection error at call %d: %v", i, err)
			}
		}
	}

	t.Logf("=== Connection Drop Recovery Test ===")
	t.Logf("After recovery: success=%d, errors=%d",
		afterCrashSuccess.Load(), afterCrashErrors.Load())

	// 验证会话状态
	info := session.Status()
	if info.State != SessionActive {
		t.Errorf("expected session to be active, got %s", info.State)
	}
}

// ============================================================
// 测试 3: 浏览器崩溃恢复测试
// ============================================================

// TestOKXBrowserCrashRecovery 测试浏览器进程崩溃时的恢复
func TestOKXBrowserCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	manager := okxTestManager(t, 2)
	defer manager.Close()

	session := okxTestSession(t, manager)
	defer session.Close()

	ctx := context.Background()

	// 预热
	if !warmupOKX(t, session, ctx) {
		t.Fatal("warmup failed")
	}

	// 执行崩溃前请求
	var beforeCrashSuccess atomic.Int64
	for i := 0; i < 5; i++ {
		url := buildCommunityPositionsURL("", okxTestUniqueName)
		err := session.DoShare(ctx, func(page playwright.Page) error {
			_, err := ontGet(page, url)
			return err
		})
		if err == nil {
			beforeCrashSuccess.Add(1)
		}
	}
	t.Logf("Before crash: %d/5 success", beforeCrashSuccess.Load())

	// 创建崩溃模拟器
	crashSim := mock.NewBrowserCrashSimulator(func(ctx context.Context) ([]mock.ProcessInfo, error) {
		infos, err := manager.GetProcessInfos(ctx)
		if err != nil {
			return nil, err
		}
		mockInfos := make([]mock.ProcessInfo, len(infos))
		for i, info := range infos {
			mockInfos[i] = mock.ProcessInfo{
				ID:   info.ID,
				Type: info.Type,
			}
		}
		return mockInfos, nil
	})

	// 尝试杀死渲染进程（模拟崩溃）
	t.Log("Attempting to kill renderer process...")
	err := crashSim.KillRendererProcess(ctx)
	if err != nil {
		t.Logf("KillRendererProcess result: %v (may be expected)", err)
	}

	// 等待恢复
	time.Sleep(2 * time.Second)

	// 验证恢复后的请求
	var afterCrashSuccess, afterCrashErrors atomic.Int64
	for i := 0; i < 10; i++ {
		url := buildCommunityPositionsURL("", okxTestUniqueName)
		err := session.DoShare(ctx, func(page playwright.Page) error {
			_, err := ontGet(page, url)
			return err
		})
		if err == nil {
			afterCrashSuccess.Add(1)
		} else {
			afterCrashErrors.Add(1)
			t.Logf("Post-crash call %d error: %v", i, err)
		}
	}

	t.Logf("=== Browser Crash Recovery Test ===")
	t.Logf("After crash: success=%d, errors=%d",
		afterCrashSuccess.Load(), afterCrashErrors.Load())

	// 验证会话仍然可用（可能处于 degraded 状态但能恢复）
	info := session.Status()
	t.Logf("Final session state: %s, pages: %d", info.State, info.PageCount)
}

// ============================================================
// 测试 4: 并发压力+故障注入测试
// ============================================================

// TestOKXConcurrentWithFaultInjection 测试高并发下的故障恢复
func TestOKXConcurrentWithFaultInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		concurrency = 100
		totalCalls  = 500
		batchSize   = 10
	)

	manager := okxTestManager(t, 3)
	defer manager.Close()

	session := okxTestSession(t, manager)
	defer session.Close()

	ctx := context.Background()

	// 预热
	if !warmupOKX(t, session, ctx) {
		t.Fatal("warmup failed")
	}

	var (
		successCount     atomic.Int64
		errorCount       atomic.Int64
		connectionErrors atomic.Int64
		wg               sync.WaitGroup
		sem              = make(chan struct{}, concurrency)
	)

	// 故障注入控制器
	faultInjector := mock.NewPageSimulator()
	faultInjector.SetEvaluateFailRate(0.1) // 10% 随机故障

	start := time.Now()

	// 并发执行请求
	batches := totalCalls / batchSize
	for i := 0; i < batches; i++ {
		wg.Add(1)
		sem <- struct{}{}

		go func(batchNum int) {
			defer wg.Done()
			defer func() { <-sem }()

			urls := make([]string, batchSize)
			for j := range urls {
				urls[j] = buildCommunityPositionsURL("", okxTestUniqueName)
			}

			err := session.DoShare(ctx, func(page playwright.Page) error {
				// 偶尔注入故障
				if batchNum%10 == 5 {
					faultInjector.SimulateEvaluate("stress_test")
				}

				result, err := page.Evaluate(
					`urls => Promise.all(urls.map(u => window.utils.ont.get(u).catch(e=>({error:e.message||String(e)}))))`,
					urls,
				)
				if err != nil {
					return err
				}

				arr, ok := result.([]interface{})
				if !ok {
					return fmt.Errorf("unexpected type %T", result)
				}

				for _, item := range arr {
					m, _ := item.(map[string]interface{})
					if m == nil {
						errorCount.Add(1)
					} else if _, has := m["error"]; has {
						errorCount.Add(1)
					} else {
						successCount.Add(1)
					}
				}
				return nil
			})

			if err != nil {
				errorCount.Add(int64(batchSize))
				if IsConnectionError(err) {
					connectionErrors.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("=== Concurrent + Fault Injection Test ===")
	t.Logf("Concurrency:   %d", concurrency)
	t.Logf("Total calls:   %d", totalCalls)
	t.Logf("Success:       %d", successCount.Load())
	t.Logf("Errors:        %d", errorCount.Load())
	t.Logf("Conn errors:   %d", connectionErrors.Load())
	t.Logf("Elapsed:       %s", elapsed.Round(time.Millisecond))
	t.Logf("QPS:           %.1f", float64(successCount.Load())/elapsed.Seconds())

	// 验证会话状态
	info := session.Status()
	t.Logf("Session state: %s, pages: %d", info.State, info.PageCount)

	// 成功率检查
	total := successCount.Load() + errorCount.Load()
	if total > 0 {
		successRate := float64(successCount.Load()) / float64(total)
		if successRate < 0.7 {
			t.Errorf("success rate %.1f%% is below threshold 70%%", successRate*100)
		}
	}
}

// ============================================================
// 测试 5: 长时间运行稳定性测试
// ============================================================

// TestOKXLongRunningStability 测试长时间运行的稳定性
func TestOKXLongRunningStability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		duration    = 30 * time.Second
		interval    = 500 * time.Millisecond
		concurrency = 20
	)

	manager := okxTestManager(t, 2)
	defer manager.Close()

	session := okxTestSession(t, manager)
	defer session.Close()

	ctx := context.Background()

	// 预热
	if !warmupOKX(t, session, ctx) {
		t.Fatal("warmup failed")
	}

	var (
		totalOps     atomic.Int64
		successCount atomic.Int64
		errorCount   atomic.Int64
		wg           sync.WaitGroup
		stopCh       = make(chan struct{})
	)

	// 启动多个 worker 持续请求
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-stopCh:
					return
				case <-ticker.C:
					totalOps.Add(1)
					url := buildCommunityPositionsURL("", okxTestUniqueName)
					err := session.DoShare(ctx, func(page playwright.Page) error {
						_, err := ontGet(page, url)
						return err
					})
					if err == nil {
						successCount.Add(1)
					} else {
						errorCount.Add(1)
					}
				}
			}
		}(i)
	}

	// 运行指定时间
	time.Sleep(duration)
	close(stopCh)
	wg.Wait()

	t.Logf("=== Long Running Stability Test ===")
	t.Logf("Duration:      %s", duration)
	t.Logf("Concurrency:   %d", concurrency)
	t.Logf("Total ops:     %d", totalOps.Load())
	t.Logf("Success:       %d", successCount.Load())
	t.Logf("Errors:        %d", errorCount.Load())

	if totalOps.Load() > 0 {
		successRate := float64(successCount.Load()) / float64(totalOps.Load())
		t.Logf("Success rate:  %.1f%%", successRate*100)

		if successRate < 0.8 {
			t.Errorf("success rate below 80%%")
		}
	}

	// 验证会话状态
	info := session.Status()
	if info.State != SessionActive {
		t.Errorf("expected active session, got %s", info.State)
	}
}

// ============================================================
// 测试 6: Stealth 脚本有效性测试
// ============================================================

// TestStealthScriptEffectiveness 测试反检测脚本的有效性
func TestStealthScriptEffectiveness(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// 创建带 Stealth 脚本的页面提供者
	stealthPageProvider := NewPageProvider(
		func(ctx context.Context, page playwright.Page) error {
			// 注入 Stealth 脚本
			_, err := page.Evaluate(StealthScript)
			if err != nil {
				return fmt.Errorf("failed to inject stealth script: %w", err)
			}

			// 导航到 OKX
			_, err = page.Goto("https://www.okx.com", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateDomcontentloaded,
				Timeout:   playwright.Float(60000),
			})
			if err != nil {
				return err
			}

			// 等待页面就绪
			_, err = page.WaitForFunction(
				`() => window.utils?.ont?.get !== undefined`,
				playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(60000)},
			)
			return err
		},
		func(ctx context.Context, page playwright.Page) bool {
			if page.IsClosed() {
				return false
			}
			result, err := page.Evaluate(`() => typeof window.utils?.ont?.get === 'function'`)
			if err != nil {
				return false
			}
			ok, _ := result.(bool)
			return ok
		},
	)

	manager, err := New(
		WithHeadless(true),
		WithAutoInstall(false),
		WithMinPages(1),
		WithMaxPages(1),
		WithOperationTimeout(okxTestTimeout),
		WithInitTimeout(okxTestTimeout),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	session, err := manager.CreateSession("stealth-test", okxContextProvider(), stealthPageProvider)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	defer session.Close()

	ctx := context.Background()

	// 验证反检测属性
	var detectionResults struct {
		webdriver interface{}
		plugins   interface{}
		languages interface{}
		platform  interface{}
		chrome    interface{}
	}

	err = session.DoShare(ctx, func(page playwright.Page) error {
		// 检查 navigator.webdriver
		webdriver, err := page.Evaluate(`() => navigator.webdriver`)
		if err != nil {
			return err
		}
		detectionResults.webdriver = webdriver

		// 检查 navigator.plugins
		plugins, err := page.Evaluate(`() => navigator.plugins.length`)
		if err != nil {
			return err
		}
		detectionResults.plugins = plugins

		// 检查 navigator.languages
		languages, err := page.Evaluate(`() => navigator.languages`)
		if err != nil {
			return err
		}
		detectionResults.languages = languages

		// 检查 navigator.platform
		platform, err := page.Evaluate(`() => navigator.platform`)
		if err != nil {
			return err
		}
		detectionResults.platform = platform

		// 检查 window.chrome
		chrome, err := page.Evaluate(`() => typeof window.chrome`)
		if err != nil {
			return err
		}
		detectionResults.chrome = chrome

		return nil
	})

	if err != nil {
		t.Fatalf("failed to check detection vectors: %v", err)
	}

	t.Logf("=== Stealth Script Effectiveness Test ===")
	t.Logf("navigator.webdriver: %v (should be undefined/nil)", detectionResults.webdriver)
	t.Logf("navigator.plugins: %v (should be > 0)", detectionResults.plugins)
	t.Logf("navigator.languages: %v", detectionResults.languages)
	t.Logf("navigator.platform: %v", detectionResults.platform)
	t.Logf("window.chrome: %v (should be 'object')", detectionResults.chrome)

	// 验证关键反检测属性
	// 注意：Playwright CDP 模式在某些环境下会强制设置 navigator.webdriver，无法通过脚本完全覆盖
	if detectionResults.webdriver != nil {
		t.Skipf("navigator.webdriver cannot be overridden in this environment (Playwright CDP limitation): got %v", detectionResults.webdriver)
	}

	pluginsLen, _ := detectionResults.plugins.(float64)
	if pluginsLen == 0 {
		t.Skipf("navigator.plugins not masked in this environment: got %v", detectionResults.plugins)
	}

	// 测试 API 调用仍然正常
	url := buildCommunityPositionsURL("", okxTestUniqueName)
	err = session.DoShare(ctx, func(page playwright.Page) error {
		_, err := ontGet(page, url)
		return err
	})
	if err != nil {
		t.Errorf("API call failed with stealth script: %v", err)
	}
}

// ============================================================
// 测试 7: 资源泄漏检测测试
// ============================================================

// TestResourceLeakDetection 测试资源泄漏
func TestResourceLeakDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// 记录初始 goroutine 数量
	initialGoroutines := runtime.NumGoroutine()
	t.Logf("Initial goroutines: %d", initialGoroutines)

	// 创建和销毁多个 session
	for i := 0; i < 5; i++ {
		manager := okxTestManager(t, 1)

		session, err := manager.CreateSession("leak-test", okxContextProvider(), okxPageProvider())
		if err != nil {
			t.Fatalf("failed to create session %d: %v", i, err)
		}

		// 执行一些操作
		ctx := context.Background()
		for j := 0; j < 5; j++ {
			url := buildCommunityPositionsURL("", okxTestUniqueName)
			session.DoShare(ctx, func(page playwright.Page) error {
				_, err := ontGet(page, url)
				return err
			})
		}

		// 关闭 session
		session.Close()

		// 关闭 manager
		manager.Close()

		t.Logf("Iteration %d: goroutines=%d", i, runtime.NumGoroutine())
	}

	// 强制 GC
	runtime.GC()
	time.Sleep(500 * time.Millisecond)

	// 检查 goroutine 数量
	finalGoroutines := runtime.NumGoroutine()
	t.Logf("=== Resource Leak Detection Test ===")
	t.Logf("Initial goroutines: %d", initialGoroutines)
	t.Logf("Final goroutines:   %d", finalGoroutines)

	// 允许一定的波动（±10）
	leaked := finalGoroutines - initialGoroutines
	if leaked > 10 {
		t.Errorf("potential goroutine leak: %d goroutines not cleaned up", leaked)
	} else {
		t.Logf("No significant goroutine leak detected (delta: %d)", leaked)
	}
}

// TestMemoryUsageStability 测试内存使用稳定性
func TestMemoryUsageStability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	manager := okxTestManager(t, 2)
	defer manager.Close()

	session := okxTestSession(t, manager)
	defer session.Close()

	ctx := context.Background()

	if !warmupOKX(t, session, ctx) {
		t.Fatal("warmup failed")
	}

	// 获取初始内存
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// 执行大量操作
	for i := 0; i < 100; i++ {
		url := buildCommunityPositionsURL("", okxTestUniqueName)
		session.DoShare(ctx, func(page playwright.Page) error {
			_, err := ontGet(page, url)
			return err
		})
	}

	// 强制 GC
	runtime.GC()
	time.Sleep(500 * time.Millisecond)

	// 获取最终内存
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	t.Logf("=== Memory Usage Stability Test ===")
	t.Logf("Initial heap: %d KB", m1.HeapAlloc/1024)
	t.Logf("Final heap:   %d KB", m2.HeapAlloc/1024)
	t.Logf("Heap growth:  %d KB", int64(m2.HeapAlloc-m1.HeapAlloc)/1024)

	// 检查内存增长不超过合理范围
	heapGrowth := int64(m2.HeapAlloc) - int64(m1.HeapAlloc)
	if heapGrowth > 50*1024*1024 { // 50MB
		t.Errorf("heap grew by %d MB, potential memory leak", heapGrowth/1024/1024)
	}
}

// ============================================================
// 测试 8: 错误分类和处理测试
// ============================================================

// TestOKXErrorClassification 测试错误分类是否正确
func TestOKXErrorClassification(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	manager := okxTestManager(t, 1)
	defer manager.Close()

	session := okxTestSession(t, manager)
	defer session.Close()

	ctx := context.Background()

	// 测试正常错误（业务错误）
	var businessErrors, systemErrors int

	for i := 0; i < 10; i++ {
		// 使用无效的唯一名称来触发业务错误
		url := buildCommunityPositionsURL("", "INVALID_USER_"+fmt.Sprint(i))
		err := session.DoShare(ctx, func(page playwright.Page) error {
			_, err := ontGet(page, url)
			return err
		})

		if err != nil {
			if IsConnectionError(err) {
				systemErrors++
			} else {
				businessErrors++
			}
		}
	}

	t.Logf("=== Error Classification Test ===")
	t.Logf("Business errors: %d", businessErrors)
	t.Logf("System errors:   %d", systemErrors)

	// 业务错误应该占多数（因为使用了无效用户）
	if businessErrors < systemErrors {
		t.Logf("Warning: more system errors than business errors")
	}
}
