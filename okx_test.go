package browserpm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
)

// --- OKX helpers ---

func buildURL(base string, params map[string]string) string {
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}
	return base + "?" + values.Encode()
}

func nowMillis() string {
	return strconv.FormatInt(time.Now().UnixMilli(), 10)
}

func buildCommunityPositionsURL(baseURL string, uniqueName string) string {
	if baseURL == "" {
		baseURL = "https://www.okx.com"
	}
	base := baseURL + "/priapi/v5/ecotrade/public/community/user/position-current"
	params := map[string]string{
		"uniqueName": uniqueName,
		"t":          nowMillis(),
	}
	return buildURL(base, params)
}

// ontGet calls OKX's window.utils.ont.get via page.Evaluate.
func ontGet(page playwright.Page, apiURL string) (map[string]interface{}, error) {
	if page == nil || page.IsClosed() {
		return nil, fmt.Errorf("page is closed or nil")
	}

	result, err := page.Evaluate(
		`u => window.utils.ont.get(u).catch(e=>({error:e.message||String(e)}))`,
		apiURL,
	)
	if err != nil {
		return nil, fmt.Errorf("evaluate failed for url=%s: %w", apiURL, err)
	}

	resp, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response type %T for url=%s", result, apiURL)
	}

	if errMsg, hasErr := resp["error"]; hasErr {
		return nil, fmt.Errorf("API error for url=%s: %s", apiURL, formatErrorValue(errMsg))
	}

	return resp, nil
}

func formatErrorValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]interface{}:
		if msg, ok := val["message"].(string); ok {
			return msg
		}
		if msg, ok := val["msg"].(string); ok {
			return msg
		}
		if jsonBytes, err := json.Marshal(val); err == nil {
			return string(jsonBytes)
		}
		return fmt.Sprintf("%+v", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// --- OKX Context & Page Providers ---

func okxContextProvider() ContextProvider {
	return NewContextProvider(
		playwright.BrowserNewContextOptions{
			UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"),
		},
		nil,
	)
}

func okxPageProvider() PageProvider {
	return NewPageProvider(
		func(ctx context.Context, page playwright.Page) error {
			_, err := page.Goto("https://www.okx.com", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateDomcontentloaded,
				Timeout:   playwright.Float(60000),
			})
			if err != nil {
				return fmt.Errorf("goto okx.com failed: %w", err)
			}
			_, err = page.WaitForFunction(
				`() => window.utils?.ont?.get !== undefined`,
				playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(60000)},
			)
			if err != nil {
				return fmt.Errorf("wait for ont.get failed: %w", err)
			}
			return nil
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
}

// TestOKX is the main integration test for OKX ontGet high-concurrency monitoring.
//
// Configuration summary (from exhaustive optimisation):
//
//	Baseline:         77 QPS  (5 pages, 50 concurrency, single ontGet)
//	Optimised:      3813 QPS  (1 page, 1000 concurrency, batch100 Promise.all)
//	Improvement:     ~50x
//
// Key findings:
//   - Fewer pages (1-3) outperform more pages (10-15) because the browser
//     process handles concurrent async JS natively.
//   - Batch Evaluate (Promise.all) is the biggest single improvement (~5-10x)
//     by reducing CDP round-trips.
//   - Higher concurrency (500-1000 goroutines) keeps more requests in-flight.
func TestOKX(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const (
		pageCount   = 1
		concurrency = 1000
		totalCalls  = 2000
		batchSize   = 100
		uniqueName  = "E512EAA2C34FAF44"
	)

	manager, err := New(
		WithHeadless(true),
		WithAutoInstall(true),
		WithMinPages(pageCount),
		WithMaxPages(pageCount),
		WithPoolTTL(30*time.Minute),
		WithOperationTimeout(90*time.Second),
		WithInitTimeout(90*time.Second),
		WithHealthCheckInterval(10*time.Minute),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	defer manager.Close()

	session, err := manager.CreateSession("okx", okxContextProvider(), okxPageProvider())
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	ctx := context.Background()

	// Warm up: verify single call works.
	warmURL := buildCommunityPositionsURL("", uniqueName)
	err = session.DoShare(ctx, func(page playwright.Page) error {
		resp, err := ontGet(page, warmURL)
		if err != nil {
			return err
		}
		t.Logf("warm-up response keys: %v", resp)
		return nil
	})
	if err != nil {
		t.Fatalf("warm-up call failed: %v", err)
	}

	// Stress test with batch Promise.all.
	var (
		successCount atomic.Int64
		errorCount   atomic.Int64
		wg           sync.WaitGroup
		sem          = make(chan struct{}, concurrency)
	)

	batches := totalCalls / batchSize
	start := time.Now()
	for i := 0; i < batches; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			urls := make([]string, batchSize)
			for j := range urls {
				urls[j] = buildCommunityPositionsURL("", uniqueName)
			}
			err := session.DoShare(ctx, func(page playwright.Page) error {
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
				t.Logf("warm-up response keys: %v", result)
				return nil
			})
			if err != nil {
				errorCount.Add(int64(batchSize))
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	success := successCount.Load()
	errors := errorCount.Load()
	qps := float64(success) / elapsed.Seconds()

	t.Logf("=== OKX Stress Test Results ===")
	t.Logf("Pages:        %d", pageCount)
	t.Logf("Concurrency:  %d", concurrency)
	t.Logf("Batch size:   %d", batchSize)
	t.Logf("Total calls:  %d", totalCalls)
	t.Logf("Success:      %d", success)
	t.Logf("Errors:       %d", errors)
	t.Logf("Elapsed:      %s", elapsed.Round(time.Millisecond))
	t.Logf("QPS:          %.1f", qps)

	info := session.Status()
	t.Logf("Session:      state=%s pages=%d active_ops=%d", info.State, info.PageCount, info.ActiveOps)

	if success == 0 {
		t.Fatal("all calls failed")
	}
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
