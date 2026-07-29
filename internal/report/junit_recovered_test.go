package report

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/store"
)

func TestBuildJUnitCase_RecoveredIsPassingWithSuffix(t *testing.T) {
	v := store.Verdict{Target: "ws://h/ws", Status: "pass", Recovered: true}
	tc := buildJUnitCase(v, nil)
	assert.Nil(t, tc.Failure, "recovered does not fail the suite")
	assert.Nil(t, tc.Error)
	assert.Contains(t, tc.Name, "(recovered)", "recovered testcase is marked")
}

func TestBuildJUnitCase_NormalPassUnchanged(t *testing.T) {
	v := store.Verdict{Target: "ws://h/ws", Status: "pass"}
	tc := buildJUnitCase(v, nil)
	assert.Nil(t, tc.Failure)
	assert.NotContains(t, tc.Name, "(recovered)")
}
