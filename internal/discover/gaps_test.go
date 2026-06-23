package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/project"
)

func TestGaps_FlagMissingHostAndPrefix(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://localhost:8081"}, // both missing
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
