package protocoldiscover

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// ErrNoProtocol signals the model found no WS protocol in the inputs. It is
// distinct from a hard error: the command reports it and exits cleanly rather
// than treating an unauthenticated channel as a failure.
var ErrNoProtocol = errors.New("no websocket protocol found")

// SourceFile is a doc/example file fed to the model. It mirrors
// authdiscover.SourceFile: {Path, Content}. Content never holds credential
// values — only public docs or message examples.
type SourceFile struct {
	Path    string
	Content string
}

// inferOutput is the JSON shape the LLM must return (mirrors authdiscover's
// discoverOutput). The Driver deserializes the response into this struct.
type inferOutput struct {
	Found    bool                   `json:"found"`
	Framing  string                 `json:"framing"`
	TypePath string                 `json:"type_path"`
	Auth     *inferAuth             `json:"auth,omitempty"`
	Roles    map[string]*inferRole  `json:"roles,omitempty"`
	Batches  map[string]*inferBatch `json:"batches,omitempty"`
	Notes    string                 `json:"notes"`
}

type inferBatch struct {
	ItemType  string `json:"item_type"`
	ItemsPath string `json:"items_path"`
}

type inferAuth struct {
	Strategy      string `json:"strategy"`
	Param         string `json:"param"`
	CredentialRef string `json:"credential_ref"`
}

type inferRole struct {
	CredentialRef string            `json:"credential_ref,omitempty"`
	Params        map[string]string `json:"params,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Subprotocols  []string          `json:"subprotocols,omitempty"`
	Handshake     *inferHandshake   `json:"handshake,omitempty"`
}

type inferHandshake struct {
	AwaitType string `json:"await_type"`
	Timeout   int    `json:"timeout"`
	Optional  bool   `json:"optional"`
}

// sampleOutcome classifies one inference attempt so the voter can count
// categories across samples instead of collapsing them into a single error.
type sampleOutcome int

const (
	outcomeFound    sampleOutcome = iota // a validated *project.Protocol
	outcomeNotFound                      // the model signalled found=false
	outcomeFailed                        // drift / parse / invalid / infra failure
)

// failReason is a non-leaking diagnostic tag for the all-failed error message.
// It never carries raw model output.
type failReason string

const (
	reasonDrift      failReason = "drift"
	reasonParse      failReason = "parse"
	reasonInvalid    failReason = "invalid"
	reasonInfra      failReason = "infra"
	reasonUngrounded failReason = "ungrounded"
)

// sample is the result of one inference attempt.
type sample struct {
	outcome sampleOutcome
	proto   *project.Protocol // set only when outcome == outcomeFound
	score   int               // set by selectProtocol when outcome == outcomeFound
	reason  failReason        // set only when outcome == outcomeFailed
	detail  string            // safe diagnostic (e.g. validation cause); never a credential value
}

// inferOnce runs a single protocol_draft inference and classifies the outcome.
// It never returns an error: every adverse result (drift, parse failure,
// validation failure, or a DecideWithTools error such as budget/retry/ctx
// exhaustion) becomes outcomeFailed so the voter can continue with the next
// sample. Systemic cancellation is handled by the voter's ctx.Err() check
// between samples, not here.
func inferOnce(ctx context.Context, driver *ai.Driver, cfg *project.Config, serviceName string, inputs []SourceFile) sample {
	prompt := buildInferPrompt(serviceName, actorNames(cfg), inputs)
	res, err := driver.DecideWithTools(ctx, prompt, []llm.Tool{protocolDraftTool()})
	if err != nil {
		// Per-sample-local: budget/retry/ctx. The voter short-circuits on
		// ctx.Err() between samples; here we just record and move on.
		return sample{outcome: outcomeFailed, reason: reasonInfra}
	}
	if len(res.ToolCalls) == 0 {
		return sample{outcome: outcomeFailed, reason: reasonDrift}
	}
	input := res.ToolCalls[0].Input
	if found, _ := input["found"].(bool); !found {
		return sample{outcome: outcomeNotFound}
	}
	p, err := argsToProtocol(input)
	if err != nil {
		return sample{outcome: outcomeFailed, reason: reasonParse}
	}
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

// validateGrounding checks that every hard literal the model cited — a role
// handshake's await_type and a batch's flush block — is backed by a verbatim
// source quote that actually appears in the input files. It reads the raw tool
// input map (not the assembled Protocol) so the transient `source` evidence
// never enters project.Protocol. A handshake/batch present without a source
// quote, or whose quote is not a (whitespace-normalized) substring of the
// joined input corpus, is "ungrounded". The error names only the failure mode;
// it never includes the raw quote or any model payload.
func validateGrounding(input map[string]any, inputs []SourceFile) error {
	var corp strings.Builder
	for _, f := range inputs {
		corp.WriteString(f.Content)
	}
	corpus := normalizeWS(corp.String())

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
			if !strings.Contains(corpus, normalizeWS(src)) {
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
			if !strings.Contains(corpus, normalizeWS(src)) {
				return errors.New("batch flush block ungrounded: source quote not found in inputs")
			}
		}
	}
	return nil
}

// normalizeWS collapses every run of whitespace (spaces, tabs, newlines) into a
// single space and trims the ends. validateGrounding compares normalized
// corpus against normalized quotes so a substantively-correct quote with
// different indentation than the source still matches (copy fidelity), while a
// genuine misquote (different tokens) still fails.
func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// selectProtocol applies the voting rules to N samples:
//   - >=1 Found  -> the highest-scoring Found (ties broken by earliest index).
//   - 0 Found, >=1 NotFound -> ErrNoProtocol.
//   - 0 Found, 0 NotFound (all Failed) -> a hard error with reason counts.
//
// Scoring is computed here (not in inferOnce) so the modal fields across all
// Found samples are known for the consensus tie-break.
func selectProtocol(samples []sample) (*project.Protocol, error) {
	modalFraming, modalTypePath := modalFields(samples)

	var best *sample
	for i := range samples {
		s := &samples[i]
		if s.outcome != outcomeFound {
			continue
		}
		s.score = scoreProtocol(s.proto, modalFraming, modalTypePath)
		if best == nil || s.score > best.score {
			best = s
		}
	}
	if best != nil {
		return best.proto, nil
	}
	for _, s := range samples {
		if s.outcome == outcomeNotFound {
			return nil, ErrNoProtocol
		}
	}
	return nil, fmt.Errorf("protocol inference failed across all samples: %s", summarizeFailures(samples))
}

// modalFields returns the most common Framing and TypePath across Found
// samples, for the consensus tie-break in scoreProtocol. Empty when no Found
// samples. Ties are broken deterministically (see modeOf).
func modalFields(samples []sample) (framing, typePath string) {
	fcount := map[string]int{}
	tcount := map[string]int{}
	for _, s := range samples {
		if s.outcome != outcomeFound || s.proto == nil {
			continue
		}
		fcount[s.proto.Framing]++
		tcount[s.proto.TypePath]++
	}
	return modeOf(fcount), modeOf(tcount)
}

// modeOf returns the key with the highest count, breaking ties by the
// lexicographically smallest key so the result is deterministic regardless of
// map iteration order. Returns "" when no key has a positive count.
func modeOf(counts map[string]int) string {
	var best string
	bestCount := 0
	for k, c := range counts {
		switch {
		case c > bestCount:
			best, bestCount = k, c
		case c == bestCount && c > 0 && (best == "" || k < best):
			best = k
		}
	}
	return best
}

// summarizeFailures renders a deterministic "N <reason>" summary of the Failed
// samples for the all-failed error message. It leaks no raw model output.
// When exactly one sample failed and it carries a safe diagnostic detail (e.g.
// a validation cause naming an actor), that detail is appended so the user gets
// an actionable message; multiple failures are summarized by reason only.
func summarizeFailures(samples []sample) string {
	counts := map[failReason]int{}
	var failed []sample
	for _, s := range samples {
		if s.outcome == outcomeFailed {
			counts[s.reason]++
			failed = append(failed, s)
		}
	}
	var parts []string
	for _, r := range []failReason{reasonInfra, reasonDrift, reasonParse, reasonInvalid, reasonUngrounded} {
		if counts[r] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[r], r))
		}
	}
	if len(parts) == 0 {
		return "no samples"
	}
	summary := strings.Join(parts, ", ")
	if len(failed) == 1 && failed[0].detail != "" {
		summary += ": " + failed[0].detail
	}
	return summary
}

// scoreProtocol ranks a validated draft so the voter can pick the strongest.
// The observed false-negative signature is omission — tail runs drop
// structures — so the score rewards drafts that recognized more (and harder)
// structures. Weights are opinionated but simple and intentionally untuned:
// "more structures beats fewer" is the dominant signal. The consensus bonuses
// (modalFraming/modalTypePath) only break ties; they never override coverage.
func scoreProtocol(p *project.Protocol, modalFraming, modalTypePath string) int {
	if p == nil {
		return 0
	}
	score := 0
	if p.TypePath != "" {
		score++
	}
	if p.Auth != nil {
		score++
	}
	score += len(p.Roles)
	score += len(p.Batches) * 2 // batching is a non-obvious structure; weight it
	handshakeRoles := 0
	for _, r := range p.Roles {
		if r != nil && r.Handshake != nil {
			handshakeRoles++
		}
	}
	score += handshakeRoles * 2 // handshake is the hardest structure; weight it
	if modalFraming != "" && p.Framing == modalFraming {
		score++
	}
	if modalTypePath != "" && p.TypePath == modalTypePath {
		score++
	}
	return score
}

// DefaultInferSamples is the default number of drafts Infer runs before
// selecting the strongest. N>1 absorbs run-to-run variance (false negatives,
// parse failures); see 2026-08-01-protocol-infer-n-sample-voting-design.md.
// Overridable via the protocol infer --samples flag.
const DefaultInferSamples = 3

// Infer asks the LLM to draft a protocol description from the given inputs,
// runs `samples` drafts, and returns the strongest validated one. samples < 1
// is clamped to 1 (single-shot). See inferOnce for the per-sample states and
// selectProtocol for the voting rules. The three-state contract is preserved:
// ErrNoProtocol when the consensus is "no protocol here"; a hard error only
// when every sample failed. Systemic cancellation short-circuits via ctx.Err().
func Infer(ctx context.Context, driver *ai.Driver, cfg *project.Config, serviceName string, inputs []SourceFile, samples int) (*project.Protocol, error) {
	if driver == nil {
		return nil, errors.New("nil driver")
	}
	if samples < 1 {
		samples = 1
	}
	results := make([]sample, 0, samples)
	for i := 0; i < samples; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		results = append(results, inferOnce(ctx, driver, cfg, serviceName, inputs))
	}
	return selectProtocol(results)
}

// actorsOf returns the config's actor list, used to confirm credential_ref
// names a real actor. Nil cfg yields nil (validation then rejects any ref).
func actorsOf(cfg *project.Config) []project.Actor {
	if cfg == nil {
		return nil
	}
	return cfg.Actors
}

// actorNames returns the declared actor names for prompt injection. The model
// needs the names (not the full records) to pick a credential_ref that passes
// validation.
func actorNames(cfg *project.Config) []string {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Actors))
	for _, a := range cfg.Actors {
		names = append(names, a.Name)
	}
	return names
}

// buildInferPrompt assembles the prompt. It describes WHAT to recognize and
// leaves the JSON shape to the protocol_draft tool schema (the tool definition
// carries it; the prompt no longer hand-writes a shape block). It never
// includes credential values — credential_ref names an actor, it is not a token.
// actors is the project's declared actor names; the prompt lists them so the
// model picks a real credential_ref instead of inventing one.
func buildInferPrompt(serviceName string, actors []string, inputs []SourceFile) string {
	var b strings.Builder
	b.WriteString("You are drafting a WebSocket protocol description for a cerberus test config.\n")
	b.WriteString("Read the docs/source below and call the protocol_draft tool once with what you infer.\n")
	fmt.Fprintf(&b, "The target service is %q.\n\n", serviceName)
	b.WriteString("Recognize and fill these structures where present:\n")
	b.WriteString("- Wire framing (json/text/binary) and the envelope: the dotted path to the message routing key (e.g. a {type,payload,...} envelope -> type_path \"type\").\n")
	b.WriteString("- How auth is attached (query param / header / subprotocol) and which actor supplies it.\n")
	b.WriteString("- Connection roles: distinct connection types (e.g. web, bridge) and their discriminator params/headers/subprotocols.\n")
	b.WriteString("- Post-connect handshake: a message sent in the connect/open handler (NOT in the message handler) right after connect. A send there guarded by a condition (e.g. only when a peer is online — `if (peers.length > 0) ws.send({type: X})`) is a peer-gated handshake: set optional=true so a timeout still succeeds the connect; an unconditional send is a mandatory handshake (optional=false). Set await_type to the EXACT `type:` string literal that send emits — copy it verbatim from the source (e.g. `devices:sync`), do not paraphrase or invent a name. You MUST also set handshake.source to a verbatim source snippet that contains BOTH the guard condition AND the emitted `type:` literal (copied exactly, contiguous, as it appears in the source); a snippet not found verbatim in the source is rejected.\n")
	b.WriteString("- Message batching: look for a timer/coalesce pattern — a handler that buffers items and flushes them on a setTimeout (or interval) as a DIFFERENT routing key (e.g. session:output buffered, then flushed as session:output-batch). Record the FLUSH key as the batch key, item_type as the original per-item routing key, and items_path as the FULL dotted path from the frame root to the array (e.g. `payload.lines`, not just `lines`). You MUST set the batch ... source field to a verbatim snippet of the flush-emit block (the line that types the batch routing key and holds the payload array); a snippet not found verbatim in the source is rejected.\n\n")
	b.WriteString("If no WebSocket protocol is described, call protocol_draft with found=false.\n")
	if len(actors) > 0 {
		fmt.Fprintf(&b, "credential_ref MUST be one of the actors declared in project.yaml: %s. Leave credential_ref empty rather than inventing a name, and NEVER include real credential values or tokens.\n\n", strings.Join(actors, ", "))
	} else {
		b.WriteString("No actors are declared in project.yaml; leave credential_ref empty rather than inventing a name, and NEVER include real credential values or tokens.\n\n")
	}
	for _, f := range inputs {
		b.WriteString("--- " + filepath.Base(f.Path) + " ---\n")
		b.WriteString(truncateContent(f.Content, 8000))
		b.WriteString("\n\n")
	}
	return b.String()
}

// truncateContent caps an input file's contribution to the prompt so a single
// huge doc cannot blow the context budget. n is a rune count (not bytes) so the
// cut never splits a multi-byte UTF-8 sequence, which would yield malformed
// output at the truncation point.
func truncateContent(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "\n…[truncated]"
}
