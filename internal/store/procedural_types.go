package store

// ProceduralMemory represents an L3 learned strategy.
type ProceduralMemory struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Condition      string    `json:"condition"`
	Action         string    `json:"action"`
	Effectiveness  float64   `json:"effectiveness"`
	UsageCount     int       `json:"usage_count"`
	ProjectName    string    `json:"project_name,omitempty"`
	Category       string    `json:"category"`
	Type           string    `json:"type"` // "failure" or "success"
	Archived       bool      `json:"archived"`
	CreatedAt      string    `json:"created_at"`
	Embedding      []float64 `json:"embedding,omitempty"`
	EmbeddingModel string    `json:"embedding_model,omitempty"`
}
