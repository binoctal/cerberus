package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAuthStorageBlob(t *testing.T) {
	b := authStorageBlob("tok", "refresh", "user_1", 1787675385090)
	var m map[string]any
	if err := json.Unmarshal([]byte(b), &m); err != nil {
		t.Fatal(err)
	}
	st := m["state"].(map[string]any)
	if st["token"] != "tok" {
		t.Errorf("token: %v", st["token"])
	}
	if st["refreshToken"] != "refresh" {
		t.Errorf("refreshToken: %v", st["refreshToken"])
	}
	if st["tokenExpiry"].(float64) != 1787675385090 {
		t.Errorf("tokenExpiry: %v", st["tokenExpiry"])
	}
	u := st["user"].(map[string]any)
	if u["id"] != "user_1" {
		t.Errorf("user id: %v", u["id"])
	}
	if !strings.Contains(b, `"version":0`) {
		t.Error("zustand persist version field missing")
	}
}
