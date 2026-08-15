package project

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Finding evidence tiers — same vocabulary as claims reconciliation: a
// failure observed while driving the real process is a stronger defect
// signal than a self-played one.
const (
	FindingTierReal     = "real"
	FindingTierEmulated = "emulated"
)

// Finding statuses. open: the defect is unaddressed; resolved: a later
// change addressed it (set by hand — the backflow only opens findings).
const (
	FindingOpen     = "open"
	FindingResolved = "resolved"
)

// Finding is one observed defect: a FAILED case from a completed session,
// recorded beside the claims ledger (findings are observations, claims are
// promises). Identity = case ref + error-signature, so the same failure
// across sessions bumps count/last_seen instead of piling up entries.
type Finding struct {
	ID string `yaml:"id"`
	// Summary is the failing step's error, first line (human-scannable).
	Summary string `yaml:"summary"`
	// CaseRef / SessionRef locate the failure; SessionRef is the LATEST
	// session that observed it.
	CaseRef    string   `yaml:"case_ref"`
	SessionRef string   `yaml:"session_ref"`
	ClaimRefs  []string `yaml:"claim_refs,omitempty"`
	// Tier is the evidence tier the failing case reached (real/emulated).
	Tier string `yaml:"tier"`
	// Status is open or resolved.
	Status string `yaml:"status"`
	// FirstSeen / LastSeen are RFC3339 timestamps (caller-supplied: the
	// session clock, not file-write time).
	FirstSeen string `yaml:"first_seen"`
	LastSeen  string `yaml:"last_seen"`
	// Count is how many sessions observed this signature (>=1).
	Count int `yaml:"count"`
}

// FindingsFile is the .cerberus/findings.yaml document.
type FindingsFile struct {
	Findings []Finding `yaml:"findings"`
}

// FindingInput is one failed-case observation fed to UpsertFinding.
type FindingInput struct {
	CaseRef      string
	ErrorSummary string
	SessionRef   string
	ClaimRefs    []string
	Tier         string
	// Now is the RFC3339 observation timestamp (caller-supplied).
	Now string
}

// findingID derives the stable identity: case ref + first 8 hex of the
// error-signature hash. The summary itself is NOT hashed (it is display
// text and may be truncated differently by callers).
func findingID(in FindingInput) string {
	sig := sha256.Sum256([]byte(in.ErrorSummary))
	return in.CaseRef + "-" + hex.EncodeToString(sig[:])[:8]
}

// UpsertFinding merges one observation into ff: a matching id bumps
// count/last_seen and re-points SessionRef; otherwise a new open finding is
// appended. Returns whether a new finding was created.
func UpsertFinding(ff *FindingsFile, in FindingInput) bool {
	id := findingID(in)
	for i := range ff.Findings {
		if ff.Findings[i].ID == id {
			f := &ff.Findings[i]
			f.Count++
			f.LastSeen = in.Now
			f.SessionRef = in.SessionRef
			return false
		}
	}
	ff.Findings = append(ff.Findings, Finding{
		ID: id, Summary: in.ErrorSummary,
		CaseRef: in.CaseRef, SessionRef: in.SessionRef,
		ClaimRefs: in.ClaimRefs, Tier: in.Tier,
		Status: FindingOpen,
		FirstSeen: in.Now, LastSeen: in.Now, Count: 1,
	})
	return true
}

// LoadFindings reads <projectDir>/.cerberus/findings.yaml. A missing file is
// (nil, nil) — findings are optional; an invalid file is an error.
func LoadFindings(projectDir string) (*FindingsFile, error) {
	raw, err := os.ReadFile(filepath.Join(projectDir, ".cerberus", "findings.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("findings: %w", err)
	}
	var ff FindingsFile
	if err := yaml.Unmarshal(raw, &ff); err != nil {
		return nil, fmt.Errorf("findings.yaml: %w", err)
	}
	if err := ValidateFindings(&ff); err != nil {
		return nil, fmt.Errorf("findings.yaml: %w", err)
	}
	return &ff, nil
}

// ValidateFindings checks the invariants: non-empty ids and summaries,
// unique ids, known tier and status.
func ValidateFindings(ff *FindingsFile) error {
	if ff == nil {
		return nil
	}
	seen := map[string]bool{}
	for i, f := range ff.Findings {
		if f.ID == "" {
			return fmt.Errorf("findings[%d]: id is required", i)
		}
		if seen[f.ID] {
			return fmt.Errorf("findings[%d]: duplicate finding id %q", i, f.ID)
		}
		seen[f.ID] = true
		if f.Summary == "" {
			return fmt.Errorf("findings[%d]: summary is required", i)
		}
		if f.Tier != FindingTierReal && f.Tier != FindingTierEmulated {
			return fmt.Errorf("findings[%d]: tier %q must be real or emulated", i, f.Tier)
		}
		if f.Status != FindingOpen && f.Status != FindingResolved {
			return fmt.Errorf("findings[%d]: status %q must be open or resolved", i, f.Status)
		}
	}
	return nil
}

// SaveFindings writes ff to <projectDir>/.cerberus/findings.yaml after
// validating (never persist an invalid document).
func SaveFindings(projectDir string, ff *FindingsFile) error {
	if err := ValidateFindings(ff); err != nil {
		return err
	}
	raw, err := yaml.Marshal(ff)
	if err != nil {
		return fmt.Errorf("findings.yaml: %w", err)
	}
	dir := filepath.Join(projectDir, ".cerberus")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("findings.yaml: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "findings.yaml"), raw, 0o644)
}
