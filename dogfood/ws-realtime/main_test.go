package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServer_Login(t *testing.T) {
	srv := httptest.NewServer(newServer().routes())
	defer srv.Close()

	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
		wantToken  bool
	}{
		{name: "valid creds", body: `{"email":"web@dogfood.local","password":"dogfood-web"}`, wantStatus: http.StatusOK, wantToken: true},
		{name: "missing creds", body: `{"email":"","password":""}`, wantStatus: http.StatusUnauthorized, wantToken: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(srv.URL+"/login", "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status=%d want %d", resp.StatusCode, tc.wantStatus)
			}
			var got struct {
				Token string `json:"token"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&got)
			if tc.wantToken && got.Token == "" {
				t.Fatal("expected non-empty token")
			}
		})
	}
}
