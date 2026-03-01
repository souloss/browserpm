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
	// 1. Create a manager with options.
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

	// 2. Define providers.
	contextProvider := browserpm.NewContextProvider(
		playwright.BrowserNewContextOptions{
			UserAgent: playwright.String("browserpm-example/1.0"),
		},
		func(ctx context.Context, bCtx playwright.BrowserContext) error {
			fmt.Println("context setup complete")
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

	// 3. Create a session.
	session, err := manager.CreateSession("example", contextProvider, pageProvider)
	if err != nil {
		log.Fatalf("failed to create session: %v", err)
	}

	ctx := context.Background()

	// 4. Exclusive page (created, used, then closed).
	err = session.Do(ctx, func(page playwright.Page) error {
		title, err := page.Title()
		if err != nil {
			return err
		}
		fmt.Printf("[Do] page title: %s\n", title)
		return nil
	})
	if err != nil {
		log.Printf("Do failed: %v", err)
	}

	// 5. Shared page (from pool, reused across calls).
	for i := 0; i < 5; i++ {
		err = session.DoShare(ctx, func(page playwright.Page) error {
			result, err := page.Evaluate(`() => document.title`)
			if err != nil {
				return err
			}
			fmt.Printf("[DoShare #%d] title: %v\n", i, result)
			return nil
		})
		if err != nil {
			log.Printf("DoShare #%d failed: %v", i, err)
		}
	}

	// 6. Query session status.
	info := session.Status()
	fmt.Printf("Session: name=%s state=%s pages=%d\n", info.Name, info.State, info.PageCount)

	// 7. List all sessions.
	for _, s := range manager.ListSessions() {
		fmt.Printf("  - %s (%s)\n", s.Name, s.State)
	}

	// 8. Process info (if CDP is available).
	pInfos, err := manager.GetProcessInfos(ctx)
	if err == nil {
		for _, p := range pInfos {
			fmt.Printf("Process %d (%s): RSS=%dMB\n", p.ID, p.Type, p.RSS/1024/1024)
		}
	}

	fmt.Println("done")
}
