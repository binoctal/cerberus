package protocoldiscover

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
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

// Infer asks the LLM to draft a protocol description from the given inputs,
// validates it, and returns it (not written to disk). The driver is passed in
// so tests inject a mock and Infer never builds LLM clients. serviceName names
// the service the draft targets; it is used only to label the prompt (the actor
// list for credential_ref validation comes from cfg).
//
// On a parse failure the returned error is a static message that does NOT
// include the raw LLM response (Driver.Decide embeds it; we hide it). On a
// validation failure the cause is wrapped. When the model reports no protocol
// is described it returns ErrNoProtocol.
func Infer(ctx context.Context, driver *ai.Driver, cfg *project.Config, serviceName string, inputs []SourceFile) (*project.Protocol, error) {
	if driver == nil {
		return nil, errors.New("nil driver")
	}

	prompt := buildInferPrompt(serviceName, inputs)

	var out inferOutput
	if err := driver.Decide(ctx, prompt, &out); err != nil {
		// Driver.Decide's error embeds the raw response; do not propagate it.
		return nil, errors.New("could not parse LLM output into Protocol")
	}

	if !out.Found {
		return nil, ErrNoProtocol
	}

	p := &project.Protocol{
		Framing:  out.Framing,
		TypePath: out.TypePath,
	}
	if out.Auth != nil {
		p.Auth = &project.ProtocolAuth{
			Strategy:      out.Auth.Strategy,
			Param:         out.Auth.Param,
			CredentialRef: out.Auth.CredentialRef,
		}
	}
	// Initialize p.Roles before assigning into it; project.Protocol's Roles
	// map is nil on construction and the loop below writes by key.
	if len(out.Roles) > 0 {
		p.Roles = map[string]*project.ProtocolRole{}
	}
	for name, r := range out.Roles {
		role := &project.ProtocolRole{
			CredentialRef: r.CredentialRef,
			Params:        r.Params,
			Headers:       r.Headers,
			Subprotocols:  r.Subprotocols,
		}
		if r.Handshake != nil {
			role.Handshake = &project.RoleHandshake{
				AwaitType: r.Handshake.AwaitType,
				Timeout:   r.Handshake.Timeout,
			}
		}
		p.Roles[name] = role
	}

	if err := project.ValidateProtocol(p, actorsOf(cfg)); err != nil {
		return nil, fmt.Errorf("model produced an invalid protocol: %w", err)
	}
	return p, nil
}

// actorsOf returns the config's actor list, used to confirm credential_ref
// names a real actor. Nil cfg yields nil (validation then rejects any ref).
func actorsOf(cfg *project.Config) []project.Actor {
	if cfg == nil {
		return nil
	}
	return cfg.Actors
}

// buildInferPrompt assembles the prompt (PROVISIONAL content — tune via
// dogfooding). It MUST inline the JSON shape because ai.Driver.Decide does not
// inject the schema into the prompt — it only parses the response. It never
// includes credential values; credential_ref names an actor, it is not a token.
func buildInferPrompt(serviceName string, inputs []SourceFile) string {
	var b strings.Builder
	b.WriteString("You are drafting a WebSocket protocol description for a cerberus test config.\n")
	b.WriteString("From the docs/message examples below, infer: the wire framing, the routing-key path, how auth is attached, and any connection roles.\n")
	fmt.Fprintf(&b, "The target service is %q.\n\n", serviceName)
	b.WriteString("Respond with ONLY a JSON object of this shape:\n")
	b.WriteString("{\n")
	b.WriteString("  \"found\": <true if a WS protocol is described, false otherwise>,\n")
	b.WriteString("  \"framing\": \"json\",                      // json | text | binary\n")
	b.WriteString("  \"type_path\": \"type\",                    // dotted path to the routing key\n")
	b.WriteString("  \"auth\": {\"strategy\":\"query\",\"param\":\"token\",\"credential_ref\":\"<actor>\"},\n")
	b.WriteString("  \"roles\": {\"web\": {\"credential_ref\":\"<actor>\",\"params\":{\"type\":\"web\"},\"handshake\":{\"await_type\":\"ready\",\"timeout\":5}}},\n")
	b.WriteString("  \"notes\": \"one-line rationale\"\n")
	b.WriteString("}\n\n")
	b.WriteString("credential_ref MUST name an actor that exists in project.yaml — NEVER invent one, and NEVER include real credential values or tokens.\n\n")
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
