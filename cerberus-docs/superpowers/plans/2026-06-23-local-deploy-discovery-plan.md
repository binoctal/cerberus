# Local Deploy Discovery Implementation Plan (Block ②)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Add `cerberus discover`, which parses `docker-compose.yml` into `project.yaml` services (name/url/health), plus a run-time hint nudging users to run it.

**Architecture:** A new pure-logic `internal/discover/` package (parse → filter infra → map ports/health → merge into config → report gaps). `cmd/cerberus/main_discover.go` wires it into a `cerberus discover` cobra command; `cmd/cerberus/main_run.go` prints a hint when services are empty but a compose file is present.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`, `github.com/spf13/cobra`, `github.com/stretchr/testify`.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, no CGo.
- Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Comments and commit messages in English.
- All docs under `cerberus-docs/`.
- Block ① already merged: `project.Service` has `PathPrefix`, `Actor` has `Service`.

## File Structure

- Create `internal/discover/compose.go` — `ParseCompose` + compose YAML structs
- Create `internal/discover/filter.go` — `FilterServices` (infra blacklist)
- Create `internal/discover/map.go` — `ToProjectServices` (port→url, healthcheck→health)
- Create `internal/discover/merge.go` — `MergeIntoConfig` (dedup by name, preserve overrides)
- Create `internal/discover/gaps.go` — `Gaps` + `FormatGaps`
- Create `internal/discover/run_hint.go` — `ShouldHintDiscover` + `HintMessage`
- Create `cmd/cerberus/main_discover.go` — `discoverCmd()` cobra command
- Modify `cmd/cerberus/main.go:15` — register `discoverCmd()`
- Modify `cmd/cerberus/main_run.go` — print hint at RunE start

---

### Task 1: Parse docker-compose.yml

**Files:**
- Create: `internal/discover/compose.go`
- Test: `internal/discover/compose_test.go`

**Interfaces:**
- Produces: `type ComposeFile struct { Services map[string]ComposeService }`; `type ComposeService struct { Image string; Ports []string; Healthcheck ComposeHealthcheck }`; `type ComposeHealthcheck struct { Test []string }`; `func ParseCompose(data []byte) (*ComposeFile, error)`.

- [x] **Step 1: Write the failing test**

```go
package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCompose_ExtractsServices(t *testing.T) {
	src := []byte(`
services:
  gateway:
    image: relay-gateway:dev
    ports:
      - "8081:8080"
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/health"]
  postgres:
    image: postgres:16
    ports:
      - "5432:5432"
`)
	f, err := ParseCompose(src)
	require.NoError(t, err)
	require.Len(t, f.Services, 2)
	assert.Equal(t, "relay-gateway:dev", f.Services["gateway"].Image)
	assert.Equal(t, []string{"8081:8080"}, f.Services["gateway"].Ports)
	assert.Equal(t, []string{"CMD", "wget", "--spider", "-q", "http://localhost:8080/health"}, f.Services["gateway"].Healthcheck.Test)
}
```

- [x] **Step 2: Run, verify FAIL**

Run: `go test ./internal/discover/ -run TestParseCompose -v`
Expected: FAIL — package/types undefined.

- [x] **Step 3: Implement**

```go
package discover

import "gopkg.in/yaml.v3"

// ComposeFile is the subset of docker-compose.yml we read.
type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
}

// ComposeService is a single entry under services:.
type ComposeService struct {
	Image       string           `yaml:"image"`
	Ports       []string         `yaml:"ports"`
	Healthcheck ComposeHealthcheck `yaml:"healthcheck"`
}

// ComposeHealthcheck mirrors the healthcheck block.
type ComposeHealthcheck struct {
	Test []string `yaml:"test"`
}

// ParseCompose decodes a docker-compose.yml document.
func ParseCompose(data []byte) (*ComposeFile, error) {
	var f ComposeFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}
```

- [x] **Step 4: Run, verify PASS**

Run: `go test ./internal/discover/ -run TestParseCompose -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/discover/compose.go internal/discover/compose_test.go
git commit -m "feat(discover): parse docker-compose.yml services"
```

---

### Task 2: Filter infra services

**Files:**
- Create: `internal/discover/filter.go`
- Test: `internal/discover/filter_test.go`

**Interfaces:**
- Consumes: `ComposeService`.
- Produces: `func FilterServices(services map[string]ComposeService, include, exclude []string) []NamedComposeService` where `type NamedComposeService struct { Name string; Service ComposeService }`.

- [x] **Step 1: Write the failing test**

```go
package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterServices_DropsInfraAndPortless(t *testing.T) {
	services := map[string]ComposeService{
		"gateway":   {Image: "relay-gateway:dev", Ports: []string{"8081:8080"}},
		"postgres":  {Image: "postgres:16", Ports: []string{"5432:5432"}},
		"redis":     {Image: "redis:7", Ports: []string{"6379:6379"}},
		"edgeless":  {Image: "relay-edge:dev"}, // no ports → dropped
	}
	got := FilterServices(services, nil, nil)
	names := namesOf(got)
	assert.Contains(t, names, "gateway")
	assert.NotContains(t, names, "postgres")
	assert.NotContains(t, names, "redis")
	assert.NotContains(t, names, "edgeless")
}

func TestFilterServices_IncludeExcludeOverride(t *testing.T) {
	services := map[string]ComposeService{
		"gateway":  {Image: "relay-gateway:dev", Ports: []string{"8081:8080"}},
		"postgres": {Image: "postgres:16", Ports: []string{"5432:5432"}},
	}
	// include forces postgres back in; exclude drops gateway
	got := FilterServices(services, []string{"postgres"}, []string{"gateway"})
	names := namesOf(got)
	assert.Contains(t, names, "postgres")
	assert.NotContains(t, names, "gateway")
}

func namesOf(s []NamedComposeService) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = v.Name
	}
	return out
}
```

- [x] **Step 2: Run, verify FAIL**

Run: `go test ./internal/discover/ -run TestFilterServices -v`
Expected: FAIL.

- [x] **Step 3: Implement**

```go
package discover

import "strings"

// NamedComposeService pairs a service name with its definition.
type NamedComposeService struct {
	Name    string
	Service ComposeService
}

// infraImageSubstrings identifies infra images by substring. Conservative:
// a few well-known names; users override with --include for anything missed.
var infraImageSubstrings = []string{
	"postgres", "pgvector", "mysql", "mariadb", "redis", "memcached",
	"mongo", "kafka", "zookeeper", "rabbitmq", "nginx", "traefik",
	"minio", "elastic", "consul", "etcd",
}

func isInfraImage(image string) bool {
	low := strings.ToLower(image)
	for _, s := range infraImageSubstrings {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// FilterServices drops infra images and portless services, then applies
// explicit --include (force keep) and --exclude (force drop) overrides.
func FilterServices(services map[string]ComposeService, include, exclude []string) []NamedComposeService {
	var out []NamedComposeService
	for name, svc := range services {
		if contains(exclude, name) {
			continue
		}
		if contains(include, name) {
			out = append(out, NamedComposeService{Name: name, Service: svc})
			continue
		}
		if len(svc.Ports) == 0 {
			continue
		}
		if isInfraImage(svc.Image) {
			continue
		}
		out = append(out, NamedComposeService{Name: name, Service: svc})
	}
	return out
}
```

- [x] **Step 4: Run, verify PASS**

Run: `go test ./internal/discover/ -run TestFilterServices -v`
Expected: PASS (all three subtests).

- [x] **Step 5: Commit**

```bash
git add internal/discover/filter.go internal/discover/filter_test.go
git commit -m "feat(discover): filter infra and portless services"
```

---

### Task 3: Map compose service → project.Service (port + health)

**Files:**
- Create: `internal/discover/map.go`
- Test: `internal/discover/map_test.go`

**Interfaces:**
- Consumes: `NamedComposeService`.
- Produces: `func ToProjectServices(in []NamedComposeService) []project.Service`.

- [x] **Step 1: Write the failing test**

```go
package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/project"
)

func TestToProjectServices_PortAndHealth(t *testing.T) {
	in := []NamedComposeService{
		{Name: "gateway", Service: ComposeService{
			Ports: []string{"8081:8080"},
			Healthcheck: ComposeHealthcheck{Test: []string{"CMD", "wget", "--spider", "-q", "http://localhost:8080/health"}},
		}},
		{Name: "router", Service: ComposeService{
			Ports: []string{"8085"}, // short form → host 8085
		}},
	}
	got := ToProjectServices(in)
	assert.Equal(t, []project.Service{
		{Name: "gateway", URL: "http://localhost:8081", Health: "/health"},
		{Name: "router", URL: "http://localhost:8085", Health: ""},
	}, got)
}

func TestHostPort(t *testing.T) {
	cases := map[string]string{
		"8081:8080":      "8081",
		"127.0.0.1:8081:8080": "8081",
		"8085":           "8085",
		"":               "",
	}
	for in, want := range cases {
		assert.Equal(t, want, hostPort(in), "input %q", in)
	}
}
```

- [x] **Step 2: Run, verify FAIL**

Run: `go test ./internal/discover/ -run "TestToProjectServices|TestHostPort" -v`
Expected: FAIL.

- [x] **Step 3: Implement**

```go
package discover

import (
	"strings"

	"github.com/binoctal/cerberus/internal/project"
)

// hostPort extracts the host-side port from a docker-compose ports entry.
// "8081:8080" → "8081"; "127.0.0.1:8081:8080" → "8081"; "8085" → "8085".
func hostPort(entry string) string {
	if entry == "" {
		return ""
	}
	parts := strings.Split(entry, ":")
	switch len(parts) {
	case 1:
		return parts[0]            // "8085"
	case 2:
		return parts[0]            // "8081:8080"
	default:
		return parts[len(parts)-2] // "127.0.0.1:8081:8080"
	}
}

// healthPath extracts the path from a healthcheck test list, finding the
// first http(s) URL and returning its path; "" if none.
func healthPath(test []string) string {
	for _, tok := range test {
		for _, scheme := range []string{"http://", "https://"} {
			if i := strings.Index(tok, scheme); i >= 0 {
				u := tok[i+len(scheme):]
				// strip host[:port]
				if slash := strings.Index(u, "/"); slash >= 0 {
					return u[slash:]
				}
			}
		}
	}
	return ""
}

// ToProjectServices maps filtered compose services to cerberus project.Service
// values with localhost URLs and (best-effort) health paths.
func ToProjectServices(in []NamedComposeService) []project.Service {
	out := make([]project.Service, 0, len(in))
	for _, n := range in {
		port := ""
		if len(n.Service.Ports) > 0 {
			port = hostPort(n.Service.Ports[0])
		}
		out = append(out, project.Service{
			Name:   n.Name,
			URL:    "http://localhost:" + port,
			Health: healthPath(n.Service.Healthcheck.Test),
		})
	}
	return out
}
```

(Delete the first incorrect `hostPort` draft; keep only the explicit one.)

- [x] **Step 4: Run, verify PASS**

Run: `go test ./internal/discover/ -run "TestToProjectServices|TestHostPort" -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/discover/map.go internal/discover/map_test.go
git commit -m "feat(discover): map ports and healthcheck to project.Service"
```

---

### Task 4: Merge discovered services into project config (dedup, preserve overrides)

**Files:**
- Create: `internal/discover/merge.go`
- Test: `internal/discover/merge_test.go`

**Interfaces:**
- Consumes: `*project.Config`, `[]project.Service`.
- Produces: `func MergeIntoConfig(cfg *project.Config, discovered []project.Service) (added []string)` — appends services whose Name is absent from cfg.Services (existing services keep their hand-written overrides); returns the names appended.

- [x] **Step 1: Write the failing test**

```go
package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/project"
)

func TestMergeIntoConfig_PreservesExistingAppendsNew(t *testing.T) {
	cfg := &project.Config{}
	cfg.Services = []project.Service{
		{Name: "gateway", URL: "http://localhost:8081", Headers: map[string]string{"Host": "api.modelsite.ai"}}, // hand-written override
	}
	discovered := []project.Service{
		{Name: "gateway", URL: "http://localhost:8081", Health: "/health"}, // must NOT overwrite
		{Name: "admin", URL: "http://localhost:8086", Health: "/health"},  // appended
	}
	added := MergeIntoConfig(cfg, discovered)
	assert.Equal(t, []string{"admin"}, added)
	assert.Len(t, cfg.Services, 2)
	// gateway kept its Host override, Health not filled (override preserved)
	assert.Equal(t, "api.modelsite.ai", cfg.Services[0].Headers["Host"])
	assert.Equal(t, "", cfg.Services[0].Health)
	assert.Equal(t, "/health", cfg.Services[1].Health)
}
```

- [x] **Step 2: Run, verify FAIL**

Run: `go test ./internal/discover/ -run TestMergeIntoConfig -v`
Expected: FAIL.

- [x] **Step 3: Implement**

```go
package discover

import "github.com/binoctal/cerberus/internal/project"

// MergeIntoConfig appends discovered services whose Name is not already
// present in cfg.Services. Existing entries are left untouched so hand-written
// overrides (domain, key-bearing headers, path_prefix) are preserved.
// Returns the names that were appended.
func MergeIntoConfig(cfg *project.Config, discovered []project.Service) []string {
	existing := make(map[string]bool, len(cfg.Services))
	for _, s := range cfg.Services {
		existing[s.Name] = true
	}
	var added []string
	for _, s := range discovered {
		if existing[s.Name] {
			continue
		}
		cfg.Services = append(cfg.Services, s)
		existing[s.Name] = true
		added = append(added, s.Name)
	}
	return added
}
```

- [x] **Step 4: Run, verify PASS**

Run: `go test ./internal/discover/ -run TestMergeIntoConfig -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/discover/merge.go internal/discover/merge_test.go
git commit -m "feat(discover): merge services into config preserving overrides"
```

---

### Task 5: Report gaps (what still needs manual filling)

**Files:**
- Create: `internal/discover/gaps.go`
- Test: `internal/discover/gaps_test.go`

**Interfaces:**
- Produces: `type ServiceGaps struct { Name string; MissingHost bool; MissingPathPrefix bool }`; `func Gaps(services []project.Service) []ServiceGaps`; `func FormatGaps(gaps []ServiceGaps, hasActorKey bool) string`.

- [x] **Step 1: Write the failing test**

```go
package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/project"
)

func TestGaps_FlagMissingHostAndPrefix(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://localhost:8081"},                              // both missing
		{Name: "admin", URL: "http://localhost:8086", Headers: map[string]string{"Host": "x"}, PathPrefix: []string{"/api/admin"}},
	}
	gaps := Gaps(services)
	assert.Len(t, gaps, 1)
	assert.Equal(t, "gateway", gaps[0].Name)
	assert.True(t, gaps[0].MissingHost)
	assert.True(t, gaps[0].MissingPathPrefix)
}

func TestFormatGaps_MentionsActorKey(t *testing.T) {
	out := FormatGaps(nil, false)
	assert.Contains(t, out, "actor")
	assert.Contains(t, out, "key")
}
```

- [x] **Step 2: Run, verify FAIL**

Run: `go test ./internal/discover/ -run "TestGaps|TestFormatGaps" -v`
Expected: FAIL.

- [x] **Step 3: Implement**

```go
package discover

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/project"
)

// ServiceGaps records which required-by-test fields a discovered service lacks.
type ServiceGaps struct {
	Name             string
	MissingHost      bool // domain routing (Host header)
	MissingPathPrefix bool // for block ① service attribution
}

// Gaps inspects services and returns one entry per service missing a field.
func Gaps(services []project.Service) []ServiceGaps {
	var out []ServiceGaps
	for _, s := range services {
		g := ServiceGaps{Name: s.Name}
		if s.Headers["Host"] == "" {
			g.MissingHost = true
		}
		if len(s.PathPrefix) == 0 {
			g.MissingPathPrefix = true
		}
		if g.MissingHost || g.MissingPathPrefix {
			out = append(out, g)
		}
	}
	return out
}

// FormatGaps renders a human-readable gap report. hasActorKey indicates
// whether at least one actor carries credentials (auth key); when false the
// report reminds the user to add an actor key.
func FormatGaps(gaps []ServiceGaps, hasActorKey bool) string {
	var b strings.Builder
	if len(gaps) > 0 {
		b.WriteString("gaps (fill manually in project.yaml):\n")
		for _, g := range gaps {
			var missing []string
			if g.MissingHost {
				missing = append(missing, "domain (Host header)")
			}
			if g.MissingPathPrefix {
				missing = append(missing, "path_prefix")
			}
			fmt.Fprintf(&b, "  %s: needs %s\n", g.Name, strings.Join(missing, ", "))
		}
	}
	if !hasActorKey {
		b.WriteString("gaps: add at least one actor with a credentials key (Bearer) for authenticated paths\n")
	}
	return b.String()
}
```

- [x] **Step 4: Run, verify PASS**

Run: `go test ./internal/discover/ -run "TestGaps|TestFormatGaps" -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/discover/gaps.go internal/discover/gaps_test.go
git commit -m "feat(discover): report domain/path_prefix/key gaps"
```

---

### Task 6: `cerberus discover` CLI command

**Files:**
- Create: `cmd/cerberus/main_discover.go`
- Modify: `cmd/cerberus/main.go:15` (add `discoverCmd()` to the `rootCmd.AddCommand(...)` list)
- Test: `cmd/cerberus/main_discover_test.go`

**Interfaces:**
- Consumes: `discover.ParseCompose`, `FilterServices`, `ToProjectServices`, `MergeIntoConfig`, `Gaps`, `FormatGaps`; `project.LoadFromFile`/`LoadFromYAML`; `os.ReadFile`/`os.WriteFile`.

- [x] **Step 1: Write the failing test (golden: compose → merged project.yaml content)**

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/project"
)

func TestDiscoverCmd_WritesMergedProjectYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(`
services:
  gateway:
    image: relay-gateway:dev
    ports: ["8081:8080"]
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/health"]
  postgres:
    image: postgres:16
    ports: ["5432:5432"]
`), 0644))

	err := runDiscover(dir, "docker-compose.yml", nil, nil, false)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".cerberus", "project.yaml"))
	require.NoError(t, err)
	var cfg project.Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	names := []string{cfg.Services[0].Name, cfg.Services[1].Name}
	assert.Contains(t, names, "gateway")
	assert.NotContains(t, names, "postgres") // infra filtered
}
```

(The test calls a package-level `runDiscover(workDir, compose string, include, exclude []string, dryRun bool) error` — factor the command's RunE body into this testable function.)

- [x] **Step 2: Run, verify FAIL**

Run: `go test ./cmd/cerberus/ -run TestDiscoverCmd -v`
Expected: FAIL — `runDiscover` undefined.

- [x] **Step 3: Implement `main_discover.go`**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/discover"
	"github.com/binoctal/cerberus/internal/project"
)

var (
	discoverComposePath string
	discoverDryRun      bool
	discoverInclude     []string
	discoverExclude     []string
)

func discoverCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover services from docker-compose.yml into project.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiscover(".", discoverComposePath, discoverInclude, discoverExclude, discoverDryRun)
		},
	}
	cmd.Flags().StringVar(&discoverComposePath, "compose", "docker-compose.yml", "docker-compose file to read")
	cmd.Flags().BoolVar(&discoverDryRun, "dry-run", false, "print result without writing project.yaml")
	cmd.Flags().StringSliceVar(&discoverInclude, "include", nil, "service names to force-include")
	cmd.Flags().StringSliceVar(&discoverExclude, "exclude", nil, "service names to force-exclude")
	return cmd
}

func runDiscover(workDir, composePath string, include, exclude []string, dryRun bool) error {
	data, err := os.ReadFile(filepath.Join(workDir, composePath))
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}
	parsed, err := discover.ParseCompose(data)
	if err != nil {
		return fmt.Errorf("parse compose: %w", err)
	}
	filtered := discover.FilterServices(parsed.Services, include, exclude)
	if len(filtered) == 0 {
		return fmt.Errorf("no discoverable services (all filtered as infra or portless; use --include to force)")
	}
	services := discover.ToProjectServices(filtered)

	cfg := &project.Config{}
	cfgPath := filepath.Join(workDir, ".cerberus", "project.yaml")
	if existing, err := project.LoadFromFile(cfgPath); err == nil {
		cfg = existing
	}
	added := discover.MergeIntoConfig(cfg, services)
	hasActorKey := len(cfg.Actors) > 0

	fmt.Printf("discovered %d service(s); added %d new: %v\n", len(filtered), len(added), added)
	fmt.Print(discover.FormatGaps(discover.Gaps(cfg.Services), hasActorKey))

	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(workDir, ".cerberus"), 0755); err != nil {
		return err
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, out, 0644)
}
```

Register it in `cmd/cerberus/main.go` by adding `discoverCmd()` inside the existing `rootCmd.AddCommand(...)` argument list.

- [x] **Step 4: Run, verify PASS**

Run: `go test ./cmd/cerberus/ -run TestDiscoverCmd -v && go build ./...`
Expected: PASS + build OK.

- [x] **Step 5: Commit**

```bash
git add cmd/cerberus/main_discover.go cmd/cerberus/main_discover_test.go cmd/cerberus/main.go
git commit -m "feat(cmd): cerberus discover command"
```

---

### Task 7: Run-time hint to run discover first

**Files:**
- Create: `internal/discover/run_hint.go`
- Test: `internal/discover/run_hint_test.go`
- Modify: `cmd/cerberus/main_run.go` (at the start of RunE, after `projCfg` is loaded)

**Interfaces:**
- Produces: `func ShouldHintDiscover(services []project.Service, composeExists bool) bool`; `const HintMessage string`.

- [x] **Step 1: Write the failing test**

```go
package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/project"
)

func TestShouldHintDiscover(t *testing.T) {
	assert.True(t, ShouldHintDiscover(nil, true), "empty services + compose present → hint")
	assert.False(t, ShouldHintDiscover(nil, false), "no compose → no hint")
	assert.False(t, ShouldHintDiscover([]project.Service{{Name: "x"}}, true), "services configured → no hint")
}

func TestHintMessage_MentionsDiscover(t *testing.T) {
	assert.Contains(t, HintMessage, "cerberus discover")
}
```

- [x] **Step 2: Run, verify FAIL**

Run: `go test ./internal/discover/ -run "TestShouldHint|TestHintMessage" -v`
Expected: FAIL.

- [x] **Step 3: Implement `run_hint.go`**

```go
package discover

import "github.com/binoctal/cerberus/internal/project"

// HintMessage is printed by `cerberus run` when services look unconfigured.
const HintMessage = "hint: docker-compose.yml present but no services configured. Run `cerberus discover` to generate them, then fill domain/path_prefix/key."

// ShouldHintDiscover reports whether run should nudge the user toward discover.
func ShouldHintDiscover(services []project.Service, composeExists bool) bool {
	return len(services) == 0 && composeExists
}
```

Wire into `cmd/cerberus/main_run.go` RunE, right after `projCfg := loadProjectConfig(...)`:

```go
if composePath, _ := filepath.Abs("docker-compose.yml"); fileExists(composePath) {
	if discover.ShouldHintDiscover(projCfg.Services, true) {
		fmt.Println(discover.HintMessage)
	}
}
```

(Add a tiny `fileExists` helper if one isn't already in the package, and the `filepath`/`discover`/`fmt` imports.)

- [x] **Step 4: Run, verify PASS + build**

Run: `go test ./internal/discover/ -run "TestShouldHint|TestHintMessage" -v && go build ./...`
Expected: PASS + build OK.

- [x] **Step 5: Commit**

```bash
git add internal/discover/run_hint.go internal/discover/run_hint_test.go cmd/cerberus/main_run.go
git commit -m "feat(cmd): hint to run discover when services unconfigured"
```

---

## Self-Review Notes

- **Spec coverage:** compose parse (T1), infra filter (T2), port/health map (T3), merge preserving overrides (T4), gap report incl. actor key (T5), `cerberus discover` CLI with --compose/--dry-run/--include/--exclude (T6), run-time hint (T7). Spec's "不参与 endpoint 归属" — discover only writes name/url/health, never PathPrefix (T3/T4), matching the spec. Spec's "domain/key 不碰" — surfaced as gaps (T5), filled manually.
- **Type consistency:** `NamedComposeService` defined T2, consumed T3. `MergeIntoConfig` returns `[]string` (added names) used by T6's print. `Gaps`/`FormatGaps`/`ServiceGaps` consistent T5↔T6. `ShouldHintDiscover`/`HintMessage` consistent T7↔main_run wiring.
- **Known follow-up:** Task 6's `runDiscover` is the testable seam; the cobra RunE stays thin.
