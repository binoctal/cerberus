package project

import (
	"strings"
	"testing"
)

func TestExpandReplicas(t *testing.T) {
	yaml := `
project: {name: rt}
actors:
  - name: bridge-pty
    fidelity: real-process
    replicas: 3
    process:
      setup: ["./bin", "pair", "-n", "{{actor.name}}"]
      start: ["./bin", "start", "-d", "{{actor.name}}"]
`
	cfg, err := LoadFromYAML([]byte(yaml), "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Actors) != 3 {
		t.Fatalf("want 3 expanded actors, got %d", len(cfg.Actors))
	}
	want := []string{"bridge-pty-1", "bridge-pty-2", "bridge-pty-3"}
	for i, a := range cfg.Actors {
		if a.Name != want[i] {
			t.Fatalf("actor %d name = %q, want %q", i, a.Name, want[i])
		}
		if a.Replicas != 0 {
			t.Fatalf("expanded actor must carry Replicas 0, got %d", a.Replicas)
		}
	}
}

func TestExpandReplicasValidation(t *testing.T) {
	cases := []struct {
		name, yaml, wantErr string
	}{
		{"replicas on emulated actor", `
project: {name: rt}
actors:
  - name: a
    replicas: 2
`, "replicas requires fidelity real-process"},
		{"expanded name collision", `
project: {name: rt}
actors:
  - name: a
    fidelity: real-process
    replicas: 2
    process: {start: ["x"]}
  - name: a-1
    fidelity: real-process
    process: {start: ["x"]}
`, "duplicate actor name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFromYAML([]byte(tc.yaml), "")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
