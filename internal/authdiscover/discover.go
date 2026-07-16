package authdiscover

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/project"
)

// ErrNoAuthFlow signals the model found no login flow in the target. It is
// distinct from a hard error: the command reports it and exits cleanly rather
// than treating a public API as a failure.
var ErrNoAuthFlow = errors.New("no login flow found")

// discoverOutput is the JSON shape the LLM must return. The Driver deserializes
// the response into this struct (ParseStructuredOutput tolerates markdown
// fences). Found/Notes are not part of AuthFlow.
type discoverOutput struct {
	Found bool `json:"found"`
	Login struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Body    map[string]string `json:"body"`
		Headers map[string]string `json:"headers"`
	} `json:"login"`
	TokenFrom string `json:"token_from"`
	InjectAs  string `json:"inject_as"`
	Notes     string `json:"notes"`
}

// Discover reads the target service's source, asks the LLM to infer a single
// login flow, and returns it (not written to disk). The driver is passed in so
// tests inject a mock and Discover never builds LLM clients. serviceURL is the
// base the model's login.path is relative to.
//
// On a parse/validation failure the returned error wraps the cause WITHOUT the
// raw LLM response (Driver.Decide embeds it; we hide it). On "no login flow" it
// returns ErrNoAuthFlow.
func Discover(ctx context.Context, driver *ai.Driver, cfg *project.Config, actorName, serviceURL string) (*project.AuthFlow, error) {
	if _, err := findActor(cfg, actorName); err != nil {
		return nil, err
	}

	files, err := selectSourceFiles(cfg.Code.Root)
	if err != nil {
		return nil, fmt.Errorf("select source files: %w", err)
	}

	prompt := buildDiscoverPrompt(serviceURL, files, credentialFieldNames(cfg, actorName))

	var out discoverOutput
	if err := driver.Decide(ctx, prompt, &out); err != nil {
		// Driver.Decide's error embeds the raw response; do not propagate it.
		return nil, errors.New("could not parse LLM output into AuthFlow")
	}

	if !out.Found {
		return nil, ErrNoAuthFlow
	}

	af := &project.AuthFlow{
		Login: project.AuthLogin{
			Method:  out.Login.Method,
			Path:    out.Login.Path,
			Body:    out.Login.Body,
			Headers: out.Login.Headers,
		},
		TokenFrom: out.TokenFrom,
		InjectAs:  out.InjectAs,
	}
	if vErr := project.ValidateAuthFlow(af); vErr != nil {
		return nil, fmt.Errorf("model produced an invalid auth flow: %w", vErr)
	}
	return af, nil
}

func findActor(cfg *project.Config, name string) (project.Actor, error) {
	if cfg == nil {
		return project.Actor{}, errors.New("config is nil")
	}
	for _, a := range cfg.Actors {
		if a.Name == name {
			return a, nil
		}
	}
	return project.Actor{}, fmt.Errorf("actor %q not found in config", name)
}

// credentialFieldNames returns the credential field names the actor has, so the
// prompt can ask for {email}/{password} placeholders. Values are never included.
func credentialFieldNames(cfg *project.Config, actorName string) []string {
	a, err := findActor(cfg, actorName)
	if err != nil {
		return nil
	}
	var names []string
	if a.Credentials.Email != "" {
		names = append(names, "email")
	}
	if a.Credentials.Password != "" {
		names = append(names, "password")
	}
	return names
}

// buildDiscoverPrompt assembles the prompt. It MUST inline the JSON shape
// because ai.Driver.Decide does not inject the schema into the prompt — it only
// parses the response.
func buildDiscoverPrompt(serviceURL string, files []SourceFile, credFields []string) string {
	var b strings.Builder
	b.WriteString("You are inferring the login/authentication flow for a web service.\n")
	b.WriteString("Read the source snippets below, locate the login endpoint, its request body shape, and the JSON path of the returned token.\n\n")
	fmt.Fprintf(&b, "The service URL is %q; login.path should be relative to it unless absolute.\n\n", serviceURL)
	b.WriteString("Respond with ONLY a JSON object of this exact shape:\n")
	b.WriteString("{\n")
	b.WriteString("  \"found\": <true if a login flow exists, false if the API is public>,\n")
	b.WriteString("  \"login\": {\n")
	b.WriteString("    \"method\": \"POST\",\n")
	b.WriteString("    \"path\": \"/login\",                 // relative to the service URL\n")
	b.WriteString("    \"body\": {\"field\": \"...\"},        // use placeholders for credentials\n")
	b.WriteString("    \"headers\": {}                      // optional static headers\n")
	b.WriteString("  },\n")
	b.WriteString("  \"token_from\": \"token\",             // dot-path into the JSON response, e.g. data.accessToken\n")
	b.WriteString("  \"inject_as\": \"Authorization: Bearer {token}\",\n")
	b.WriteString("  \"notes\": \"one-line rationale\"\n")
	b.WriteString("}\n\n")
	if len(credFields) > 0 {
		b.WriteString("Credential placeholders available (by field name) in login.body: ")
		for _, f := range credFields {
			fmt.Fprintf(&b, "{%s} ", f)
		}
		b.WriteString("— NEVER copy real credential values.\n\n")
	}
	b.WriteString("Source snippets:\n\n")
	for _, f := range files {
		b.WriteString("--- " + filepath.Base(f.Path) + " ---\n")
		b.WriteString(f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}
