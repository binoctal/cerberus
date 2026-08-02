package protocoldiscover

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// signalWindow is a slice of source extracted around a candidate literal,
// fed to pass 2 so the model reads a small anchored region instead of the
// whole file.
type signalWindow struct {
	literal string
	text    string
}

// windowRadius is the number of source lines above and below each candidate
// literal that extractWindows includes. A few lines of context are enough to
// judge whether a send is guarded or whether an emit is a timer flush.
const windowRadius = 3

// extractWindows returns one window per distinct literal that appears in the
// corpus (±radius lines around its first match). Literals absent from the
// corpus (invented) yield no window. Pure.
func extractWindows(corpus string, literals []string, radius int) []signalWindow {
	lines := strings.Split(corpus, "\n")
	var out []signalWindow
	seen := map[string]bool{}
	for _, lit := range literals {
		if lit == "" || seen[lit] {
			continue
		}
		idx := substringLineIndex(lines, lit)
		if idx < 0 {
			continue
		}
		lo := idx - radius
		if lo < 0 {
			lo = 0
		}
		hi := idx + radius + 1
		if hi > len(lines) {
			hi = len(lines)
		}
		out = append(out, signalWindow{literal: lit, text: strings.Join(lines[lo:hi], "\n")})
		seen[lit] = true
	}
	return out
}

// substringLineIndex returns the 0-based index of the first line containing
// sub, or -1.
func substringLineIndex(lines []string, sub string) int {
	for i, l := range lines {
		if strings.Contains(l, sub) {
			return i
		}
	}
	return -1
}

// routingKeyRe matches a quoted colon-shaped routing-key literal such as
// 'devices:sync' or "session:output-batch". Requiring quotes keeps the scan off
// unquoted prose; over-matching is harmless because pass 2 only confirms a
// window that shows a guarded send / timer flush.
var routingKeyRe = regexp.MustCompile(`["']([A-Za-z][\w]*(?::[\w-]+)+)["']`)

// candidateLiterals scans the corpus for routing-key-shaped string literals so
// pass 2 can anchor on them EVEN WHEN pass 1 omitted the hard structure. This
// is the recall fix: anchoring no longer depends on the model naming the
// literal in pass 1. Deduped, order-preserving, capped.
func candidateLiterals(corpus string) []string {
	matches := routingKeyRe.FindAllStringSubmatch(corpus, -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		lit := m[1]
		if lit == "" || seen[lit] {
			continue
		}
		seen[lit] = true
		out = append(out, lit)
		if len(out) >= 20 {
			break
		}
	}
	return out
}

// guardRe matches a conditional guard that gates a peer-gated send, e.g.
// `if (onlineDevices.length > 0)`. The [^=] before > avoids matching `>=`/`=>`.
var guardRe = regexp.MustCompile(`if\s*\(.*[^=]>[=]?\s*0\b`)

// detectGuardedHandshake locates a peer-gated post-connect send deterministically:
// a conditional guard followed within a few lines by a send/broadcast carrying a
// routing-key literal. Returns that literal (the handshake await_type) or "".
//
// This is the code-side "locate" for the handshake. It exists because the N=18
// benchmark showed pass 2's LLM judgment of the same anchored window is
// unreliable for this pattern — the deterministic detector is. When it fires,
// the caller attaches the handshake without depending on pass 2.
func detectGuardedHandshake(corpus string) string {
	lines := strings.Split(corpus, "\n")
	for i, line := range lines {
		if !guardRe.MatchString(line) {
			continue
		}
		hi := i + guardSendLookahead
		if hi > len(lines) {
			hi = len(lines)
		}
		window := strings.Join(lines[i:hi], "\n")
		if !strings.Contains(window, "send(") && !strings.Contains(window, "broadcastToWeb(") {
			continue
		}
		if m := routingKeyRe.FindStringSubmatch(window); m != nil {
			return m[1]
		}
	}
	return ""
}

// guardSendLookahead is the number of lines after a guard to scan for the send.
const guardSendLookahead = 4

// defaultHandshakeTimeout is the timeout (seconds) applied to an added
// handshake; ValidateProtocol requires timeout > 0.
const defaultHandshakeTimeout = 5

// setTimeoutRe matches the timer primitive that triggers a batch flush.
var setTimeoutRe = regexp.MustCompile(`setTimeout|setInterval`)

// arrayFieldRe matches a payload field assigned a bare property access into a
// buffer (e.g. `lines: batch.lines`). Function-call values (`timestamp:
// Date.now()`) are filtered in extractItemsPath (RE2 has no lookahead).
var arrayFieldRe = regexp.MustCompile(`(\w+):\s*[A-Za-z_]\w*\.[A-Za-z_]\w*`)

// containsSend reports whether a line opens a frame send/broadcast.
func containsSend(line string) bool {
	return strings.Contains(line, "broadcastToWeb(") ||
		strings.Contains(line, "broadcast(") ||
		strings.Contains(line, ".send(")
}

// detectTimerFlushBatch locates a timer-flush batch deterministically: a
// setTimeout/setInterval in the corpus plus a send/broadcast whose routing key
// ends in `-batch`, with the array field carried in its payload. Returns
// flush_key / item_type / items_path (any empty when not found).
//
// Like detectGuardedHandshake, this is the code-side "locate" for the batch —
// done deterministically because pass 2's LLM judgment proved unreliable. The
// `-batch` suffix convention yields item_type; the payload's buffer-array field
// yields items_path (frame-rooted via `payload.`).
func detectTimerFlushBatch(corpus string) (flushKey, itemType, itemsPath string) {
	if !setTimeoutRe.MatchString(corpus) {
		return "", "", ""
	}
	lines := strings.Split(corpus, "\n")
	for i, l := range lines {
		if !containsSend(l) {
			continue
		}
		hi := i + flushEmitLookahead
		if hi > len(lines) {
			hi = len(lines)
		}
		block := strings.Join(lines[i:hi], "\n")
		var batchKey string
		for _, m := range routingKeyRe.FindAllStringSubmatch(block, -1) {
			if strings.HasSuffix(m[1], "-batch") {
				batchKey = m[1]
				break
			}
		}
		if batchKey == "" {
			continue
		}
		return batchKey, strings.TrimSuffix(batchKey, "-batch"), extractItemsPath(block)
	}
	return "", "", ""
}

// flushEmitLookahead is the number of lines after a send opening scanned for the
// routing key and payload (multi-line object literal).
const flushEmitLookahead = 6

// extractItemsPath reads the items_path off a flush emit block: the payload
// field that holds the buffer array, prefixed `payload.` when the emit carries
// a payload object. Empty when no array field is found. Function-call values
// (e.g. `timestamp: Date.now()`) are skipped — the array field is a bare buffer
// property access (`lines: batch.lines`).
func extractItemsPath(block string) string {
	hasPayload := strings.Contains(block, "payload")
	region := block
	if hasPayload {
		if idx := strings.Index(block, "payload"); idx >= 0 {
			region = block[idx:]
		}
	}
	for _, loc := range arrayFieldRe.FindAllStringSubmatchIndex(region, -1) {
		// Skip function-call values: the match must not be followed by "(".
		rest := strings.TrimLeft(region[loc[1]:], " \t")
		if strings.HasPrefix(rest, "(") {
			continue
		}
		leaf := region[loc[2]:loc[3]]
		if hasPayload {
			return "payload." + leaf
		}
		return leaf
	}
	return ""
}

// confirmSignalsTool is the pass-2 tool. The model has the anchored windows in
// its prompt (not in the tool input) and reports whether a guarded post-connect
// handshake and/or a timer-flush batch is present among them, transcribing the
// exact literals off the windows.
func confirmSignalsTool() llm.Tool {
	str := func() map[string]any { return map[string]any{"type": "string"} }
	return llm.Tool{
		Name:        "confirm_signals",
		Description: "Confirm the guarded post-connect handshake and the timer-flush batch by reading the provided anchored source windows. Call once.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"handshake": map[string]any{"type": "object", "properties": map[string]any{
					"present":    map[string]any{"type": "boolean", "description": "true if a window shows a post-connect send guarded by a condition (peer-gated handshake)."},
					"await_type": str(),
				}},
				"batch": map[string]any{"type": "object", "properties": map[string]any{
					"present":    map[string]any{"type": "boolean", "description": "true if a window shows a timer-flush emit coalescing items under a different routing key."},
					"flush_key":  str(),
					"item_type":  str(),
					"items_path": str(),
				}},
			},
			"required": []any{"handshake", "batch"},
		},
	}
}

// buildConfirmPrompt renders the anchored windows and instructs pass-2
// selection. The literal "ANCHORED SOURCE WINDOWS" doubles as the MockClient
// routing key in tests (it does not appear in pass-1 prompts).
func buildConfirmPrompt(windows []signalWindow) string {
	var b strings.Builder
	b.WriteString("ANCHORED SOURCE WINDOWS\n\n")
	b.WriteString("You are reading ONLY the anchored source windows below (each extracted around a candidate literal the first pass named). Judge them in isolation.\n\n")
	for i, w := range windows {
		fmt.Fprintf(&b, "--- window %d (around %q) ---\n%s\n\n", i+1, w.literal, w.text)
	}
	b.WriteString("Call confirm_signals once:\n")
	b.WriteString("- handshake.present=true ONLY if a window shows a post-connect send guarded by a condition (e.g. `if (peers.length > 0) ws.send(...)`); set await_type to the EXACT type: literal that guarded send emits. Otherwise present=false.\n")
	b.WriteString("- batch.present=true ONLY if a window shows a timer/flush emit that coalesces items under a DIFFERENT routing key; set flush_key (the flush routing key), item_type (the pre-batch routing key), and items_path as the FULL dotted path from the FRAME ROOT to the array (e.g. `payload.lines` — the key on the emitted frame's payload — NOT a buffer variable name like `batch.lines`). Otherwise present=false.\n")
	return b.String()
}

// signalConfirmation is the pass-2 verdict on the hard literals.
type signalConfirmation struct {
	handshakePresent bool
	awaitType        string
	batchPresent     bool
	flushKey         string
	itemType         string
	itemsPath        string
}

// joinCorpus concatenates input file contents into the search corpus.
func joinCorpus(inputs []SourceFile) string {
	var b strings.Builder
	for _, f := range inputs {
		b.WriteString(f.Content)
	}
	return b.String()
}

// hasHardStructure reports whether the draft carries anything pass 2 can
// refine (a role handshake or a batch).
func hasHardStructure(p *project.Protocol) bool {
	if len(p.Batches) > 0 {
		return true
	}
	for _, r := range p.Roles {
		if r != nil && r.Handshake != nil {
			return true
		}
	}
	return false
}

// refineSignals runs pass 2: gather candidate literals from the pass-1 draft,
// code-extract anchored windows, and (if any) ask the model to select the
// guarded handshake / flush off those windows. Returns (confirmation, failed):
// failed=true means the pass-2 call itself errored or drifted (the sample is
// dropped by voting). An empty candidate set (invented literals) returns a
// zero confirmation with failed=false — absence, not failure.
func refineSignals(ctx context.Context, driver *ai.Driver, draft *project.Protocol, inputs []SourceFile) (signalConfirmation, bool) {
	corpus := joinCorpus(inputs)
	var literals []string
	for _, r := range draft.Roles {
		if r != nil && r.Handshake != nil && r.Handshake.AwaitType != "" {
			literals = append(literals, r.Handshake.AwaitType)
		}
	}
	for key := range draft.Batches {
		literals = append(literals, key)
	}
	// Recall fix: also seed candidates directly from the corpus, so pass 2 can
	// anchor even when pass 1 omitted the hard structure entirely.
	literals = append(literals, candidateLiterals(corpus)...)
	windows := extractWindows(corpus, literals, windowRadius)
	if len(windows) == 0 {
		return signalConfirmation{}, false
	}
	res, err := driver.DecideWithTools(ctx, buildConfirmPrompt(windows), []llm.Tool{confirmSignalsTool()})
	if err != nil || len(res.ToolCalls) == 0 {
		return signalConfirmation{}, true
	}
	return parseConfirmation(res.ToolCalls[0].Input, corpus), false
}

// parseConfirmation reads the confirm_signals tool input into a
// signalConfirmation, rejecting literals not present in the corpus (the model
// may still invent off-window) and batches whose flush_key and items_path leaf
// do not co-occur locally (a pass-2 mis-fire where a handshake literal leaks
// into the batch). Leak-safe: carries no raw payload.
func parseConfirmation(input map[string]any, corpus string) signalConfirmation {
	var c signalConfirmation
	if hs, ok := input["handshake"].(map[string]any); ok {
		c.handshakePresent, _ = hs["present"].(bool)
		c.awaitType, _ = hs["await_type"].(string)
		if c.handshakePresent && c.awaitType != "" && !strings.Contains(corpus, c.awaitType) {
			c.handshakePresent = false
		}
	}
	if b, ok := input["batch"].(map[string]any); ok {
		c.batchPresent, _ = b["present"].(bool)
		c.flushKey, _ = b["flush_key"].(string)
		c.itemType, _ = b["item_type"].(string)
		c.itemsPath, _ = b["items_path"].(string)
		if c.batchPresent {
			if c.flushKey != "" && !strings.Contains(corpus, c.flushKey) {
				c.batchPresent = false
			}
			if c.itemType != "" && !strings.Contains(corpus, c.itemType) {
				c.batchPresent = false
			}
			// Coherence: the array field must live in the SAME flush emit as
			// the routing key. A leaf that only appears elsewhere (e.g. a
			// handshake literal leaked into items_path) is a mis-fire — drop
			// the batch rather than emit a wrong value.
			if c.batchPresent && !coherentBatch(corpus, c.flushKey, c.itemsPath) {
				c.batchPresent = false
			}
		}
	}
	return c
}

// coherentBatch reports whether the items_path leaf field appears within
// ±windowRadius lines of the flush_key in the corpus — i.e. the flush emit
// block carries the array. This catches pass-2 mis-fires where a literal from
// a different construct (e.g. the handshake devices:sync) leaks into the
// batch fields.
func coherentBatch(corpus, flushKey, itemsPath string) bool {
	leaf := itemsPath
	if i := strings.LastIndex(itemsPath, "."); i >= 0 {
		leaf = itemsPath[i+1:]
	}
	if leaf == "" {
		return false
	}
	lines := strings.Split(corpus, "\n")
	idx := substringLineIndex(lines, flushKey)
	if idx < 0 {
		return false
	}
	lo := idx - windowRadius
	if lo < 0 {
		lo = 0
	}
	hi := idx + windowRadius + 1
	if hi > len(lines) {
		hi = len(lines)
	}
	return strings.Contains(strings.Join(lines[lo:hi], "\n"), leaf)
}

// mergeConfirmation applies the pass-2 verdict to the draft: keep a handshake
// only if confirmed (overwriting await_type, preserving timeout/optional), and
// replace batches with the single confirmed batch (or drop them). When pass 1
// omitted a handshake but pass 2 confirmed one, the handshake is ADDED to the
// connect-side role — the recall fix that lets two-pass land structures pass 1
// never emitted.
func mergeConfirmation(p *project.Protocol, c signalConfirmation) {
	anyKept := false
	for _, r := range p.Roles {
		if r == nil || r.Handshake == nil {
			continue
		}
		if c.handshakePresent && c.awaitType != "" {
			r.Handshake.AwaitType = c.awaitType
			anyKept = true
		} else {
			r.Handshake = nil
		}
	}
	if c.handshakePresent && c.awaitType != "" && !anyKept {
		// Pass 1 left no handshake on any role; attach the confirmed one to the
		// connect-side role. These handshakes are peer-gated, so default optional.
		if role := connectRole(p.Roles); role != nil {
			role.Handshake = &project.RoleHandshake{AwaitType: c.awaitType, Timeout: defaultHandshakeTimeout, Optional: true}
		}
	}
	if c.batchPresent && c.flushKey != "" && c.itemType != "" && c.itemsPath != "" {
		p.Batches = map[string]*project.ProtocolBatch{c.flushKey: {ItemType: c.itemType, ItemsPath: c.itemsPath}}
	} else {
		p.Batches = nil
	}
}

// connectRole returns the role most likely to be the post-connect side for an
// added handshake: the "web" role when present, else the lexicographically
// smallest named role (deterministic). Nil when there are no roles.
func connectRole(roles map[string]*project.ProtocolRole) *project.ProtocolRole {
	if r := roles["web"]; r != nil {
		return r
	}
	names := make([]string, 0, len(roles))
	for n := range roles {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if roles[n] != nil {
			return roles[n]
		}
	}
	return nil
}
