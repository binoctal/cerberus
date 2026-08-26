package agent

import (
	"path/filepath"
	"testing"

	"github.com/binoctal/cerberus/internal/types"
)

func TestBrowserExpectWindowClamp(t *testing.T) {
	for _, c := range []struct{ in, want int }{{0, 10}, {-3, 10}, {1, 1}, {15, 15}, {99, 30}} {
		if got := expectWindowSeconds(c.in); got != c.want {
			t.Errorf("expectWindowSeconds(%d)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestShotPath(t *testing.T) {
	got := shotPath("/proj", "case-1", "after-create")
	want := filepath.Join("/proj", ".cerberus", "runtime", "shots", "case-1-after-create.png")
	if got != want {
		t.Errorf("shotPath=%q want %q", got, want)
	}
}

func TestFailReason(t *testing.T) {
	a := types.BrowserExpectAction{Selector: "text=Connected", Expectation: "text_present"}
	if r := failReason(a, true, "Connected"); r != "" {
		t.Errorf("pass must yield empty reason, got %q", r)
	}
	if r := failReason(a, false, ""); r == "" {
		t.Error("fail must yield a reason")
	}
}
