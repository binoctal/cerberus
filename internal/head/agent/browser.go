package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	pw "github.com/playwright-community/playwright-go"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// BrowserExecutor drives a headless browser via Playwright.
type BrowserExecutor struct {
	pw      *pw.Playwright
	browser pw.Browser
	page    pw.Page
	logger  *zap.Logger
	mu      sync.Mutex // serializes all page operations; a Playwright page is not concurrency-safe
}

// NewBrowserExecutor creates a browser executor by launching a headless Chromium.
// Returns an error if Playwright driver or browser binary is unavailable.
func NewBrowserExecutor(logger *zap.Logger) (*BrowserExecutor, error) {
	driver, err := pw.Run()
	if err != nil {
		return nil, fmt.Errorf("start playwright driver: %w", err)
	}

	browser, err := driver.Chromium.Launch(pw.BrowserTypeLaunchOptions{
		Headless: pw.Bool(true),
	})
	if err != nil {
		_ = driver.Stop()
		return nil, fmt.Errorf("launch chromium: %w", err)
	}

	page, err := browser.NewPage()
	if err != nil {
		_ = browser.Close()
		_ = driver.Stop()
		return nil, fmt.Errorf("create page: %w", err)
	}

	return &BrowserExecutor{
		pw:      driver,
		browser: browser,
		page:    page,
		logger:  logger,
	}, nil
}

// Close shuts down the browser and Playwright driver.
func (e *BrowserExecutor) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.page != nil {
		_ = e.page.Close()
	}
	if e.browser != nil {
		_ = e.browser.Close()
	}
	if e.pw != nil {
		_ = e.pw.Stop()
	}
}

// Execute dispatches browser actions.
func (e *BrowserExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	e.mu.Lock()
	defer e.mu.Unlock()

	start := time.Now()

	switch a := action.(type) {
	case types.BrowserGotoAction:
		return e.gotoPage(a, start)
	case types.BrowserClickAction:
		return e.clickElement(a, start)
	case types.BrowserFillAction:
		return e.fillField(a, start)
	case types.BrowserEvalAction:
		return e.evalJS(a, start)
	default:
		return types.ErrorResult{Err: fmt.Sprintf("browser executor: unsupported action %T", action)}
	}
}

func (e *BrowserExecutor) gotoPage(a types.BrowserGotoAction, start time.Time) types.ExecutorResult {
	waitUntil := "load"
	if a.WaitUntil != "" {
		waitUntil = a.WaitUntil
	}

	waitUntilState := pw.WaitUntilState(waitUntil)
	resp, err := e.page.Goto(a.URL, pw.PageGotoOptions{
		WaitUntil: &waitUntilState,
	})
	if err != nil {
		return types.BrowserResult{
			OK: false, URL: a.URL, Err: err.Error(), Latency: time.Since(start),
		}
	}

	title, _ := e.page.Title()
	text, _ := e.page.Locator("body").TextContent()

	statusCode := 0
	if resp != nil {
		statusCode = resp.Status()
	}

	return types.BrowserResult{
		OK:      statusCode < 400,
		URL:     e.page.URL(),
		Title:   title,
		Text:    truncateStr(text, 5000),
		Latency: time.Since(start),
	}
}

func (e *BrowserExecutor) clickElement(a types.BrowserClickAction, start time.Time) types.ExecutorResult {
	locator := e.page.Locator(a.Selector)
	if err := locator.Click(); err != nil {
		return types.BrowserResult{
			OK: false, URL: e.page.URL(), Err: fmt.Sprintf("click %q: %v", a.Selector, err),
			Latency: time.Since(start),
		}
	}

	title, _ := e.page.Title()
	return types.BrowserResult{
		OK:      true,
		URL:     e.page.URL(),
		Title:   title,
		Latency: time.Since(start),
	}
}

func (e *BrowserExecutor) fillField(a types.BrowserFillAction, start time.Time) types.ExecutorResult {
	locator := e.page.Locator(a.Selector)
	if err := locator.Fill(a.Value); err != nil {
		return types.BrowserResult{
			OK: false, URL: e.page.URL(), Err: fmt.Sprintf("fill %q: %v", a.Selector, err),
			Latency: time.Since(start),
		}
	}

	return types.BrowserResult{
		OK:      true,
		URL:     e.page.URL(),
		Latency: time.Since(start),
	}
}

func (e *BrowserExecutor) evalJS(a types.BrowserEvalAction, start time.Time) types.ExecutorResult {
	result, err := e.page.Evaluate(a.Expression)
	if err != nil {
		return types.BrowserResult{
			OK: false, URL: e.page.URL(), Err: fmt.Sprintf("eval: %v", err),
			Latency: time.Since(start),
		}
	}

	evalStr := ""
	if result != nil {
		evalStr = fmt.Sprintf("%v", result)
	}

	return types.BrowserResult{
		OK:         true,
		URL:        e.page.URL(),
		EvalResult: evalStr,
		Latency:    time.Since(start),
	}
}

// TakeScreenshot captures the current page as a base64-encoded PNG.
func (e *BrowserExecutor) TakeScreenshot() (string, error) {
	data, err := e.page.Screenshot(pw.PageScreenshotOptions{
		Type: pw.ScreenshotTypePng,
	})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

func truncateStr(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
