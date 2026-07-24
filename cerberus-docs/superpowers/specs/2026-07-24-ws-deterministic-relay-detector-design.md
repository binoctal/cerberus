# WS Deterministic Relay Detector (F1 fork-3C) — Design

Status: Design (autonomous; chosen 2026-07-24 after the live dogfood showed the A1
LLM path does not emit `ws_relay` for GLM).
Trigger: `TestScoutRelayEmission_Live` (settings.json GLM key) confirmed Scout relay
generation's LLM step does not fire for GLM — the `ws_relay` prompt bullet did not
steer the model off the single-role pattern. The deterministic expander + executor
are proven; the gap is the LLM *emission* trigger. This design adds a deterministic
relay detector so the common, protocol-derivable relay (a peer-join signal) is
generated WITHOUT the LLM.

## Goal

When a service's protocol declares ≥2 roles and one role (A) has an **optional**
handshake awaiting type T, emit a deterministic multi-connection relay `Steps` case:
connect A (the signal receiver) → connect the peer role(s) (the joiner whose connect
triggers the signal) → receive T on A. This is exactly the open-agents
`device:online` peer-join relay the F1 dogfood validated, produced without an LLM.

## Why optional handshake = the trigger

An **optional** handshake (`optional: true`) is, by F2's definition, a peer-gated
signal: it does not arrive on a lone connect (the await times out, conn stays alive);
it arrives only when a peer joins. That is precisely a relayed peer-join signal. A
mandatory handshake is not peer-gated (it arrives alone), so it is not a relay
trigger. Deriving the relay from the optional-handshake declaration needs no goal
parsing and no LLM.

## Approach (resolved fork: F1 fork-3C, deterministic)

- **Trigger:** protocol with ≥2 roles AND a role A with `handshake.optional == true`
  and a non-empty `await_type` T.
- **Emit:** a relay `Steps` case — `ws_connect` A (receiver, first), `ws_connect`
  each peer role (sorted), `ws_receive` T on A (timeout from A's handshake timeout).
  Connect order is A-first (the signal receiver must be connected before the joiner
  connects, else the DO has no web to push to — F1 R3).
- **Suppress redundancy:** the per-role path already emits a single-connection
  `ws_receive T` for A (via `wsDecisiveTypes`), which would time out (T only arrives
  when a peer joins — not in a single-conn case). The detector records `(A, T)` as
  covered and the per-role loop skips emitting that receive. A's `ws_connect` and any
  other receive types are unaffected.
- **Coexist with A1 (LLM `ws_relay`):** unchanged. A1 handles richer relays (e.g.
  type-transform exchanges like `session:start`↔`session:created`) when a capable
  model emits them; this detector handles the protocol-derivable peer-join signal.
  The type-transform exchange is NOT derivable from the protocol alone (no optional
  handshake signals it) and stays with A1/future.

## Design

### `wsRelayCases(svc)` — new, in `internal/head/scout/ws_cases.go`

```go
// wsRelayCases emits deterministic multi-connection relay Steps cases for the
// peer-join signals in a service's protocol: each role with an OPTIONAL handshake
// (await_type T) receives T when a peer connects. Returns the cases plus the
// (role → set(signalType)) pairs they cover, so the per-role loop can skip the
// redundant single-connection receive (which would time out without the peer).
// Pure; no LLM. Deterministic (sorted roles).
func wsRelayCases(svc project.Service) ([]agent.TestCase, map[string]map[string]bool) {
	var cases []agent.TestCase
	covered := map[string]map[string]bool{}
	if svc.Protocol == nil || len(svc.Protocol.Roles) < 2 {
		return cases, covered
	}
	names := slices.Sorted(maps.Keys(svc.Protocol.Roles))
	for _, aName := range names {
		a := svc.Protocol.Roles[aName]
		if a == nil || a.Handshake == nil || !a.Handshake.Optional || a.Handshake.AwaitType == "" {
			continue
		}
		var peers []string
		for _, p := range names {
			if p != aName {
				peers = append(peers, p)
			}
		}
		signal := a.Handshake.AwaitType
		steps := []agent.TestStep{{Action: "ws_connect", ConnectionID: aName, Role: aName}}
		for _, p := range peers {
			steps = append(steps, agent.TestStep{Action: "ws_connect", ConnectionID: p, Role: p})
		}
		steps = append(steps, agent.TestStep{
			Action: "ws_receive", ConnectionID: aName, Type: signal, Timeout: a.Handshake.Timeout,
		})
		cases = append(cases, agent.TestCase{
			ID:          "ws-" + svc.Name + "-relay-" + aName + "-signal-" + sanitizeTypeID(signal),
			Name:        fmt.Sprintf("%s %s receives relayed %s on peer join", svc.Name, aName, signal),
			Service:     svc.Name, Target: svc.URL, Action: "ws_flow",
			Expectation: fmt.Sprintf("%s role %s receives %s relayed when a peer connects", svc.Name, aName, signal),
			Priority:    0.8, Steps: steps,
		})
		if covered[aName] == nil {
			covered[aName] = map[string]bool{}
		}
		covered[aName][signal] = true
	}
	return cases, covered
}
```

### `WSCasesCovered` integration

At the top of the per-service loop (after the protocol check), call the detector and
feed its covered map into the per-role receive loop:

```go
relayCases, relayCovered := wsRelayCases(svc)
cases = append(cases, relayCases...)
for _, roleName := range slices.Sorted(maps.Keys(svc.Protocol.Roles)) {
	if covered[svc.Name][roleName] { continue }
	// ... existing wsStepsCase / connect+receive ...
	for _, typ := range wsDecisiveTypes(role, goal) {
		if relayCovered[roleName][typ] {
			continue // the deterministic relay case already receives this signal
		}
		// ... existing ws_receive case ...
	}
}
```

`WSCases(cfg, goal)` (the wrapper) is unchanged.

## Behavior changes

- A ≥2-role protocol with an optional-handshake role now gets a deterministic
  multi-connection relay `Steps` case (the peer-join signal is actually receivable).
- The redundant single-conn `ws_receive` of that signal is suppressed (it would time
  out).
- No optional-handshake role / <2 roles ⇒ byte-identical to today.
- Existing WS/Steps/relay tests stay green.

## Constraints

- Go 1.25, pure-Go; `coder/websocket v1.8.14` ONLY; no new deps; no expression
  evaluator; no protocol-schema change.
- Production change confined to `ws_cases.go` (new `wsRelayCases` + `WSCasesCovered`
  integration) + docs. Executor/`runSteps`/`stepToAction`/`TestStep`/protocol-schema
  unchanged.
- Author `binoctal <binoctal@gmail.com>`; no Co-Authored-By; English; docs only in
  `cerberus-docs/`; `make check` green.
- Determinism: roles/signals iterated in sorted order; the assembled `Steps` order is
  fixed (A, peers sorted, receive).

## Testing

- `wsRelayCases`: emits a relay case for an optional-handshake role in a 2-role
  protocol (connect A first, then peer, then receive T on A); no emission for <2
  roles, a mandatory handshake, or no handshake; multiple optional-handshake roles ⇒
  one relay case each; covered map records (A, T).
- `WSCasesCovered`: the redundant single-conn receive-T for A is suppressed when the
  relay case covers it; other receives/connects unaffected; backwards-compat (no
  optional handshake ⇒ identical).
- **Re-run `TestScoutRelayEmission_Live`** (`//go:build live`): with the GLM key, the
  device:online relay goal now produces a multi-connection relay `Steps` case
  deterministically (closing the GLM gap the live dogfood found). This is the
  headline validation.

## Non-goals

- Detecting type-transform exchanges (`session:start`↔`session:created`) — not
  protocol-derivable; stays with A1 (LLM) or future.
- Connecting only a subset of peers (all peers are connected; the signal fires on the
  first join).
- Changing the LLM `ws_relay` (A1) path.
