package ai

import (
	"fmt"
	"testing"
)

func BenchmarkResponseCache_SetGet(b *testing.B) {
	cache := NewResponseCache(5 * 60 * 1e9) // 5 min TTL

	b.Run("set", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.Set(fmt.Sprintf("prompt-%d", i), "response", TokenUsage{TotalTokens: 100})
		}
	})

	b.Run("get_hit", func(b *testing.B) {
		cache.Set("cached-prompt", "cached-response", TokenUsage{TotalTokens: 100})
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cache.Get("cached-prompt")
		}
	})

	b.Run("get_miss", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cache.Get(fmt.Sprintf("miss-%d", i))
		}
	})
}

func BenchmarkResponseCache_Eviction(b *testing.B) {
	cache := NewResponseCache(5 * 60 * 1e9)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Set(fmt.Sprintf("prompt-%d", i), "response", TokenUsage{TotalTokens: 100})
	}
}
