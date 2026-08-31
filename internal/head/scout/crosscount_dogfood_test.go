package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

// The loader attaches .cerberus/vocab/<protocol_ref>.vocab.yaml to each
// service (loader.go:146), so LoadFromFile yields the fully assembled
// services the generator will see on the next run.
func TestDogfoodCrossUserCount(t *testing.T) {
	cfg, err := project.LoadFromFile("../../../dogfood/realtime-e2e/.cerberus/project.yaml")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, svc := range cfg.Services {
		for _, c := range httpRouteCases(svc) {
			if len(c.ID) > 10 && c.ID[len(c.ID)-10:] == "-crossuser" {
				n++
			}
		}
	}
	if n != 7 {
		t.Fatalf("crossuser cases = %d, want 7 (spec's candidate count)", n)
	}
}
