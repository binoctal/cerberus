package project

type ProjectModel struct {
	Navigation     NavigationModel `yaml:"navigation"`
	API            APIModel        `yaml:"api"`
	SchemaAnalyzed bool            `yaml:"schema_analyzed"`
	TechStack      []string        `yaml:"tech_stack,omitempty"`
	InvariantHints []InvariantHint `yaml:"invariants_hints,omitempty"`
}

type NavigationModel struct {
	Pages      []PageDef `yaml:"pages"`
	TotalPages int       `yaml:"total_pages"`
}

type PageDef struct {
	Path         string  `yaml:"path"`
	Type         string  `yaml:"type,omitempty"`
	RequiresAuth bool    `yaml:"requires_auth,omitempty"`
	Confidence   float64 `yaml:"confidence"`
}

type APIModel struct {
	Endpoints      []EndpointDef `yaml:"endpoints"`
	TotalEndpoints int           `yaml:"total_endpoints"`
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

func (pm *ProjectModel) MaturityScore() float64 {
	if pm == nil {
		return 0
	}
	pageScore := 0.0
	if pm.Navigation.TotalPages > 0 {
		pageScore = float64(len(pm.Navigation.Pages)) / float64(pm.Navigation.TotalPages)
	}

	apiScore := 0.0
	if pm.API.TotalEndpoints > 0 {
		apiScore = float64(len(pm.API.Endpoints)) / float64(pm.API.TotalEndpoints)
	}

	schemaScore := 0.0
	if pm.SchemaAnalyzed {
		schemaScore = 1.0
	}

	return pageScore*0.3 + apiScore*0.4 + schemaScore*0.3
}
