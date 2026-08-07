package report

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/session"
)

// TestRenderContractSection_UnmeasuredAssessment asserts that a session whose
// Assessment has Measured=false (e.g. a SaaS/WS session with no local SUT) is
// rendered as N/A rather than the hallucinated "Not Reached / 0.0%".
func TestRenderContractSection_UnmeasuredAssessment(t *testing.T) {
	data := &ReportData{
		Summary: &session.SessionSummary{
			Contract: &contract.Contract{
				Depth: "line",
				CoverageGate: contract.Gate{
					Module:          "example.com/mod",
					LineThreshold:   0.8,
					BranchThreshold: 0.6,
				},
			},
			Assessment: &contract.Assessment{
				Measured: false,
			},
		},
	}

	var b strings.Builder
	renderContractSection(&b, data)
	md := b.String()

	assert.Contains(t, md, "⚪ N/A", "unmeasured assessment must surface N/A status")
	assert.NotContains(t, md, "Not Reached", "unmeasured assessment must not hallucinate Not Reached")
	assert.NotContains(t, md, "0.0%", "unmeasured assessment must not render a meaningless numeric coverage")
	assert.Contains(t, md, "**Coverage**: N/A", "unmeasured assessment should explain why coverage is N/A")
}

// TestRenderContractSection_MeasuredAssessment is the regression guard: when
// Measured=true the existing rendering (status from Reached, Coverage from
// CoveragePct) is unchanged.
func TestRenderContractSection_MeasuredAssessment(t *testing.T) {
	t.Run("reached", func(t *testing.T) {
		data := &ReportData{
			Summary: &session.SessionSummary{
				Contract: &contract.Contract{
					Depth: "line",
					CoverageGate: contract.Gate{
						Module:          "example.com/mod",
						LineThreshold:   0.8,
						BranchThreshold: 0.6,
					},
				},
				Assessment: &contract.Assessment{
					Measured:    true,
					Reached:     true,
					CoveragePct: 0.875,
				},
			},
		}

		var b strings.Builder
		renderContractSection(&b, data)
		md := b.String()

		assert.Contains(t, md, "✅ Reached")
		assert.Contains(t, md, "**Coverage**: 87.5%")
		assert.NotContains(t, md, "⚪ N/A")
	})

	t.Run("not reached", func(t *testing.T) {
		data := &ReportData{
			Summary: &session.SessionSummary{
				Contract: &contract.Contract{
					Depth: "line",
					CoverageGate: contract.Gate{
						Module:          "example.com/mod",
						LineThreshold:   0.8,
						BranchThreshold: 0.6,
					},
				},
				Assessment: &contract.Assessment{
					Measured:    true,
					Reached:     false,
					CoveragePct: 0.40,
				},
			},
		}

		var b strings.Builder
		renderContractSection(&b, data)
		md := b.String()

		assert.Contains(t, md, "❌ Not Reached")
		assert.Contains(t, md, "**Coverage**: 40.0%")
		assert.NotContains(t, md, "⚪ N/A")
	})
}
