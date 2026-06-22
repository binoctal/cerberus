package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A malformed 200 body must surface as an error, not be swallowed into an
// empty success (which then gets misattributed to the model's output).
func TestOpenAICompleteMalformedBodyReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-valid-json{"))
	}))
	defer srv.Close()

	c := NewOpenAIClient("k", "gpt-4", srv.URL)
	_, err := c.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err, "malformed 200 body must return error, not empty success")
}
