package project

// ProjectModel is the cognitive model built during exploration (§3.2).
// Stored in project_models table and .cerberus/project-model.yaml.
type ProjectModel struct {
	Navigation     NavigationModel `yaml:"navigation"`
	API            APIModel        `yaml:"api"`
	SchemaAnalyzed bool            `yaml:"schema_analyzed"`
	TechStack      []string        `yaml:"tech_stack,omitempty"`
	InvariantHints []InvariantHint `yaml:"invariants_hints,omitempty"`
}

type NavigationModel struct {
	Pages []PageDef `yaml:"pages"`
}

type PageDef struct {
	Path         string  `yaml:"path"`
	Type         string  `yaml:"type,omitempty"`
	RequiresAuth bool    `yaml:"requires_auth,omitempty"`
	Confidence   float64 `yaml:"confidence"`
}

type APIModel struct {
	Endpoints []EndpointDef `yaml:"endpoints"`
}

type EndpointDef struct {
	Method     string  `yaml:"method"`
	Path       string  `yaml:"path"`
	Confidence float64 `yaml:"confidence"`
}

type InvariantHint struct {
	ID          string  `yaml:"id"`
	Source      string  `yaml:"source"`
	Description string  `yaml:"description"`
	Confidence  float64 `yaml:"confidence"`
	Severity    string  `yaml:"severity,omitempty"`
}

// InfoScore computes project knowledge score (0.0 - 1.0) based on
// absolute information quality, NOT unknown totals.
//
// The old MaturityScore used count/total ratios, but "total pages" and
// "total endpoints" are unknowable at cognition time — discovering them
// IS the cognition step. This version uses known quantities only:
//
//	known_info_score = weighted_sum(
//	    known_endpoints × avg_confidence,   # 40% — most valuable for API testing
//	    known_pages × avg_confidence,       # 30% — valuable for UI testing
//	    schema_analyzed ? 1.0 : 0.0,        # 20% — binary signal
//	    has_historical_model ? 1.0 : 0.0,   # 10% — reuse signal
//	) / max_possible_score
//
// max_possible_score uses soft saturation (e.g. 20 endpoints = full score)
// so the result stays bounded regardless of project size.
func (pm *ProjectModel) InfoScore(hasHistoricalModel bool) float64 {
	if pm == nil {
		return 0
	}

	// Endpoint score: capped at 20 endpoints for normalization
	endpointScore := cappedConfidenceScore(len(pm.API.Endpoints), 20, avgConfidenceEndpoints(pm.API.Endpoints))

	// Page score: capped at 30 pages for normalization
	pageScore := cappedConfidenceScore(len(pm.Navigation.Pages), 30, avgConfidencePages(pm.Navigation.Pages))

	// Schema analysis: binary
	schemaScore := 0.0
	if pm.SchemaAnalyzed {
		schemaScore = 1.0
	}

	// Historical model: binary
	historyScore := 0.0
	if hasHistoricalModel {
		historyScore = 1.0
	}

	return endpointScore*0.4 + pageScore*0.3 + schemaScore*0.2 + historyScore*0.1
}

// cappedConfidenceScore computes min(count, cap)/cap × avgConfidence.
// Uses soft saturation so large projects don't exceed 1.0.
func cappedConfidenceScore(count, cap int, avgConfidence float64) float64 {
	if count == 0 || cap <= 0 {
		return 0
	}
	capped := min(count, cap)
	return (float64(capped) / float64(cap)) * avgConfidence
}

func avgConfidenceEndpoints(items []EndpointDef) float64 {
	if len(items) == 0 {
		return 0
	}
	sum := 0.0
	for _, e := range items {
		sum += e.Confidence
	}
	return sum / float64(len(items))
}

func avgConfidencePages(items []PageDef) float64 {
	if len(items) == 0 {
		return 0
	}
	sum := 0.0
	for _, p := range items {
		sum += p.Confidence
	}
	return sum / float64(len(items))
}

// MaturityScore returns the old-style score for backward compatibility.
// Deprecated: use InfoScore instead.
func (pm *ProjectModel) MaturityScore() float64 {
	return pm.InfoScore(false)
}
