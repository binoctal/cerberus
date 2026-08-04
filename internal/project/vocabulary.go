package project

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Vocabulary is the directed-edge routing vocabulary for a WS protocol. It is
// the single source of truth for the dynamic test generator and (future)
// Scout context. Type is an edge label, not a primary key. Struct tags carry
// BOTH yaml (on-disk file) and json (extractor subprocess stdout) so the same
// type decodes from either source.
type Vocabulary struct {
	Source VocabSource `yaml:"source" json:"source"`
	Edges  []VocabEdge `yaml:"edges" json:"edges"`
}

// VocabSource records where the vocabulary was extracted from.
type VocabSource struct {
	Files       []VocabFile `yaml:"files" json:"files"`
	ProtocolRef string      `yaml:"protocol_ref" json:"protocol_ref"`
}

// VocabFile is one source file the vocabulary was derived from.
type VocabFile struct {
	Path string `yaml:"path" json:"path"`
	Hash string `yaml:"hash" json:"hash"`
}

// VocabEdge is one directed message flow: a frame of Type leaves FromRole (or a
// DO-spontaneous null) bound for ToRole under Trigger. Guard is provenance only;
// the test generator executes off FromRole.
type VocabEdge struct {
	FromRole            string             `yaml:"from_role" json:"from_role"` // web | bridge | null
	ToRole              string             `yaml:"to_role" json:"to_role"`     // web | bridge
	Type                string             `yaml:"type" json:"type"`
	Trigger             string             `yaml:"trigger" json:"trigger"` // connect_web|connect_bridge|disconnect_bridge|message_handled|broadcast_endpoint
	Guard               string             `yaml:"guard,omitempty" json:"guard,omitempty"`
	Delivery            VocabDelivery      `yaml:"delivery" json:"delivery"`
	RouteField          string             `yaml:"route_field,omitempty" json:"route_field,omitempty"`
	OnMissingRoute      *VocabMissingRoute `yaml:"on_missing_route,omitempty" json:"on_missing_route,omitempty"`
	RequiresPresentRole string             `yaml:"requires_present_role,omitempty" json:"requires_present_role,omitempty"`
	SideEffects         []VocabSideEffect  `yaml:"side_effects,omitempty" json:"side_effects,omitempty"`
	Batch               *VocabBatch        `yaml:"batch,omitempty" json:"batch,omitempty"`
	Partial             bool               `yaml:"partial,omitempty" json:"partial,omitempty"`
	Unsupported         bool               `yaml:"unsupported,omitempty" json:"unsupported,omitempty"`
	Source              VocabEdgeSource    `yaml:"source" json:"source"`
}

// VocabDelivery declares how a frame is distributed.
type VocabDelivery struct {
	Mode          string `yaml:"mode" json:"mode"` // broadcast_web | send_bridge_by_device | unicast_web
	ExcludeSender bool   `yaml:"exclude_sender,omitempty" json:"exclude_sender,omitempty"`
}

// VocabMissingRoute declares the reaction when a route_field target is absent.
type VocabMissingRoute struct {
	Kind string `yaml:"kind" json:"kind"` // send_error
	Code string `yaml:"code" json:"code"`
}

// VocabSideEffect is an out-of-band action triggered by an edge.
type VocabSideEffect struct {
	Kind      string   `yaml:"kind" json:"kind"` // notify_orchestrator | stuck_recovery
	WhenTypes []string `yaml:"when_types,omitempty" json:"when_types,omitempty"`
}

// VocabBatch declares a deferred flush window for batched edges.
type VocabBatch struct {
	WindowMs int    `yaml:"window_ms" json:"window_ms"`
	Key      string `yaml:"key" json:"key"`
}

// VocabEdgeSource locates the emit point(s) in the source file.
type VocabEdgeSource struct {
	Spans []VocabSpan `yaml:"spans" json:"spans"`
}

// VocabSpan is a half-open source line range.
type VocabSpan struct {
	Start int `yaml:"start" json:"start"`
	End   int `yaml:"end" json:"end"`
}

// LoadVocabulary reads and parses a vocab.yaml file.
func LoadVocabulary(path string) (*Vocabulary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vocab: read %s: %w", path, err)
	}
	var v Vocabulary
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("vocab: parse %s: %w", path, err)
	}
	return &v, nil
}
