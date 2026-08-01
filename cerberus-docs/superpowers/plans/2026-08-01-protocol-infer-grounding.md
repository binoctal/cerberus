# `protocol infer` Grounded Literals — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Force the model to ground the hard protocol literals (handshake `await_type`, batch flush block) in verbatim source quotes, and reject drafts whose quotes are absent from the inputs — so voting keeps only grounded drafts and wrong/hallucinated literals are dropped instead of emitted.

**Architecture:** Extend the `protocol_draft` tool schema with a `source` field on the handshake and batch sub-objects. Add a pure `validateGrounding(input, inputs)` that checks each cited `source` is a substring of the joined input corpus, and wire it into `inferOnce` as a new terminal step (`outcomeFailed` / `reasonUngrounded`). `source` is read from the raw tool input map and discarded — it never enters `project.Protocol`. Single LLM turn, no new orchestration; composes with N-sample voting.

**Tech Stack:** Go 1.25, `github.com/binoctal/cerberus`, `internal/llm` tool schema, `internal/protocoldiscover` (`inferOnce`, `selectProtocol`), `strings`.

## Global Constraints

- Commit author: `binoctal <binoctal@gmail.com>`, **no** `Co-Authored-By`.
- Commit messages and code comments in English.
- `make check` (fmt + lint + test) EXIT 0 + clean git tree after every task.
- No CGo. Follow existing comment density and naming idiom.
- Documents only in `cerberus-docs/` (never `docs/`).
- TDD: write the failing test, run it RED, implement, run it GREEN, commit.

## File Structure

- **Modify** `internal/protocoldiscover/tools.go` — `source` field in handshake + batch schema.
- **Modify** `internal/protocoldiscover/tools_test.go` — schema exposes `source`.
- **Modify** `internal/protocoldiscover/infer.go` — `reasonUngrounded`, `validateGrounding`, the new `inferOnce` step, `buildInferPrompt` citation guidance.
- **Modify** `internal/protocoldiscover/infer_test.go` — grounding unit tests; migrate the 2 handshake/batch-bearing existing tests; add voting-with-grounding tests.
- **Append** `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md` — grounded Run 14+ section.

---

## Task 1: `source` field in the tool schema

**Files:**
- Modify: `internal/protocoldiscover/tools.go`
- Test: `internal/protocoldiscover/tools_test.go`

**Interfaces:**
- Produces: `source` properties on the `handshake` and `batches.<key>` sub-schemas of `protocolDraftTool()`.

- [ ] **Step 1: Write the failing test**

Append to `internal/protocoldiscover/tools_test.go`:

```go
func TestProtocolDraftTool_HandshakeAndBatchExposeSource(t *testing.T) {
	tool := protocolDraftTool()
	top := tool.InputSchema
	props := top["properties"].(map[string]any)

	// roles.<role>.handshake.properties.source — the verbatim quote backing
	// await_type (must include the guard + type literal).
	rolesProp := props["roles"].(map[string]any)
	roleProps := rolesProp["additionalProperties"].(map[string]any)["properties"].(map[string]any)
	handshakeProps := roleProps["handshake"].(map[string]any)["properties"].(map[string]any)
	assert.Contains(t, handshakeProps, "source", "handshake schema must expose a source quote field")

	// batches.<key>.properties.source — the verbatim flush-emit block.
	batchesProp := props["batches"].(map[string]any)
	batchProps := batchesProp["additionalProperties"].(map[string]any)["properties"].(map[string]any)
	assert.Contains(t, batchProps, "source", "batch schema must expose a source quote field")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/protocoldiscover/ -run TestProtocolDraftTool_HandshakeAndBatchExposeSource -v`
Expected: FAIL — `source` not in handshake/batch properties.

- [ ] **Step 3: Add `source` to the schema**

In `internal/protocoldiscover/tools.go`, add a `"source"` entry to the handshake `properties` map (inside `protocolDraftTool`):

```go
							"handshake": map[string]any{"type": "object", "description": "Mandatory/best-effort post-connect exchange.", "properties": map[string]any{
								"await_type": str(),
								"timeout":    map[string]any{"type": "number"},
								"optional":   map[string]any{"type": "boolean", "description": "true = best-effort: a timeout still succeeds the connect (peer-gated handshake)."},
								"source":     map[string]any{"type": "string", "description": "Verbatim source snippet proving await_type. MUST include the guard condition (e.g. onlineDevices.length > 0) AND the emitted type: literal, copied exactly."},
							}},
```

and a `"source"` entry to the batch `properties` map:

```go
						"additionalProperties": map[string]any{"type": "object", "properties": map[string]any{
							"item_type":  str(),
							"items_path": str(),
							"source":     map[string]any{"type": "string", "description": "Verbatim snippet of the flush emit — the block that types the batch routing key and contains the payload array field."},
						}},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/protocoldiscover/ -run TestProtocolDraftTool -v`
Expected: PASS (all schema tests).

- [ ] **Step 5: `make check` + commit**

```bash
make check
git add internal/protocoldiscover/tools.go internal/protocoldiscover/tools_test.go
git commit -m "feat(protocoldiscover): protocol_draft schema adds source quotes for handshake/batch"
```

---

## Task 2: `validateGrounding` + `inferOnce` wiring

**Files:**
- Modify: `internal/protocoldiscover/infer.go`
- Test: `internal/protocoldiscover/infer_test.go`

**Interfaces:**
- Produces: `validateGrounding(input map[string]any, inputs []SourceFile) error`, `reasonUngrounded`, and the new `inferOnce` terminal step. A role handshake or batch present in the draft now REQUIRES a `source` quote found in the inputs, else the sample is `outcomeFailed`/`reasonUngrounded`.

**Migration note (must read before implementing):** grounding only fires when a draft contains a handshake or a batch. Auth-only / roles-only drafts are unaffected. Two existing tests carry handshake/batch fixtures and MUST be migrated in Step 4 so they stay GREEN: `TestInfer_RolesPopulated` and `TestInfer_Voting_PicksHigherScored`.

- [ ] **Step 1: Write the failing unit tests**

Append to `internal/protocoldiscover/infer_test.go`:

```go
func TestValidateGrounding_HandshakeSourcePresent(t *testing.T) {
	input := map[string]any{
		"roles": map[string]any{"web": map[string]any{
			"handshake": map[string]any{"await_type": "devices:sync", "source": "if (onlineDevices.length > 0) { ws.send({type: 'devices:sync'})"},
		}},
	}
	inputs := []SourceFile{{Content: "if (onlineDevices.length > 0) { ws.send({type: 'devices:sync'})"}}
	assert.NoError(t, validateGrounding(input, inputs))
}

func TestValidateGrounding_HandshakeSourceAbsent(t *testing.T) {
	input := map[string]any{
		"roles": map[string]any{"web": map[string]any{
			"handshake": map[string]any{"await_type": "devices:sync", "source": "if (onlineDevices.length > 0) { ws.send({type: 'devices:sync'})"},
		}},
	}
	err := validateGrounding(input, []SourceFile{{Content: "totally unrelated source"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ungrounded")
	// Leak guard: the raw quote must not echo back in the error.
	assert.NotContains(t, err.Error(), "devices:sync")
}

func TestValidateGrounding_HandshakeMissingSource(t *testing.T) {
	input := map[string]any{
		"roles": map[string]any{"web": map[string]any{
			"handshake": map[string]any{"await_type": "devices:sync"},
		}},
	}
	err := validateGrounding(input, []SourceFile{{Content: "whatever"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ungrounded")
}

func TestValidateGrounding_BatchSourceChecked(t *testing.T) {
	good := map[string]any{
		"batches": map[string]any{"session:output-batch": map[string]any{
			"item_type": "session:output", "items_path": "payload.lines",
			"source": "type: 'session:output-batch', payload: { lines }",
		}},
	}
	assert.NoError(t, validateGrounding(good, []SourceFile{{Content: "type: 'session:output-batch', payload: { lines }"}}))

	bad := map[string]any{
		"batches": map[string]any{"session:output-batch": map[string]any{
			"source": "type: 'session:output-batch', payload: { lines }",
		}},
	}
	err := validateGrounding(bad, []SourceFile{{Content: "no match here"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ungrounded")
}

func TestValidateGrounding_NoHardLiterals(t *testing.T) {
	// No handshake, no batches -> nothing to ground -> nil regardless of inputs.
	assert.NoError(t, validateGrounding(map[string]any{"found": true, "framing": "json"}, nil))
}

func TestInferOnce_UngroundedHandshake(t *testing.T) {
	cfg := cfgWithService()
	driver := mockToolDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"roles": map[string]any{"web": map[string]any{
			"credential_ref": "web",
			"handshake":      map[string]any{"await_type": "devices:sync", "source": "NOT IN INPUTS"},
		}},
	})
	s := inferOnce(context.Background(), driver, cfg, "rt", []SourceFile{{Content: "unrelated"}})
	assert.Equal(t, outcomeFailed, s.outcome)
	assert.Equal(t, reasonUngrounded, s.reason)
}

func TestInferOnce_GroundedHandshake(t *testing.T) {
	cfg := cfgWithService()
	quote := "if (onlineDevices.length > 0) { ws.send({type: 'devices:sync'})"
	driver := mockToolDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"roles": map[string]any{"web": map[string]any{
			"credential_ref": "web",
			"handshake":      map[string]any{"await_type": "devices:sync", "source": quote},
		}},
	})
	s := inferOnce(context.Background(), driver, cfg, "rt", []SourceFile{{Content: quote}})
	assert.Equal(t, outcomeFound, s.outcome)
	require.NotNil(t, s.proto)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/protocoldiscover/ -run "TestValidateGrounding|TestInferOnce_(Un|G)rounded" -v`
Expected: FAIL / compile error — `validateGrounding`, `reasonUngrounded` undefined.

- [ ] **Step 3: Implement `validateGrounding` and wire it into `inferOnce`**

In `internal/protocoldiscover/infer.go`, add the reason constant alongside the others:

```go
const (
	reasonDrift      failReason = "drift"
	reasonParse      failReason = "parse"
	reasonInvalid    failReason = "invalid"
	reasonInfra      failReason = "infra"
	reasonUngrounded failReason = "ungrounded"
)
```

Add `validateGrounding` (a pure function over the raw tool input map):

```go
// validateGrounding checks that every hard literal the model cited — a role
// handshake's await_type and a batch's flush block — is backed by a verbatim
// source quote that actually appears in the input files. It reads the raw tool
// input map (not the assembled Protocol) so the transient `source` evidence
// never enters project.Protocol. A handshake/batch present without a source
// quote, or whose quote is not a substring of the joined input corpus, is
// "ungrounded". The error names only the failure mode; it never includes the
// raw quote or any model payload.
func validateGrounding(input map[string]any, inputs []SourceFile) error {
	var corp strings.Builder
	for _, f := range inputs {
		corp.WriteString(f.Content)
	}
	corpus := corp.String()

	if roles, ok := input["roles"].(map[string]any); ok {
		for _, r := range roles {
			rm, ok := r.(map[string]any)
			if !ok {
				continue
			}
			hs, ok := rm["handshake"].(map[string]any)
			if !ok {
				continue
			}
			src, _ := hs["source"].(string)
			if strings.TrimSpace(src) == "" {
				return errors.New("handshake await_type ungrounded: no source quote")
			}
			if !strings.Contains(corpus, src) {
				return errors.New("handshake await_type ungrounded: source quote not found in inputs")
			}
		}
	}

	if batches, ok := input["batches"].(map[string]any); ok {
		for _, b := range batches {
			bm, ok := b.(map[string]any)
			if !ok {
				continue
			}
			src, _ := bm["source"].(string)
			if strings.TrimSpace(src) == "" {
				return errors.New("batch flush block ungrounded: no source quote")
			}
			if !strings.Contains(corpus, src) {
				return errors.New("batch flush block ungrounded: source quote not found in inputs")
			}
		}
	}
	return nil
}
```

Wire it into `inferOnce` after the `ValidateProtocol` step:

```go
	if err := project.ValidateProtocol(p, actorsOf(cfg)); err != nil {
		// The validation error references actor names, not credential values,
		// so it is safe to surface as actionable detail.
		return sample{outcome: outcomeFailed, reason: reasonInvalid, detail: err.Error()}
	}
	if err := validateGrounding(input, inputs); err != nil {
		return sample{outcome: outcomeFailed, reason: reasonUngrounded, detail: err.Error()}
	}
	return sample{outcome: outcomeFound, proto: p}
}
```

(`errors` is already imported by infer.go.)

`summarizeFailures` builds its summary from a fixed ordered reason list — add `reasonUngrounded` so ungrounded samples appear in the all-failed error message. In `summarizeFailures`, change the ordered iteration:

```go
	for _, r := range []failReason{reasonInfra, reasonDrift, reasonParse, reasonInvalid, reasonUngrounded} {
```

- [ ] **Step 4: Migrate the 2 handshake/batch-bearing existing tests**

Grounding now rejects a handshake/batch without a matching `source` quote. Both fixtures below add a `source` and pass an input file whose content contains the quote verbatim.

In `TestInfer_RolesPopulated`, change the handshake map and the `Infer` inputs:

```go
	driver := mockToolDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
		"roles": map[string]any{
			"web": map[string]any{
				"credential_ref": "web",
				"params":         map[string]any{"type": "web"},
				"handshake":      map[string]any{"await_type": "ready", "timeout": 5, "source": "if (ready) ws.send({type: 'ready'})"},
			},
		},
	})
	p, err := Infer(context.Background(), driver, cfg, "rt", []SourceFile{{Path: "room.ts", Content: "if (ready) ws.send({type: 'ready'})"}}, 1)
```

In `TestInfer_Voting_PicksHigherScored`, ground the `complete` draft's handshake and batch, and pass a corpus containing both quotes to the `Infer` call:

```go
	handshakeQuote := "if (onlineDevices.length > 0) { ws.send({type: 'devices:sync'})"
	batchQuote := "type: 'session:output-batch', payload: { lines }"
	complete := validInput()
	complete["roles"] = map[string]any{
		"web": map[string]any{
			"credential_ref": "web",
			"handshake":      map[string]any{"await_type": "devices:sync", "timeout": 5, "source": handshakeQuote},
		},
	}
	complete["batches"] = map[string]any{
		"session:output-batch": map[string]any{"item_type": "session:output", "items_path": "payload.lines", "source": batchQuote},
	}
	driver := mockSequenceDriver(t, []map[string]any{partial, complete, partial})
	corpus := []SourceFile{{Content: handshakeQuote + "\n" + batchQuote}}
	p, err := Infer(context.Background(), driver, cfgWithService(), "rt", corpus, 3)
```

(Leave the `partial` map and the trailing assertions unchanged.)

- [ ] **Step 5: Run all protocoldiscover tests**

Run: `go test ./internal/protocoldiscover/ -v`
Expected: all PASS — new grounding tests green; the 2 migrated tests green; auth-only tests unaffected.

- [ ] **Step 6: `make check` + commit**

```bash
make check
git add internal/protocoldiscover/infer.go internal/protocoldiscover/infer_test.go
git commit -m "feat(protocoldiscover): validateGrounding drops ungrounded handshake/batch literals"
```

---

## Task 3: Citation guidance in the prompt

**Files:**
- Modify: `internal/protocoldiscover/infer.go` (`buildInferPrompt`)
- Test: `internal/protocoldiscover/infer_test.go` (`TestBuildInferPrompt_RecognitionGuidance`)

**Interfaces:**
- Produces: prompt text instructing the model to populate `handshake.source` (guard + type literal) and `batch.source` (flush block), copied verbatim.

- [ ] **Step 1: Update the prompt test**

In `TestBuildInferPrompt_RecognitionGuidance`, add assertions that the prompt demands verbatim `source` quotes for handshake and batch. After the existing `assert.Contains(t, prompt, "verbatim", ...)` line, add:

```go
	// Grounding: the prompt must require a verbatim source quote for the
	// handshake (guard + type literal) and the batch flush block, copied so it
	// can be substring-matched against the inputs.
	assert.Contains(t, prompt, "handshake.source", "prompt must require a handshake source quote")
	assert.Contains(t, prompt, "batch ... source", "prompt must require a batch flush-block source quote")
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/protocoldiscover/ -run TestBuildInferPrompt_RecognitionGuidance -v`
Expected: FAIL — the prompt does not yet mention `handshake.source`.

- [ ] **Step 3: Add citation guidance to `buildInferPrompt`**

In `internal/protocoldiscover/infer.go`, replace the handshake bullet and append to the batching bullet. The handshake bullet currently ends with `...do not paraphrase or invent a name.\n`. Replace that bullet's final sentence with one that also demands the source quote, and extend the batch bullet similarly:

Replace:
```go
	b.WriteString("- Post-connect handshake: a message sent in the connect/open handler (NOT in the message handler) right after connect. A send there guarded by a condition (e.g. only when a peer is online — `if (peers.length > 0) ws.send({type: X})`) is a peer-gated handshake: set optional=true so a timeout still succeeds the connect; an unconditional send is a mandatory handshake (optional=false). Set await_type to the EXACT `type:` string literal that send emits — copy it verbatim from the source (e.g. `devices:sync`), do not paraphrase or invent a name.\n")
	b.WriteString("- Message batching: look for a timer/coalesce pattern — a handler that buffers items and flushes them on a setTimeout (or interval) as a DIFFERENT routing key (e.g. session:output buffered, then flushed as session:output-batch). Record the FLUSH key as the batch key, item_type as the original per-item routing key, and items_path as the FULL dotted path from the frame root to the array (e.g. `payload.lines`, not just `lines`).\n\n")
```

with:
```go
	b.WriteString("- Post-connect handshake: a message sent in the connect/open handler (NOT in the message handler) right after connect. A send there guarded by a condition (e.g. only when a peer is online — `if (peers.length > 0) ws.send({type: X})`) is a peer-gated handshake: set optional=true so a timeout still succeeds the connect; an unconditional send is a mandatory handshake (optional=false). Set await_type to the EXACT `type:` string literal that send emits — copy it verbatim from the source (e.g. `devices:sync`), do not paraphrase or invent a name. You MUST also set handshake.source to a verbatim source snippet that contains BOTH the guard condition AND the emitted `type:` literal (copied exactly, contiguous, as it appears in the source); a snippet not found verbatim in the source is rejected.\n")
	b.WriteString("- Message batching: look for a timer/coalesce pattern — a handler that buffers items and flushes them on a setTimeout (or interval) as a DIFFERENT routing key (e.g. session:output buffered, then flushed as session:output-batch). Record the FLUSH key as the batch key, item_type as the original per-item routing key, and items_path as the FULL dotted path from the frame root to the array (e.g. `payload.lines`, not just `lines`). You MUST set the batch ... source field to a verbatim snippet of the flush-emit block (the line that types the batch routing key and holds the payload array); a snippet not found verbatim in the source is rejected.\n\n")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/protocoldiscover/ -v`
Expected: all PASS.

- [ ] **Step 5: `make check` + commit**

```bash
make check
git add internal/protocoldiscover/infer.go internal/protocoldiscover/infer_test.go
git commit -m "feat(protocoldiscover): prompt requires verbatim source quotes for handshake/batch"
```

---

## Task 4: Dogfood grounded literals against `open-agents`

**Files:**
- Append: `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md`

Manual run + record; no Go test. Tests whether grounding citation lands the verbatim `devices:sync` (the single residual failure from the voting pass).

- [ ] **Step 1: Build and start the target**

```
make build
cd /home/mason/Documents/code_projects/private/open-agents
fnm use 22
cd apps/api && npm run dev    # wrangler dev, port 8989
```

Confirm `curl -sf http://localhost:8989/health` returns ok. Stop and record a setup blocker if not.

- [ ] **Step 2: Run `protocol infer` (default N=3, now with grounding)**

From the `open-agents` repo root:

```
cerberus protocol infer --name open-agents \
  --from apps/api/src/realtime --service api --dry-run
```

Run it 4–5 times. For each run capture the drafted YAML and note specifically: does any emitted handshake carry `await_type: devices:sync` (the verbatim source literal), and is its `source` quote present in `room.ts`? Did ungrounded handshakes get dropped (no handshake emitted rather than a wrong one)?

- [ ] **Step 3: Append the Run 14+ section**

Append to `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md` a section titled `## Grounded literals — YYYY-MM-DD` (use the run date) containing:
- The default invocation.
- A per-run table: outcome (draft / hard error reason), handshake `await_type` when present (devices:sync vs other), batch flush key/items_path.
- An honest verdict: did grounding land `devices:sync`? How often was a handshake emitted vs omitted (honest degradation)? Did any run surface an "ungrounded" all-failed error?
- Note whether the exact-match substring check rejected legitimate-but-imperfect quotes (copy fidelity) — if so, flag the normalizing-matcher follow-up.

- [ ] **Step 4: Commit**

```bash
git add cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md
git commit -m "docs(dogfood): protocol infer grounded literals run against open-agents"
```

---

## Self-Review

**Spec coverage:** spec §Tool schema change (`source` on handshake + batch) → T1. §`validateGrounding` + §`inferOnce` flow (new ungrounded step) + `reasonUngrounded` → T2. §Prompt change → T3. §Testing (validateGrounding table, inferOnce ungrounded/grounded, leak guard) → T2. §Interaction with voting → exercised by the migrated `TestInfer_Voting_PicksHigherScored` (grounded complete wins). §Dogfood → T4. §Non-goals (`Protocol` unchanged — `source` read from raw map and discarded; no second LLM call) → respected throughout. All sections mapped.

**Placeholder scan:** No TBD/TODO/"implement later". Every code block is complete and runnable. T2 Step 4 migration gives the exact fixture edits. T4 is a manual run with a concrete capture/checklist.

**Type consistency:** `validateGrounding(input map[string]any, inputs []SourceFile) error` (T2) matches its test calls and the `inferOnce` call site (T2 Step 3). `reasonUngrounded failReason = "ungrounded"` (T2) is added to `summarizeFailures`'s ordered reason list (T2 Step 3) so ungrounded samples surface in the all-failed summary. The `source` field is read from `map[string]any` in `validateGrounding` and in the schema (T1); it is never added to `inferHandshake`/`inferBatch`, so `argsToProtocol` and `project.Protocol` are untouched, matching the spec's non-goal. The migrated `TestInfer_Voting_PicksHigherScored` passes a `corpus []SourceFile` to `Infer`, matching the `inputs` parameter threaded through to `validateGrounding`.
