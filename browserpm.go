// Package browserpm provides a browser page manager built on playwright-go.
// It manages browser sessions with automatic page pooling, health checking,
// and recovery. Use channels and atomic operations for the hot path to
// minimize lock contention under high concurrency.
package browserpm

import (
	"context"

	"github.com/playwright-community/playwright-go"
)

// ContextProvider supplies the options and post-creation setup for a
// BrowserContext. Implementations must be safe for concurrent use because
// the session may rebuild the context at any time.
type ContextProvider interface {
	Options() playwright.BrowserNewContextOptions
	Setup(ctx context.Context, bCtx playwright.BrowserContext) error
}

// PageProvider defines page initialization and health checking.
// Init is called once when a page is created (shared pool) or each time
// an exclusive page is created (Do). Check is called periodically by the
// health checker and should return quickly.
type PageProvider interface {
	Init(ctx context.Context, page playwright.Page) error
	Check(ctx context.Context, page playwright.Page) bool
}

// OperationFunc is a function that operates on a page.
type OperationFunc func(page playwright.Page) error

// ProcessInfo holds resource usage information for a browser process.
type ProcessInfo struct {
	Type            string
	ID              int32
	CPU             float64
	RSS             uint64
	VMS             uint64
	ExclusiveMemory uint64
}

// --- Convenience constructors ---

type funcContextProvider struct {
	opts  playwright.BrowserNewContextOptions
	setup func(context.Context, playwright.BrowserContext) error
}

func (p *funcContextProvider) Options() playwright.BrowserNewContextOptions {
	return p.opts
}

func (p *funcContextProvider) Setup(ctx context.Context, bCtx playwright.BrowserContext) error {
	if p.setup != nil {
		return p.setup(ctx, bCtx)
	}
	return nil
}

// NewContextProvider creates a ContextProvider from options and an optional
// setup function. Pass nil for setup if no post-creation work is needed.
func NewContextProvider(opts playwright.BrowserNewContextOptions, setup func(context.Context, playwright.BrowserContext) error) ContextProvider {
	return &funcContextProvider{opts: opts, setup: setup}
}

// SimpleContextProvider creates a ContextProvider that only supplies options.
func SimpleContextProvider(opts playwright.BrowserNewContextOptions) ContextProvider {
	return &funcContextProvider{opts: opts}
}

type funcPageProvider struct {
	initFn  func(context.Context, playwright.Page) error
	checkFn func(context.Context, playwright.Page) bool
}

func (p *funcPageProvider) Init(ctx context.Context, page playwright.Page) error {
	if p.initFn != nil {
		return p.initFn(ctx, page)
	}
	return nil
}

func (p *funcPageProvider) Check(ctx context.Context, page playwright.Page) bool {
	if p.checkFn != nil {
		return p.checkFn(ctx, page)
	}
	return true
}

// NewPageProvider creates a PageProvider from init and check functions.
// Either function may be nil for default behaviour (no-op init, always healthy).
func NewPageProvider(
	initFn func(context.Context, playwright.Page) error,
	checkFn func(context.Context, playwright.Page) bool,
) PageProvider {
	return &funcPageProvider{initFn: initFn, checkFn: checkFn}
}
