package llm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSEScanner_SingleEvent(t *testing.T) {
	input := "data: hello world\n\n"
	s := newSSEScanner(strings.NewReader(input))

	require.True(t, s.Next())
	eventType, data := s.Event()
	assert.Equal(t, "", eventType)
	assert.Equal(t, "hello world", data)
	assert.False(t, s.Next())
}

func TestSSEScanner_EventType(t *testing.T) {
	input := "event: message\ndata: {\"text\":\"hi\"}\n\n"
	s := newSSEScanner(strings.NewReader(input))

	require.True(t, s.Next())
	eventType, data := s.Event()
	assert.Equal(t, "message", eventType)
	assert.Equal(t, `{"text":"hi"}`, data)
}

func TestSSEScanner_MultipleEvents(t *testing.T) {
	input := "data: first\n\ndata: second\n\n"
	s := newSSEScanner(strings.NewReader(input))

	require.True(t, s.Next())
	_, data1 := s.Event()
	assert.Equal(t, "first", data1)

	require.True(t, s.Next())
	_, data2 := s.Event()
	assert.Equal(t, "second", data2)

	assert.False(t, s.Next())
}

func TestSSEScanner_MultilineData(t *testing.T) {
	input := "data: line1\ndata: line2\ndata: line3\n\n"
	s := newSSEScanner(strings.NewReader(input))

	require.True(t, s.Next())
	_, data := s.Event()
	assert.Equal(t, "line1\nline2\nline3", data)
}

func TestSSEScanner_CommentLines(t *testing.T) {
	input := ": this is a comment\ndata: actual data\n: another comment\n\n"
	s := newSSEScanner(strings.NewReader(input))

	require.True(t, s.Next())
	_, data := s.Event()
	assert.Equal(t, "actual data", data)
}

func TestSSEScanner_DataNoSpace(t *testing.T) {
	input := "data:no_space\n\ndata: with_space\n\n"
	s := newSSEScanner(strings.NewReader(input))

	require.True(t, s.Next())
	_, d1 := s.Event()
	assert.Equal(t, "no_space", d1)

	require.True(t, s.Next())
	_, d2 := s.Event()
	assert.Equal(t, "with_space", d2)
}

func TestSSEScanner_EmptyInput(t *testing.T) {
	s := newSSEScanner(strings.NewReader(""))
	assert.False(t, s.Next())
}

func TestSSEScanner_BlankLinesIgnored(t *testing.T) {
	input := "\n\n\ndata: after blanks\n\n"
	s := newSSEScanner(strings.NewReader(input))

	require.True(t, s.Next())
	_, data := s.Event()
	assert.Equal(t, "after blanks", data)
}

func TestSSEScanner_EventReset(t *testing.T) {
	input := "event: type1\ndata: payload1\n\nevent: type2\ndata: payload2\n\n"
	s := newSSEScanner(strings.NewReader(input))

	require.True(t, s.Next())
	et1, d1 := s.Event()
	assert.Equal(t, "type1", et1)
	assert.Equal(t, "payload1", d1)

	require.True(t, s.Next())
	et2, d2 := s.Event()
	assert.Equal(t, "type2", et2)
	assert.Equal(t, "payload2", d2)
}

func TestSSEScanner_DoneSentinel(t *testing.T) {
	// OpenAI-style [DONE] sentinel.
	input := "data: {\"choices\":[]}\n\ndata: [DONE]\n\n"
	s := newSSEScanner(strings.NewReader(input))

	require.True(t, s.Next())
	_, d1 := s.Event()
	assert.Equal(t, `{"choices":[]}`, d1)

	require.True(t, s.Next())
	_, d2 := s.Event()
	assert.Equal(t, "[DONE]", d2)
}
