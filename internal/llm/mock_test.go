package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockClient_Stream(t *testing.T) {
	mock := NewMockClient(map[string]string{
		"default": `{"status":"pass","confidence":0.9}`,
	})

	events, err := mock.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)

	var collected strings.Builder
	var gotDone bool
	for evt := range events {
		switch evt.Type {
		case StreamDelta:
			collected.WriteString(evt.Content)
		case StreamDone:
			gotDone = true
			assert.NotNil(t, evt.Usage)
		case StreamError:
			t.Fatalf("unexpected error: %v", evt.Err)
		}
	}

	assert.True(t, gotDone, "should receive done event")
	assert.Equal(t, `{"status":"pass","confidence":0.9}`, collected.String())
}

func TestMockClient_Stream_DefaultResponse(t *testing.T) {
	mock := NewMockClient(nil)

	events, err := mock.Stream(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "test"}},
	})
	require.NoError(t, err)

	var gotDone bool
	for evt := range events {
		if evt.Type == StreamDone {
			gotDone = true
		}
	}
	assert.True(t, gotDone)
}
