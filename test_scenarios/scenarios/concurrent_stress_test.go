package scenarios

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/souloss/browserpm"
	"github.com/souloss/browserpm/test_scenarios/helpers"
)

func TestConcurrentStressBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(3),
		browserpm.WithMaxPages(10),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("stress-basic")

	var wg sync.WaitGroup
	var opCount atomic.Int64
	var successCount atomic.Int64
	var errorCount atomic.Int64

	concurrency := 20
	opsPerWorker := 50

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < opsPerWorker; j++ {
				opCount.Add(1)
				err := ts.DoShare(func(page playwright.Page) error {
					_, err := page.Evaluate(`() => Math.random()`)
					return err
				})

				if err != nil {
					errorCount.Add(1)
				} else {
					successCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()

	total := opCount.Load()
	success := successCount.Load()
	errs := errorCount.Load()

	t.Logf("Total: %d, Success: %d (%.1f%%), Errors: %d (%.1f%%)",
		total, success, float64(success)/float64(total)*100,
		errs, float64(errs)/float64(total)*100)

	helpers.AssertSessionActive(t, ts.Session)
}

func TestConcurrentStressMixedOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(8),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("stress-mixed")

	var wg sync.WaitGroup
	var doCount atomic.Int64
	var doShareCount atomic.Int64
	var errorCount atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					if id%2 == 0 {
						doCount.Add(1)
						err := ts.Do(func(page playwright.Page) error {
							_, err := page.Evaluate(`() => Date.now()`)
							return err
						})
						if err != nil {
							errorCount.Add(1)
						}
					} else {
						doShareCount.Add(1)
						err := ts.DoShare(func(page playwright.Page) error {
							_, err := page.Evaluate(`() => performance.now()`)
							return err
						})
						if err != nil {
							errorCount.Add(1)
						}
					}

					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Do operations: %d, DoShare operations: %d, Errors: %d",
		doCount.Load(), doShareCount.Load(), errorCount.Load())
}

func TestConcurrentStressPoolScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(1),
		browserpm.WithMaxPages(20),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("stress-scaling")

	initialStats := ts.Session.Stats()
	if initialStats != nil {
		t.Logf("Initial pages: %d", initialStats.TotalPages)
	} else {
		t.Log("Initial stats not available (pool not started)")
	}

	var wg sync.WaitGroup
	concurrency := 30

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				ts.DoShare(func(page playwright.Page) error {
					_, err := page.Evaluate(`() => 1`)
					return err
				})
				time.Sleep(20 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	finalStats := ts.Session.Stats()
	if finalStats != nil {
		initial := int64(0)
		if initialStats != nil {
			initial = int64(initialStats.TotalPages)
		}
		t.Logf("Final pages: %d (scaled from %d)", finalStats.TotalPages, initial)
		helpers.AssertPageCountInRange(t, ts.Session, 1, 20)
	} else {
		t.Log("Final stats not available")
	}
}

func TestConcurrentStressMemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithPoolTTL(30*time.Second),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("stress-memory")

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ts.DoShare(func(page playwright.Page) error {
					largeData := make([]byte, 1024*10)
					_, err := page.Evaluate(fmt.Sprintf(`() => %d`, len(largeData)))
					return err
				})
			}
		}()
	}

	wg.Wait()

	runtime.GC()
	runtime.ReadMemStats(&m2)

	heapGrowth := int64(m2.HeapAlloc) - int64(m1.HeapAlloc)
	t.Logf("Heap growth: %d MB", heapGrowth/1024/1024)

	helpers.AssertSessionActive(t, ts.Session)
}

func TestConcurrentStressErrorRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithOperationTimeout(30*time.Second),
	)

	ts := tm.CreateSession("stress-recovery")

	var wg sync.WaitGroup
	var totalOps atomic.Int64
	var successOps atomic.Int64

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				totalOps.Add(1)
				err := ts.DoShare(func(page playwright.Page) error {
					_, err := page.Evaluate(`() => ({ timestamp: Date.now() })`)
					return err
				})
				if err == nil {
					successOps.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	successRate := float64(successOps.Load()) / float64(totalOps.Load()) * 100
	t.Logf("Success rate: %.1f%% (%d/%d)", successRate, successOps.Load(), totalOps.Load())

	helpers.AssertSessionActive(t, ts.Session)
}

func TestConcurrentStressLongRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long-running test in short mode")
	}

	tm := helpers.NewTestManager(t,
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithPoolTTL(1*time.Minute),
		browserpm.WithHealthCheckInterval(10*time.Second),
	)

	ts := tm.CreateSession("stress-long-running")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var opCount atomic.Int64

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					opCount.Add(1)
					ts.DoShare(func(page playwright.Page) error {
						_, err := page.Evaluate(`() => performance.now()`)
						return err
					})
					time.Sleep(50 * time.Millisecond)
				}
			}
		}()
	}

	wg.Wait()

	t.Logf("Operations completed in 30s: %d", opCount.Load())
	t.Logf("Operations per second: %.1f", float64(opCount.Load())/30.0)

	helpers.AssertSessionActive(t, ts.Session)
}

func BenchmarkConcurrentDoShare(b *testing.B) {
	b.Skip("requires real browser - run manually")

	manager, err := browserpm.New(
		browserpm.WithHeadless(true),
		browserpm.WithAutoInstall(true),
		browserpm.WithMinPages(3),
		browserpm.WithMaxPages(10),
	)
	if err != nil {
		b.Fatal(err)
	}
	defer manager.Close()

	cp := browserpm.NewContextProvider(
		playwright.BrowserNewContextOptions{},
		func(ctx context.Context, bCtx playwright.BrowserContext) error { return nil },
	)

	pp := browserpm.NewPageProvider(
		func(ctx context.Context, page playwright.Page) error { return nil },
		func(ctx context.Context, page playwright.Page) bool { return !page.IsClosed() },
	)

	session, _ := manager.CreateSession("bench", cp, pp)
	defer session.Close()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			session.DoShare(context.Background(), func(page playwright.Page) error {
				_, err := page.Evaluate(`() => 1`)
				return err
			})
		}
	})
}
