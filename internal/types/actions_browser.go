package types

import "fmt"

// BrowserGotoAction represents browser navigation to a URL.
type BrowserGotoAction struct {
	// URL is the destination URL.
	URL string `json:"url"`
	// WaitUntil is an optional wait condition ("load", "domcontentloaded", "networkidle0").
	WaitUntil string `json:"wait_until,omitempty"`
}

func (a BrowserGotoAction) GetActionType() ActionType { return ActionBrowserGoto }
func (a BrowserGotoAction) Target() string            { return a.URL }
func (a BrowserGotoAction) Validate() error {
	if a.URL == "" {
		return fmt.Errorf("url is required")
	}
	return nil
}

// BrowserClickAction represents clicking an element on the page.
type BrowserClickAction struct {
	// Selector is the CSS selector for the element to click.
	Selector string `json:"selector"`
	// Text is the visible text of the element (for locating by text).
	Text string `json:"text,omitempty"`
	// Button is which mouse button to click ("left", "right", "middle").
	Button string `json:"button,omitempty"`
	// Modifiers are keyboard modifiers to hold ("Alt", "Control", "Meta", "Shift").
	Modifiers []string `json:"modifiers,omitempty"`
}

func (a BrowserClickAction) GetActionType() ActionType { return ActionBrowserClick }
func (a BrowserClickAction) Target() string            { return a.Selector }
func (a BrowserClickAction) Validate() error {
	if a.Selector == "" {
		return fmt.Errorf("selector is required")
	}
	return nil
}

// BrowserFillAction represents filling a form field.
type BrowserFillAction struct {
	// Selector is the CSS selector for the form field.
	Selector string `json:"selector"`
	// Value is the value to fill.
	Value string `json:"value"`
}

func (a BrowserFillAction) GetActionType() ActionType { return ActionBrowserFill }
func (a BrowserFillAction) Target() string            { return a.Selector }
func (a BrowserFillAction) Validate() error {
	if a.Selector == "" {
		return fmt.Errorf("selector is required")
	}
	if a.Value == "" {
		return fmt.Errorf("value is required")
	}
	return nil
}

// BrowserEvalAction represents executing JavaScript in the browser.
type BrowserEvalAction struct {
	// Expression is the JavaScript expression to evaluate.
	Expression string `json:"expression"`
	// Args are optional arguments to pass to the expression.
	Args []any `json:"args,omitempty"`
}

func (a BrowserEvalAction) GetActionType() ActionType { return ActionBrowserEval }
func (a BrowserEvalAction) Target() string {
	if a.Expression != "" {
		return a.Expression
	}
	return "<eval>"
}
func (a BrowserEvalAction) Validate() error {
	if a.Expression == "" {
		return fmt.Errorf("expression is required")
	}
	return nil
}

// BrowserExpectAction is a wait-type DOM assertion: poll the locator until it
// satisfies the comparator or the timeout expires. The one capability the
// goto/click/fill/eval quartet lacks — TextContent() does not wait, so async
// render makes instant checks flaky (spec 2026-08-26 §3.1).
type BrowserExpectAction struct {
	// Selector is a Playwright-engine selector (text=... | css=... | role=x[name=y]).
	Selector string `json:"selector"`
	// Expectation is the comparator: text_present | text_absent |
	// element_visible | element_count>=N.
	Expectation string `json:"expectation"`
	// Timeout is the wait window in seconds (default 10, hard cap 30).
	Timeout int `json:"timeout,omitempty"`
}

func (a BrowserExpectAction) GetActionType() ActionType { return ActionBrowserExpect }
func (a BrowserExpectAction) Target() string            { return a.Selector }
func (a BrowserExpectAction) Validate() error {
	if a.Selector == "" {
		return fmt.Errorf("selector is required")
	}
	if a.Expectation == "" {
		return fmt.Errorf("expectation comparator is required")
	}
	return nil
}
