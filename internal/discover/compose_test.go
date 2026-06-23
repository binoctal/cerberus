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
