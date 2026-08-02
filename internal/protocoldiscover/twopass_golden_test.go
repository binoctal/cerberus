package protocoldiscover

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

// goldenRoomPath is the verbatim open-agents realtime source captured as a
// golden fixture. It is the WS dogfood target the detectors were validated
// against and the basis of the PASS 7/7 benchmark result.
const goldenRoomPath = "testdata/open-agents-room.ts.golden"

func loadGolden(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", goldenRoomPath))
	// tests run with the package dir as cwd, so the path is relative to the
	// package directory; fall back to a direct read for `go test ./...` from
	// the repo root.
	if err != nil {
		b, err = os.ReadFile(goldenRoomPath)
	}
	require.NoError(t, err, "golden fixture must be present")
	return string(b)
}

// TestGolden_RealSource_HandshakeAndBatch: against the REAL open-agents source
// (not a synthetic snippet), the deterministic detectors land the exact verbatim
// 7/7 values. This is the regression anchor: if a detector change stops landing
// devices:sync or session:output-batch/payload.lines on the real target, this
// test fails before the benchmark is re-run. Goldens capture current truth.
func TestGolden_RealSource_HandshakeAndBatch(t *testing.T) {
	corpus := loadGolden(t)

	// Handshake: the first `if (... > 0)` guard (sendOnlineDevices, line 209)
	// is followed within guardSendLookahead lines by a ws.send of 'devices:sync'.
	assert.Equal(t, "devices:sync", detectGuardedHandshake(corpus),
		"golden: guarded handshake must resolve to devices:sync on the real source")

	// Batch: setTimeout (batchOutput) + a broadcastToWeb of 'session:output-batch'
	// whose payload carries `lines: batch.lines`.
	flushKey, itemType, itemsPath := detectTimerFlushBatch(corpus)
	assert.Equal(t, "session:output-batch", flushKey, "golden: flush key")
	assert.Equal(t, "session:output", itemType, "golden: item_type is flush key minus -batch")
	assert.Equal(t, "payload.lines", itemsPath, "golden: items_path is frame-rooted payload.lines")

	// candidateLiterals seeds pass-2 windows. It is capped (20) and
	// order-preserving by first appearance, so the handshake literal — early in
	// the file — is surfaced, while the batch flush key — deep in the file —
	// falls beyond the cap. This is fine because batch detection does NOT depend
	// on candidateLiterals: detectTimerFlushBatch scans the whole corpus
	// directly. If that deterministic detector is ever removed, pass 2 would
	// lose the batch (a coupling to revisit when generalizing — see plan A).
	lits := candidateLiterals(corpus)
	assert.Contains(t, lits, "devices:sync", "handshake literal is early and surfaces")
	assert.NotContains(t, lits, "session:output-batch",
		"batch flush key is deep in the file and falls beyond the 20-cap; batch detection relies on detectTimerFlushBatch, not candidateLiterals")
	assert.LessOrEqual(t, len(lits), 20, "candidate list is capped at 20")
}

// --- Overfit boundary tests -------------------------------------------------
//
// The detectors are intentionally narrow (see the design spec's "Generality
// scope"). These negative assertions pin the edges the detectors deliberately
// do NOT match, so broadening a regex is a visible, test-breaking act rather
// than a silent behavior change. Generalization must be gated on a second,
// conventionally-diverse WS target (plan A).

// TestGuardShape_OnlyGreaterThanZero: guardRe matches `> 0` and rejects the
// other peer-gate shapes the detector does NOT cover (truthiness, >= 1, !== 0).
// A guarded send using any non-matching shape is NOT detected — recall loss,
// the documented boundary.
func TestGuardShape_OnlyGreaterThanZero(t *testing.T) {
	cases := map[string]string{
		"> 0 (matched)": "if (peers.length > 0) ws.send({type:'a:b'})",
		"truthiness":    "if (peers.length) ws.send({type:'a:b'})",
		">= 1":          "if (peers.length >= 1) ws.send({type:'a:b'})",
		"!== 0":         "if (peers.length !== 0) ws.send({type:'a:b'})",
	}
	matched, unmatched := []string{}, []string{}
	for name, src := range cases {
		if got := detectGuardedHandshake(src); got == "a:b" {
			matched = append(matched, name)
		} else {
			unmatched = append(unmatched, name)
		}
	}
	assert.Equal(t, []string{"> 0 (matched)"}, matched,
		"only the `> 0` shape is detected; these must stay unmatched: %v", unmatched)
}

// TestFlushKey_OnlyBatchSuffix: detectTimerFlushBatch fires only on a routing
// key ending in `-batch`. Other flush-key conventions (`:flush`, `:bulk`,
// `batch:` prefix) are a no-op (recall loss), not a precision risk.
func TestFlushKey_OnlyBatchSuffix(t *testing.T) {
	detect := func(key string) string {
		flushKey, _, _ := detectTimerFlushBatch(
			"setTimeout(flush, 50)\nbroadcastToWeb({ type: '" + key + "', payload: { lines: b.lines } })")
		return flushKey
	}
	assert.Equal(t, "session:output-batch", detect("session:output-batch"), "-batch suffix fires")
	for _, key := range []string{"session:output:flush", "session:output:bulk", "batch:session:output"} {
		assert.Equal(t, "", detect(key), "non-batch convention %q must be a no-op", key)
	}
}

// TestGuardedHandshake_FirstGuardWins: the detector returns the FIRST guarded
// send in corpus order. On open-agents the connect path precedes the handlers,
// so this is the handshake. A target whose first guarded send lives in a
// message/rate-limit handler would yield a SPURIOUS handshake — the documented
// precision risk that gates generality.
func TestGuardedHandshake_FirstGuardWins(t *testing.T) {
	// A rate-limit guard with its own send appears BEFORE the connect handshake.
	corpus := "if (remaining > 0) ws.send({type: 'rate:limit'})\n" +
		"// ... later, the real connect path ...\n" +
		"if (peers.length > 0) ws.send({type: 'devices:sync'})\n"
	assert.Equal(t, "rate:limit", detectGuardedHandshake(corpus),
		"first guarded send wins — a non-connect first guard yields a spurious handshake (known scope limit)")
}

// TestExtractItemsPath_NonArraySiblingFirst: extractItemsPath returns the FIRST
// buffer-property payload entry, so a payload whose first such entry is a
// non-array sibling leaf preceding the real array yields a SILENT WRONG path.
// This is the extractItemsPath overfit mode — correct on open-agents (lines is
// the first/only buffer field), wrong on richer payloads.
func TestExtractItemsPath_NonArraySiblingFirst(t *testing.T) {
	// count: batch.count precedes the real array lines: batch.lines.
	block := "type: 'session:output-batch',\n" +
		"payload: { sessionId, count: batch.count, lines: batch.lines }"
	got := extractItemsPath(block)
	assert.Equal(t, "payload.count", got,
		"overfit: extractItemsPath returns the first buffer-property leaf (count), not the array (lines) — silent wrong items_path")
}

// TestConnectRole_NoWebFallsBackLexicographic: connectRole prefers a role named
// "web"; with no web role it returns the lexicographically smallest role. That
// fallback is deterministic but semantically arbitrary — an added handshake may
// attach to a role that is not the connect side. The overfit: the heuristic
// cannot identify the connect-side role without the open-agents "web" name.
func TestConnectRole_NoWebFallsBackLexicographic(t *testing.T) {
	bridge := &project.ProtocolRole{}
	mobile := &project.ProtocolRole{}
	roles := map[string]*project.ProtocolRole{"bridge": bridge, "mobile": mobile}
	got := connectRole(roles)
	assert.Same(t, bridge, got,
		"overfit: with no 'web' role, connectRole picks the lex-smallest role (bridge) — arbitrary, not the connect side")
}
