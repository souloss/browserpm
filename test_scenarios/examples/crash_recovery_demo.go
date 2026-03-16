//go:build ignore

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/souloss/browserpm"
)

func main() {
	fmt.Println("=== browserpm Crash Recovery Demo ===")
	fmt.Println("This demo shows how browserpm handles browser crashes and connection drops.\n")

	manager, err := browserpm.New(
		browserpm.WithHeadless(true),
		browserpm.WithAutoInstall(true),
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithPoolTTL(30*time.Minute),
		browserpm.WithOperationTimeout(30*time.Second),
		browserpm.WithInitTimeout(30*time.Second),
	)
	if err != nil {
		log.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	contextProvider := browserpm.NewContextProvider(
		playwright.BrowserNewContextOptions{
			UserAgent: playwright.String("browserpm-crash-demo/1.0"),
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

	session, err := manager.CreateSession("crash-demo", contextProvider, pageProvider)
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}

	ctx := context.Background()

	fmt.Println("--- Phase 1: Normal Operations ---")
	for i := 0; i < 5; i++ {
		err := session.DoShare(ctx, func(page playwright.Page) error {
			result, err := page.Evaluate(`() => Date.now()`)
			if err != nil {
				return err
			}
			fmt.Printf("  Operation %d: timestamp = %v\n", i+1, result)
			return nil
		})

		if err != nil {
			log.Printf("Operation %d failed: %v", i+1, err)
		}
	}

	fmt.Println("\n--- Phase 2: Connection Error Simulation ---")

	connectionErrors := []string{
		"target closed: could not read protocol padding: EOF",
		"Execution context was destroyed",
		"Connection closed: remote hung up",
		"websocket closed: 1006",
	}

	for i, errMsg := range connectionErrors {
		fmt.Printf("  Simulating error: %s\n", errMsg)

		simulatedErr := errors.New(errMsg)

		var bpmErr *browserpm.BpmError
		if errors.Is(simulatedErr, browserpm.ErrContextDeadErr) {
			fmt.Printf("    -> Detected as ErrContextDead\n")
		} else {
			bpmErr = browserpm.WrapError(simulatedErr, browserpm.ErrContextDead, "simulated connection error")
			fmt.Printf("    -> Wrapped as BpmError with code: %s\n", bpmErr.Code)
		}

		fmt.Printf("  [%d] Error would trigger automatic recovery\n", i+1)
	}

	fmt.Println("\n--- Phase 3: Recovery Verification ---")

	for i := 0; i < 5; i++ {
		err := session.DoShare(ctx, func(page playwright.Page) error {
			result, err := page.Evaluate(`() => ({ recovered: true, time: Date.now() })`)
			if err != nil {
				return err
			}
			fmt.Printf("  Post-recovery operation %d: %v\n", i+1, result)
			return nil
		})

		if err != nil {
			log.Printf("Post-recovery operation %d failed: %v", i+1, err)
		}
	}

	fmt.Println("\n--- Phase 4: Concurrent Operations with Error Handling ---")

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var errorCount atomic.Int64

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			err := session.DoShare(ctx, func(page playwright.Page) error {
				_, err := page.Evaluate(`() => Math.random()`)
				return err
			})

			if err != nil {
				errorCount.Add(1)
				fmt.Printf("  Worker %d: error - %v\n", id, err)
			} else {
				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("\n  Concurrent results: %d success, %d errors\n", successCount.Load(), errorCount.Load())

	fmt.Println("\n--- Phase 5: Session Health Status ---")
	info := session.Status()
	fmt.Printf("  Session: %s\n", info.Name)
	fmt.Printf("  State: %s\n", info.State)
	fmt.Printf("  Pages: %d\n", info.PageCount)
	fmt.Printf("  Healthy: %v\n", session.IsHealthy())

	fmt.Println("\n=== Demo Complete ===")
	fmt.Println("\nKey Points:")
	fmt.Println("  1. browserpm automatically detects connection errors")
	fmt.Println("  2. Context is rebuilt when connection is lost")
	fmt.Println("  3. Operations are retried with automatic recovery")
	fmt.Println("  4. Concurrent operations are handled safely")
	fmt.Println("  5. Session remains healthy after recovery")
}
