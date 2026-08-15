// Package claimsdiscover extracts the claims ledger (the product's own
// falsifiable promises) from the SUT's docs via one LLM call, triages drafts
// against the project's declared surfaces, and merges drafts into an existing
// ledger preserving manual channels (the vocab re-extraction merge rule).
package claimsdiscover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// DefaultMaxClaims caps extraction (prompt contract: ≤ max, default 15).
const DefaultMaxClaims = 15

// AnnotationNoSurface is set on claims that map to no discovered surface, so
// the hard gate stays free of un-testable noise (spec amendment #2). Only ever
// lands on NEW claims: MergeClaims preserves annotations on existing ids.
const AnnotationNoSurface = "no surface mapping"

// ErrNoClaims signals the model returned no usable claims — drift or genuinely
// claim-free docs. Distinct from a hard error: the command reports it and
// writes nothing rather than clobbering a ledger.
var ErrNoClaims = errors.New("no claims extracted")

// extractOutput is the JSON shape the claims_extract tool must return.
// draftClaim carries json tags because the tool payload uses snake_case keys
// (source_ref, implies_cardinality) that project.Claim's yaml-only tags would
// silently drop.
type extractOutput struct {
	Claims []draftClaim `json:"claims"`
}

type draftClaim struct {
	ID                 string `json:"id"`
	Text               string `json:"text"`
	SourceRef          string `json:"source_ref"`
	ImpliesCardinality int    `json:"implies_cardinality"`
}

// Extract calls the LLM on the joined doc text and returns draft claims.
// Prompt contract (prompt.go): only falsifiable capability claims; reject
// marketing adjectives; ≤ max (default 15); each with id (kebab-case), text,
// source_ref (file:line when identifiable), implies_cardinality (2+ when the
// claim text implies multiple instances/agents/devices). Invalid entries
// (non-kebab ids, empty texts, duplicate ids) are dropped, not fatal.
func Extract(ctx context.Context, drv *ai.Driver, docs map[string]string, max int) ([]project.Claim, error) {
	if drv == nil {
		return nil, errors.New("nil driver")
	}
	if max <= 0 {
		max = DefaultMaxClaims
	}
	res, err := drv.DecideWithTools(ctx, buildExtractPrompt(docs, max), []llm.Tool{claimsExtractTool()})
	if err != nil {
		return nil, fmt.Errorf("claims extract: %w", err)
	}
	if len(res.ToolCalls) == 0 {
		return nil, ErrNoClaims
	}
	raw, err := json.Marshal(res.ToolCalls[0].Input)
	if err != nil {
		return nil, fmt.Errorf("marshal tool args: %w", err)
	}
	var out extractOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse tool args: %w", err)
	}
	// Drop invalid entries per-claim instead of failing the whole draft: one
	// bad id from the model should not discard the rest. ValidateClaims on a
	// single-claim file reuses the ledger's own invariants (kebab id, text,
	// annotation shape).
	var claims []project.Claim
	seen := map[string]bool{}
	for _, d := range out.Claims {
		if d.ID == "" || seen[d.ID] {
			continue
		}
		c := project.Claim{
			ID:                 d.ID,
			Text:               d.Text,
			SourceRef:          d.SourceRef,
			ImpliesCardinality: d.ImpliesCardinality,
		}
		one := &project.ClaimsFile{Claims: []project.Claim{c}}
		if err := project.ValidateClaims(one); err != nil {
			continue
		}
		seen[c.ID] = true
		claims = append(claims, c)
	}
	if len(claims) > max {
		claims = claims[:max]
	}
	if len(claims) == 0 {
		return nil, ErrNoClaims
	}
	return claims, nil
}

// SurfaceTriage marks critical: true only for claims whose text matches a
// known surface token (service URL host, protocol role/message type, actor
// name, process command); others get critical:false + annotation
// "no surface mapping". Matching is case-insensitive substring on the claim
// text. The annotation set here lands only on NEW claims — MergeClaims (run
// after triage) preserves existing ids' Critical/StatusAnnotation.
func SurfaceTriage(draft []project.Claim, cfg *project.Config) []project.Claim {
	tokens := surfaceTokens(cfg)
	triaged := make([]project.Claim, len(draft))
	copy(triaged, draft)
	for i := range triaged {
		text := strings.ToLower(triaged[i].Text)
		mapped := false
		for _, tok := range tokens {
			if tok != "" && strings.Contains(text, tok) {
				mapped = true
				break
			}
		}
		if mapped {
			triaged[i].Critical = true
			triaged[i].StatusAnnotation = ""
		} else {
			triaged[i].Critical = false
			triaged[i].StatusAnnotation = AnnotationNoSurface
		}
	}
	return triaged
}

// surfaceTokens gathers every declared surface token from the config:
// service URL hosts, protocol role names and message types (handshake
// awaits, batch keys + item types, response types, HTTP-trigger effect
// types), actor names, and process command basenames. All lowercased —
// SurfaceTriage compares lowercased.
func surfaceTokens(cfg *project.Config) []string {
	if cfg == nil {
		return nil
	}
	var tokens []string
	add := func(s string) {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			tokens = append(tokens, s)
		}
	}
	for _, svc := range cfg.Services {
		if u, err := url.Parse(svc.URL); err == nil {
			add(u.Hostname())
		}
		if svc.Protocol == nil {
			continue
		}
		for name, r := range svc.Protocol.Roles {
			add(name)
			if r == nil {
				continue
			}
			if r.Handshake != nil {
				add(r.Handshake.AwaitType)
			}
			for _, reply := range r.Responses {
				add(reply)
			}
		}
		for key, b := range svc.Protocol.Batches {
			add(key)
			if b != nil {
				add(b.ItemType)
			}
		}
		for _, tr := range svc.Protocol.HTTPTriggers {
			if tr != nil {
				add(tr.Effect.MessageType)
			}
		}
	}
	for _, a := range cfg.Actors {
		add(a.Name)
		if a.Process == nil {
			continue
		}
		// Command basenames ("open-agents-bridge"), not full argv: a claim
		// mentions the binary, never the flags.
		for _, argv := range [][]string{a.Process.Setup, a.Process.Start} {
			if len(argv) > 0 {
				add(filepath.Base(argv[0]))
			}
		}
	}
	sort.Strings(tokens)
	return tokens
}

// MergeClaims appends new ids, preserves existing Critical and
// StatusAnnotation (the vocab re-extraction rule: manual channels survive),
// and drops ids absent from draft only when prune=true. Without prune the
// ledger is append-only. The existing Source block is carried over unchanged;
// callers refreshing it replace it after the merge.
func MergeClaims(existing *project.ClaimsFile, draft []project.Claim, prune bool) *project.ClaimsFile {
	merged := &project.ClaimsFile{}
	prev := map[string]project.Claim{}
	var prevOrder []string
	if existing != nil {
		merged.Source = existing.Source
		for _, c := range existing.Claims {
			if _, dup := prev[c.ID]; dup {
				continue
			}
			prev[c.ID] = c
			prevOrder = append(prevOrder, c.ID)
		}
	}
	inDraft := map[string]bool{}
	for _, c := range draft {
		if old, ok := prev[c.ID]; ok {
			// Refresh extracted fields, keep the manual channels.
			c.Critical = old.Critical
			c.StatusAnnotation = old.StatusAnnotation
		}
		merged.Claims = append(merged.Claims, c)
		inDraft[c.ID] = true
	}
	if !prune {
		for _, id := range prevOrder {
			if !inDraft[id] {
				merged.Claims = append(merged.Claims, prev[id])
			}
		}
	}
	return merged
}
