package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTruncateStr_Short(t *testing.T) {
	assert.Equal(t, "hello", truncateStr("hello", 10))
}

func TestTruncateStr_Exact(t *testing.T) {
	assert.Equal(t, "hello", truncateStr("hello", 5))
}

func TestTruncateStr_Truncated(t *testing.T) {
	assert.Equal(t, "hello...", truncateStr("hello world", 5))
}

func TestTruncateStr_Empty(t *testing.T) {
	assert.Equal(t, "", truncateStr("", 10))
}

func TestTruncateStr_Unicode(t *testing.T) {
	assert.Equal(t, "你好世...", truncateStr("你好世界测试", 3))
}
