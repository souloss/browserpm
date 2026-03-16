//go:build ignore

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
	manager, err := browserpm.New(
		browserpm.WithHeadless(true),
		browserpm.WithAutoInstall(true),
		browserpm.WithMinPages(2),
		browserpm.WithMaxPages(5),
		browserpm.WithPoolTTL(30*time.Minute),
	)
	if err != nil {
		log.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	contextProvider := browserpm.NewContextProvider(
		playwright.BrowserNewContextOptions{
			UserAgent: playwright.String("browserpm-example/1.0"),
		},
		func(ctx context.Context, bCtx playwright.BrowserContext) error {
			fmt.Println("Context setup complete")
			return nil
		},
	)

	pageProvider := browserpm.NewPageProvider(
		func(ctx context.Context, page playwright.Page) error {
			_, err := page.Goto("https://example.com", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			})
			return err
		},
		func(ctx context.Context, page playwright.Page) bool {
			return !page.IsClosed()
		},
	)

	session, err := manager.CreateSession("basic-example", contextProvider, pageProvider)
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}

	ctx := context.Background()

	fmt.Println("\n=== Exclusive Page Operations (Do) ===")
	for i := 0; i < 3; i++ {
		err := session.Do(ctx, func(page playwright.Page) error {
			title, err := page.Title()
			if err != nil {
				return err
			}
			fmt.Printf("  [%d] Title: %s\n", i+1, title)
			return nil
		})
		if err != nil {
			log.Printf("Do operation %d failed: %v", i+1, err)
		}
	}

	fmt.Println("\n=== Shared Page Operations (DoShare) ===")
	for i := 0; i < 5; i++ {
		err := session.DoShare(ctx, func(page playwright.Page) error {
			result, err := page.Evaluate(`() => ({ title: document.title, url: location.href })`)
			if err != nil {
				return err
			}
			fmt.Printf("  [%d] Result: %v\n", i+1, result)
			return nil
		})
		if err != nil {
			log.Printf("DoShare operation %d failed: %v", i+1, err)
		}
	}

	fmt.Println("\n=== Session Status ===")
	info := session.Status()
	fmt.Printf("  Name: %s\n", info.Name)
	fmt.Printf("  State: %s\n", info.State)
	fmt.Printf("  Page Count: %d\n", info.PageCount)
	fmt.Printf("  Active Ops: %d\n", info.ActiveOps)
	fmt.Printf("  Created: %v\n", info.CreatedAt)

	fmt.Println("\n=== Pool Statistics ===")
	stats := session.Stats()
	if stats != nil {
		fmt.Printf("  Total Pages: %d\n", stats.TotalPages)
		fmt.Printf("  Idle Pages: %d\n", stats.IdlePages)
		fmt.Printf("  Total Use: %d\n", stats.TotalUse)
	}

	fmt.Println("\n=== All Sessions ===")
	for _, s := range manager.ListSessions() {
		fmt.Printf("  - %s (%s, %d pages)\n", s.Name, s.State, s.PageCount)
	}

	fmt.Println("\n=== Health Check ===")
	unhealthy := session.HealthCheck(ctx)
	fmt.Printf("  Unhealthy pages: %d\n", unhealthy)
	fmt.Printf("  Is Healthy: %v\n", session.IsHealthy())

	fmt.Println("\nDone!")
}
