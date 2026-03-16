//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/souloss/browserpm"
)

func main() {
	fmt.Println("=== browserpm Concurrent Operations Demo ===")
	fmt.Println("This demo shows how browserpm handles high-concurrency scenarios.\n")

	manager, err := browserpm.New(
		browserpm.WithHeadless(true),
		browserpm.WithAutoInstall(true),
		browserpm.WithMinPages(3),
		browserpm.WithMaxPages(20),
		browserpm.WithPoolTTL(5*time.Minute),
		browserpm.WithOperationTimeout(30*time.Second),
	)
	if err != nil {
		log.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	contextProvider := browserpm.NewContextProvider(
		playwright.BrowserNewContextOptions{
			UserAgent: playwright.String("browserpm-concurrent-demo/1.0"),
		},
		func(ctx context.Context, bCtx playwright.BrowserContext) error {
			return nil
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

	session, err := manager.CreateSession("concurrent-demo", contextProvider, pageProvider)
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}

	ctx := context.Background()

	fmt.Println("--- Phase 1: Pool Scaling Under Load ---")

	initialStats := session.Stats()
	fmt.Printf("  Initial pool size: %d pages\n", initialStats.TotalPages)

	var wg sync.WaitGroup
	var opCount atomic.Int64
	var errorCount atomic.Int64

	concurrency := 30
	fmt.Printf("  Launching %d concurrent workers...\n\n", concurrency)

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < 10; j++ {
				opCount.Add(1)
				err := session.DoShare(ctx, func(page playwright.Page) error {
					_, err := page.Evaluate(`() => ({ worker: performance.now() })`)
					return err
				})

				if err != nil {
					errorCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	scaledStats := session.Stats()
	fmt.Printf("  Scaled pool size: %d pages\n", scaledStats.TotalPages)
	fmt.Printf("  Total operations: %d\n", opCount.Load())
	fmt.Printf("  Errors: %d\n", errorCount.Load())
	fmt.Printf("  Duration: %v\n", elapsed)
	fmt.Printf("  Throughput: %.1f ops/sec\n", float64(opCount.Load())/elapsed.Seconds())

	fmt.Println("\n--- Phase 2: Mixed Do and DoShare Operations ---")

	var doCount atomic.Int64
	var doShareCount atomic.Int64
	var mixedErrors atomic.Int64

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < 5; j++ {
				if id%2 == 0 {
					doCount.Add(1)
					err := session.Do(ctx, func(page playwright.Page) error {
						_, err := page.Evaluate(`() => 'exclusive'`)
						return err
					})
					if err != nil {
						mixedErrors.Add(1)
					}
				} else {
					doShareCount.Add(1)
					err := session.DoShare(ctx, func(page playwright.Page) error {
						_, err := page.Evaluate(`() => 'shared'`)
						return err
					})
					if err != nil {
						mixedErrors.Add(1)
					}
				}
			}
		}(i)
	}

	wg.Wait()

	fmt.Printf("  Do (exclusive) operations: %d\n", doCount.Load())
	fmt.Printf("  DoShare (shared) operations: %d\n", doShareCount.Load())
	fmt.Printf("  Mixed errors: %d\n", mixedErrors.Load())

	fmt.Println("\n--- Phase 3: Sustained Load Test ---")

	var sustainedOps atomic.Int64
	var sustainedErrors atomic.Int64

	testDuration := 10 * time.Second
	ctx2, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	fmt.Printf("  Running sustained load for %v...\n", testDuration)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx2.Done():
					return
				default:
					sustainedOps.Add(1)
					err := session.DoShare(ctx, func(page playwright.Page) error {
						_, err := page.Evaluate(`() => performance.now()`)
						return err
					})
					if err != nil {
						sustainedErrors.Add(1)
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
		}()
	}

	wg.Wait()

	fmt.Printf("  Total operations: %d\n", sustainedOps.Load())
	fmt.Printf("  Errors: %d\n", sustainedErrors.Load())
	fmt.Printf("  Average throughput: %.1f ops/sec\n", float64(sustainedOps.Load())/testDuration.Seconds())

	fmt.Println("\n--- Phase 4: Final Statistics ---")

	finalStats := session.Stats()
	info := session.Status()

	fmt.Printf("  Session: %s\n", info.Name)
	fmt.Printf("  State: %s\n", info.State)
	fmt.Printf("  Total pages: %d\n", finalStats.TotalPages)
	fmt.Printf("  Idle pages: %d\n", finalStats.IdlePages)
	fmt.Printf("  Total use count: %d\n", finalStats.TotalUse)
	fmt.Printf("  Healthy: %v\n", session.IsHealthy())

	fmt.Println("\n=== Demo Complete ===")
	fmt.Println("\nKey Observations:")
	fmt.Println("  1. Pool automatically scales under concurrent load")
	fmt.Println("  2. Do and DoShare can be used together safely")
	fmt.Println("  3. Throughput scales with pool size")
	fmt.Println("  4. Session remains stable under sustained load")
	fmt.Println("  5. All operations complete without manual intervention")
}
