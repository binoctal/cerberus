package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// claimIDRE pins claim ids to kebab-case so they are stable identifiers for
// case binding across re-extractions.
var claimIDRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// wontTestRE matches the single gate-exemption annotation form. A reason is
// REQUIRED — an exemption without a documented why is not reviewable.
var wontTestRE = regexp.MustCompile(`^wont-test\(.+\)$`)

// Claim is one falsifiable product promise drawn from the SUT's own docs.
// The ledger is the reconciliation denominator: coverage measures our model,
// the ledger measures the product's own claim sheet.
type Claim struct {
	ID string `yaml:"id"`
	// Text is the falsifiable claim (extraction rejects marketing language).
	Text string `yaml:"text"`
	// SourceRef points back into the doc it was extracted from.
	SourceRef string `yaml:"source_ref,omitempty"`
	// Critical claims participate in the hard gate (exit 3 when unproven).
	// Extraction sets it true only when the claim maps to a discovered
	// surface; otherwise the claim lands as informational.
	Critical bool `yaml:"critical,omitempty"`
	// ImpliesCardinality records that the promise involves N>1 instances
	// (e.g. "multi-agent"). Recorded, not yet enforced.
	ImpliesCardinality int `yaml:"implies_cardinality,omitempty"`
	// StatusAnnotation is the manual channel. "wont-test(<reason>)" is the
	// ONLY gate exemption. PRESERVED on re-extraction (vocab merge rule).
	StatusAnnotation string `yaml:"status_annotation,omitempty"`
}

// WontTest reports whether the claim carries a valid gate exemption.
func (c Claim) WontTest() bool {
	return wontTestRE.MatchString(c.StatusAnnotation)
}

// ClaimsFile is the .cerberus/claims.yaml document.
type ClaimsFile struct {
	Source struct {
		Files []struct {
			Path string `yaml:"path"`
			Hash string `yaml:"hash,omitempty"`
		} `yaml:"files"`
	} `yaml:"source"`
	Claims []Claim `yaml:"claims"`
}

// LoadClaims reads <projectDir>/.cerberus/claims.yaml. A missing file is
// (nil, nil) — the ledger is optional per project; an invalid file is an
// error (a broken denominator must not pass silently).
func LoadClaims(projectDir string) (*ClaimsFile, error) {
	path := filepath.Join(projectDir, ".cerberus", "claims.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("claims: %w", err)
	}
	var cf ClaimsFile
	if err := yaml.Unmarshal(raw, &cf); err != nil {
		return nil, fmt.Errorf("claims.yaml: %w", err)
	}
	if err := ValidateClaims(&cf); err != nil {
		return nil, fmt.Errorf("claims.yaml: %w", err)
	}
	return &cf, nil
}

// ValidateClaims checks the ledger invariants: unique kebab-case ids,
// non-empty texts, and wont-test annotations carrying a reason.
func ValidateClaims(cf *ClaimsFile) error {
	if cf == nil {
		return nil
	}
	seen := map[string]bool{}
	for i, c := range cf.Claims {
		if !claimIDRE.MatchString(c.ID) {
			return fmt.Errorf("claims[%d]: id %q is not a valid claim id (kebab-case)", i, c.ID)
		}
		if seen[c.ID] {
			return fmt.Errorf("claims[%d]: duplicate claim id %q", i, c.ID)
		}
		seen[c.ID] = true
		if c.Text == "" {
			return fmt.Errorf("claims[%d]: text is required", i)
		}
		if a := c.StatusAnnotation; a == "wont-test" || (len(a) >= 8 && a[:8] == "wont-test" && !c.WontTest()) {
			return fmt.Errorf("claims[%d]: annotation %q needs a reason: wont-test(<reason>)", i, c.StatusAnnotation)
		}
	}
	return nil
}
