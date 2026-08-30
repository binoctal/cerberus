package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	pw "github.com/playwright-community/playwright-go"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// BrowserExecutor drives a headless browser via Playwright.
type BrowserExecutor struct {
	pw          *pw.Playwright
	browser     pw.Browser
	page        pw.Page
	projectDir  string
	logger      *zap.Logger
	mu          sync.Mutex // serializes all page operations; a Playwright page is not concurrency-safe
	session     *browserSessionRecipe
	reloginGate reloginLimiter
	lastTarget  string // last goto target that stayed off the login page; expect-time heals return here
}

// NewBrowserExecutor creates a browser executor by launching a headless Chromium.
// projectDir anchors the screenshot sink (<projectDir>/.cerberus/runtime/shots).
// Returns an error if Playwright driver or browser binary is unavailable.
func NewBrowserExecutor(projectDir string, logger *zap.Logger) (*BrowserExecutor, error) {
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
		pw:         driver,
		browser:    browser,
		page:       page,
		projectDir: projectDir,
		logger:     logger,
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
		return e.gotoPage(ctx, a, start)
	case types.BrowserClickAction:
		return e.clickElement(a, start)
	case types.BrowserFillAction:
		return e.fillField(a, start)
	case types.BrowserEvalAction:
		return e.evalJS(a, start)
	case types.BrowserExpectAction:
		return e.expectAssertion(ctx, a, start)
	default:
		return types.ErrorResult{Err: fmt.Sprintf("browser executor: unsupported action %T", action)}
	}
}

func (e *BrowserExecutor) gotoPage(ctx context.Context, a types.BrowserGotoAction, start time.Time) types.ExecutorResult {
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

	// Let the SPA's client-side routing converge before the auth-loss check:
	// the auth bounce to /login happens after hydration (post-load), so
	// reading page.URL() straight after Goto still sees the target page
	// (run36/37: the heal never fired for exactly this reason).
	_, _ = e.page.Locator("body").TextContent()

	// Auth-loss self-heal: when the app bounced an authenticated target to
	// the login page, re-login and retry the goto once before reporting.
	e.maybeRelogin(ctx, a.URL)

	title, _ := e.page.Title()
	text, _ := e.page.Locator("body").TextContent()

	statusCode := 0
	if resp != nil {
		statusCode = resp.Status()
	}

	if final := e.page.URL(); !strings.Contains(final, loginPageMarker) {
		e.lastTarget = a.URL
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

// expectWindowSeconds clamps a declared step timeout to [1,30] with default
// 10 (spec §8: a 30 s hard cap keeps a hung page from stalling the sweep).
func expectWindowSeconds(declared int) int {
	if declared <= 0 {
		return 10
	}
	if declared > 30 {
		return 30
	}
	return declared
}

// shotPath is the screenshot sink path: shots are keyed by case + label so a
// run's captures never collide (spec amendment A5).
func shotPath(projectDir, caseID, label string) string {
	return filepath.Join(projectDir, ".cerberus", "runtime", "shots", caseID+"-"+label+".png")
}

// failReason renders "" on pass so Err stays empty for successes.
func failReason(a types.BrowserExpectAction, pass bool, observed string) string {
	if pass {
		return ""
	}
	return fmt.Sprintf("expect %s %q: not satisfied within window (observed %q)", a.Expectation, a.Selector, observed)
}

// expectAssertion polls the locator until the comparator holds or the window
// expires. Polarity (amendment A2): text_absent fails FAST on appearance and
// passes only by outliving the window; every other comparator passes on the
// first hit. The per-case timeout in executeStep bounds the loop as a whole;
// the plain sleep keeps cancellation semantics simple.
//
// Auth-loss rescue (run37): an expect that runs out its window staring at the
// login page means the session died mid-case — re-login, return to the last
// authenticated page, and poll once more before reporting failure.
func (e *BrowserExecutor) expectAssertion(ctx context.Context, a types.BrowserExpectAction, start time.Time) types.ExecutorResult {
	res := e.pollExpectation(a, start)
	if res.OK {
		return res
	}
	if e.lastTarget != "" && needsRelogin(e.lastTarget, e.page.URL()) {
		e.maybeRelogin(ctx, e.lastTarget)
		if !needsRelogin(e.lastTarget, e.page.URL()) {
			res = e.pollExpectation(a, time.Now())
			res.Latency = time.Since(start)
		}
	}
	return res
}

func (e *BrowserExecutor) pollExpectation(a types.BrowserExpectAction, start time.Time) types.BrowserResult {
	window := time.Duration(expectWindowSeconds(a.Timeout)) * time.Second
	deadline := start.Add(window)
	locator := e.page.Locator(a.Selector)
	for {
		text, _ := locator.TextContent()
		count, _ := locator.Count()
		pass, obs, err := types.EvaluateBrowserExpectation(a.Expectation, strings.TrimSpace(text), count)
		if err != nil {
			return types.BrowserResult{OK: false, URL: e.page.URL(), Selector: a.Selector,
				Expectation: a.Expectation, Err: err.Error(), Latency: time.Since(start)}
		}
		// text_absent only ends by deadline (appearance returned above as a
		// fail); every other comparator ends on first pass or deadline.
		if pass || time.Now().After(deadline) {
			return types.BrowserResult{OK: pass, URL: e.page.URL(), Selector: a.Selector,
				Expectation: a.Expectation, Observed: obs, Latency: time.Since(start),
				Err: failReason(a, pass, obs)}
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// ScreenshotToFile captures the page into the run's shots dir and returns the
// path. Called by browser_shot steps and auto-captured on any step failure;
// evidence frames store the path, not the base64 payload (spec §6).
func (e *BrowserExecutor) ScreenshotToFile(caseID, label string) (string, error) {
	data, err := e.page.Screenshot(pw.PageScreenshotOptions{Type: pw.ScreenshotTypePng})
	if err != nil {
		return "", err
	}
	p := shotPath(e.projectDir, caseID, label)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, data, 0o644)
}
