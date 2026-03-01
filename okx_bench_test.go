package browserpm

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
)

// ontGetFast is an optimised version of ontGet.
// - No nil/closed pre-check (pool guarantees healthy pages).
// - Minimal JS: URL passed as argument avoids fmt.Sprintf escaping.
func ontGetFast(page playwright.Page, apiURL string) (map[string]interface{}, error) {
	result, err := page.Evaluate(
		`u => window.utils.ont.get(u).catch(e=>({error:e.message||String(e)}))`,
		apiURL,
	)
	if err != nil {
		return nil, err
	}
	resp, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected type %T", result)
	}
	if errMsg, has := resp["error"]; has {
		return nil, fmt.Errorf("API: %v", errMsg)
	}
	return resp, nil
}

// ontGetBatch sends multiple URLs in a single Evaluate using Promise.all.
func ontGetBatch(page playwright.Page, urls []string) ([]map[string]interface{}, error) {
	result, err := page.Evaluate(
		`urls => Promise.all(urls.map(u => window.utils.ont.get(u).catch(e=>({error:e.message||String(e)}))))`,
		urls,
	)
	if err != nil {
		return nil, err
	}
	arr, ok := result.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected type %T", result)
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out, nil
}

type benchResult struct {
	name    string
	success int64
	errors  int64
	elapsed time.Duration
	qps     float64
}

func (r benchResult) String() string {
	return fmt.Sprintf("%-28s | success=%4d errors=%4d | %8s | QPS=%7.1f",
		r.name, r.success, r.errors, r.elapsed.Round(time.Millisecond), r.qps)
}

// TestOKXOptimize runs a matrix of configurations to find the peak QPS.
// It reuses a single BrowserManager and creates per-config sessions to avoid
// repeated browser startup overhead.
func TestOKXOptimize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping optimisation test in short mode")
	}

	manager, err := New(
		WithHeadless(true),
		WithAutoInstall(true),
		WithOperationTimeout(60*time.Second),
		WithInitTimeout(90*time.Second),
		WithHealthCheckInterval(10*time.Minute),
		WithPoolTTL(30*time.Minute),
	)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	defer manager.Close()

	ctx := context.Background()
	const uniqueName = "E512EAA2C34FAF44"

	type testCase struct {
		name        string
		pages       int
		concurrency int
		totalCalls  int
		useFast     bool
		batchSize   int
	}

	cases := []testCase{
		// Baseline
		{name: "orig-5p-50c", pages: 5, concurrency: 50, totalCalls: 200},

		// Fast ontGet
		{name: "fast-5p-50c", pages: 5, concurrency: 50, totalCalls: 200, useFast: true},
		{name: "fast-5p-100c", pages: 5, concurrency: 100, totalCalls: 300, useFast: true},
		{name: "fast-5p-200c", pages: 5, concurrency: 200, totalCalls: 300, useFast: true},
		{name: "fast-3p-100c", pages: 3, concurrency: 100, totalCalls: 300, useFast: true},
		{name: "fast-3p-200c", pages: 3, concurrency: 200, totalCalls: 300, useFast: true},
		{name: "fast-10p-100c", pages: 10, concurrency: 100, totalCalls: 300, useFast: true},
		{name: "fast-10p-200c", pages: 10, concurrency: 200, totalCalls: 400, useFast: true},
		{name: "fast-15p-200c", pages: 15, concurrency: 200, totalCalls: 400, useFast: true},
		{name: "fast-1p-100c", pages: 1, concurrency: 100, totalCalls: 300, useFast: true},
		{name: "fast-1p-200c", pages: 1, concurrency: 200, totalCalls: 300, useFast: true},

		// Batched: Promise.all
		{name: "batch5-5p-50c", pages: 5, concurrency: 50, totalCalls: 250, useFast: true, batchSize: 5},
		{name: "batch10-5p-50c", pages: 5, concurrency: 50, totalCalls: 300, useFast: true, batchSize: 10},
		{name: "batch5-10p-100c", pages: 10, concurrency: 100, totalCalls: 400, useFast: true, batchSize: 5},
		{name: "batch10-10p-100c", pages: 10, concurrency: 100, totalCalls: 400, useFast: true, batchSize: 10},
		{name: "batch20-5p-50c", pages: 5, concurrency: 50, totalCalls: 400, useFast: true, batchSize: 20},
		{name: "batch5-3p-200c", pages: 3, concurrency: 200, totalCalls: 300, useFast: true, batchSize: 5},
	}

	var results []benchResult

	for i, tc := range cases {
		sessionName := fmt.Sprintf("okx-%d", i)
		sess, err := manager.CreateSession(sessionName,
			okxContextProvider(), okxPageProvider(),
			WithSessionMinPages(tc.pages), WithSessionMaxPages(tc.pages))
		if err != nil {
			t.Fatalf("[%s] session: %v", tc.name, err)
		}

		// Warm up to make sure all pages are ready.
		warmURL := buildCommunityPositionsURL("", uniqueName)
		if err := sess.DoShare(ctx, func(page playwright.Page) error {
			_, e := ontGetFast(page, warmURL)
			return e
		}); err != nil {
			t.Fatalf("[%s] warm-up: %v", tc.name, err)
		}

		var (
			success atomic.Int64
			fail    atomic.Int64
			wg      sync.WaitGroup
			sem     = make(chan struct{}, tc.concurrency)
		)

		if tc.batchSize > 0 {
			batches := tc.totalCalls / tc.batchSize
			start := time.Now()
			for b := 0; b < batches; b++ {
				wg.Add(1)
				sem <- struct{}{}
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					urls := make([]string, tc.batchSize)
					for j := range urls {
						urls[j] = buildCommunityPositionsURL("", uniqueName)
					}
					err := sess.DoShare(ctx, func(page playwright.Page) error {
						res, e := ontGetBatch(page, urls)
						if e != nil {
							return e
						}
						for _, r := range res {
							if _, has := r["error"]; has {
								fail.Add(1)
							} else {
								success.Add(1)
							}
						}
						return nil
					})
					if err != nil {
						fail.Add(int64(tc.batchSize))
					}
				}()
			}
			wg.Wait()
			elapsed := time.Since(start)
			s := success.Load()
			e := fail.Load()
			qps := float64(s) / elapsed.Seconds()
			results = append(results, benchResult{tc.name, s, e, elapsed, qps})
			t.Logf("[%s] %s", tc.name, results[len(results)-1])
		} else {
			start := time.Now()
			for c := 0; c < tc.totalCalls; c++ {
				wg.Add(1)
				sem <- struct{}{}
				go func() {
					defer wg.Done()
					defer func() { <-sem }()
					apiURL := buildCommunityPositionsURL("", uniqueName)
					err := sess.DoShare(ctx, func(page playwright.Page) error {
						if tc.useFast {
							_, e := ontGetFast(page, apiURL)
							return e
						}
						_, e := ontGet(page, apiURL)
						return e
					})
					if err != nil {
						fail.Add(1)
					} else {
						success.Add(1)
					}
				}()
			}
			wg.Wait()
			elapsed := time.Since(start)
			s := success.Load()
			e := fail.Load()
			qps := float64(s) / elapsed.Seconds()
			results = append(results, benchResult{tc.name, s, e, elapsed, qps})
			t.Logf("[%s] %s", tc.name, results[len(results)-1])
		}

		// Close session to free pages for the next config.
		manager.CloseSession(sessionName)
	}

	// Summary.
	t.Log("")
	t.Log("=== QPS SUMMARY ===")
	var best benchResult
	for _, r := range results {
		t.Log(r.String())
		if r.qps > best.qps {
			best = r
		}
	}
	t.Logf("")
	t.Logf("BEST: %s", best)
}
