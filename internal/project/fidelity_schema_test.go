package project

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestValidateActors_Fidelity covers the per-actor fidelity manifest: default
// emulated (no process block), real-process requiring one, and rejection of
// unknown values.
func TestValidateActors_Fidelity(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string // substring of validation error; "" = valid
	}{
		{
			name: "default omittance is emulated and ok",
			yaml: `
actors:
  - name: web
    credentials: {}
`,
		},
		{
			name: "real-process with process block ok",
			yaml: `
actors:
  - name: b1
    credentials: {}
    fidelity: real-process
    process:
      start: ["sleep", "60"]
`,
		},
		{
			name: "real-process without process block rejected",
			yaml: `
actors:
  - name: b1
    credentials: {}
    fidelity: real-process
`,
			want: "process block is required",
		},
		{
			name: "real-process with empty start rejected",
			yaml: `
actors:
  - name: b1
    credentials: {}
    fidelity: real-process
    process:
      ready_pattern: "x"
`,
			want: "process block is required",
		},
		{
			name: "explicit emulated with process block rejected",
			yaml: `
actors:
  - name: b1
    credentials: {}
    fidelity: emulated
    process:
      start: ["sleep", "60"]
`,
			want: "must not have a process block",
		},
		{
			name: "unknown fidelity rejected",
			yaml: `
actors:
  - name: b1
    credentials: {}
    fidelity: simulated
`,
			want: `unknown fidelity "simulated"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			require.NoError(t, yaml.Unmarshal([]byte(tc.yaml), &cfg))
			var ve ValidationError
			validateActors(&cfg, &ve)
			if tc.want == "" {
				require.Empty(t, ve.Errors, "expected valid, got %v", ve.Errors)
			} else {
				require.Contains(t, strings.Join(ve.Errors, "; "), tc.want, "errors: %v", ve.Errors)
			}
		})
	}
}

// TestActorFidelityYAMLParse pins the YAML surface of the process block.
func TestActorFidelityYAMLParse(t *testing.T) {
	input := `
actors:
  - name: b1
    credentials: {}
    fidelity: real-process
    process:
      workdir: "/tmp/bridge"
      setup: ["./bridge", "pair", "-d", "b1"]
      start: ["./bridge", "start", "-d", "b1"]
      env:
        HOME: "{{runtime.dir}}/b1-home"
      capture_file: "{{runtime.dir}}/b1-home/config.json"
      capture_json:
        deviceId: "devices.b1.deviceId"
      ready_pattern: "connected"
      ready_timeout: 30s
`
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(input), &cfg))
	require.Len(t, cfg.Actors, 1)
	a := cfg.Actors[0]
	require.Equal(t, FidelityRealProcess, a.Fidelity)
	require.NotNil(t, a.Process)
	require.Equal(t, "/tmp/bridge", a.Process.Workdir)
	require.Equal(t, []string{"./bridge", "pair", "-d", "b1"}, a.Process.Setup)
	require.Equal(t, []string{"./bridge", "start", "-d", "b1"}, a.Process.Start)
	require.Equal(t, map[string]string{"HOME": "{{runtime.dir}}/b1-home"}, a.Process.Env)
	require.Equal(t, "{{runtime.dir}}/b1-home/config.json", a.Process.CaptureFile)
	require.Equal(t, map[string]string{"deviceId": "devices.b1.deviceId"}, a.Process.CaptureJSON)
	require.Equal(t, "connected", a.Process.ReadyPattern)
	require.Equal(t, "30s", a.Process.ReadyTimeout)
}
