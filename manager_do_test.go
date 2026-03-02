package browserpm

import (
	"context"
	"testing"

	"github.com/playwright-community/playwright-go"
)

func TestManagerDo_Basic(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	manager, err := New(
		WithHeadless(true),
		WithAutoInstall(true),
		WithMinPages(1),
		WithMaxPages(5),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Test basic operation without storage state
	err = manager.Do(ctx, playwright.BrowserNewContextOptions{
		UserAgent: playwright.String("browserpm-test/1.0"),
	}, func(page playwright.Page) error {
		_, err := page.Goto("https://example.com")
		if err != nil {
			return err
		}
		title, err := page.Title()
		if err != nil {
			return err
		}
		t.Logf("Page title: %s", title)
		return nil
	})
	if err != nil {
		t.Fatalf("Do failed: %v", err)
	}
}

func TestManagerDo_WithStorageState(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	manager, err := New(
		WithHeadless(true),
		WithAutoInstall(true),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// First, create a session and get storage state
	session, err := manager.CreateSession("temp",
		NewContextProvider(playwright.BrowserNewContextOptions{}, nil),
		NewPageProvider(
			func(ctx context.Context, page playwright.Page) error {
				_, err := page.Goto("https://example.com")
				return err
			},
			func(ctx context.Context, page playwright.Page) bool {
				return !page.IsClosed()
			},
		),
	)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	var storageState *playwright.StorageState
	err = session.Do(ctx, func(page playwright.Page) error {
		// Get storage state from the context
		var e error
		storageState, e = page.Context().StorageState()
		return e
	})
	if err != nil {
		t.Fatalf("failed to get storage state: %v", err)
	}
	manager.CloseSession("temp")

	// Now use Do with the storage state
	err = manager.Do(ctx, playwright.BrowserNewContextOptions{
		StorageState: storageState.ToOptionalStorageState(),
	}, func(page playwright.Page) error {
		_, err := page.Goto("https://example.com")
		return err
	})
	if err != nil {
		t.Fatalf("Do with StorageState failed: %v", err)
	}

	t.Logf("Do with StorageState succeeded")
}

func TestManagerDo_MultipleConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	manager, err := New(
		WithHeadless(true),
		WithAutoInstall(true),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()

	// Run multiple concurrent Do operations
	const numOps = 5
	done := make(chan error, numOps)

	for i := 0; i < numOps; i++ {
		go func(idx int) {
			err := manager.Do(ctx, playwright.BrowserNewContextOptions{
				UserAgent: playwright.String("browserpm-test/concurrent"),
			}, func(page playwright.Page) error {
				_, err := page.Goto("https://example.com")
				if err != nil {
					return err
				}
				t.Logf("Concurrent op %d completed", idx)
				return nil
			})
			done <- err
		}(i)
	}

	for i := 0; i < numOps; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent op %d failed: %v", i, err)
		}
	}
}

func TestManagerDo_ClosedManager(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	manager, err := New(
		WithHeadless(true),
		WithAutoInstall(true),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	manager.Close()

	ctx := context.Background()
	err = manager.Do(ctx, playwright.BrowserNewContextOptions{}, func(page playwright.Page) error {
		return nil
	})

	if err == nil {
		t.Fatal("expected error when calling Do on closed manager")
	}
	t.Logf("Expected error: %v", err)
}
