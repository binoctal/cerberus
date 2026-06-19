package contract

// Depth tiers.
const (
	DepthSmoke    = "smoke"
	DepthStandard = "standard"
	DepthThorough = "thorough"
)

// Dimensions is the per-tier expansion of what the contract should cover.
type Dimensions struct {
	PathTypes   []string
	ErrorScope  []string
	Boundaries  []string
	Concurrency bool
}

// ExpandDepth maps a tier to its dimension expansion.
func ExpandDepth(depth string) Dimensions {
	switch depth {
	case DepthSmoke:
		return Dimensions{
			PathTypes:  []string{"happy"},
			ErrorScope: []string{"none"},
		}
	case DepthThorough:
		return Dimensions{
			PathTypes:   []string{"happy", "alternative", "boundary", "edge"},
			ErrorScope:  []string{"4xx", "validation", "exception"},
			Boundaries:  []string{"empty", "zero", "max", "invalid", "extreme"},
			Concurrency: true,
		}
	default: // standard
		// Standard PathTypes excludes "boundary" per the spec depth table (boundary is thorough-only).
		// Standard covers boundaries via the separate Boundaries dimension (empty/zero/max/invalid).
		return Dimensions{
			PathTypes:  []string{"happy", "alternative"},
			ErrorScope: []string{"4xx", "validation"},
			Boundaries: []string{"empty", "zero", "max", "invalid"},
		}
	}
}
