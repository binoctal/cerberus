package project

import "testing"

func TestValidateAuthFlow_HTTPLogin(t *testing.T) {
	t.Run("valid http_login", func(t *testing.T) {
		af := &AuthFlow{
			Login:         AuthLogin{Method: "POST", Path: "/api/dev/setup"},
			TokenFrom:     "config.deviceToken",
			InjectAs:      "Authorization: Bearer {token}",
			HTTPLogin:     &AuthLogin{Method: "POST", Path: "/api/dev/login", Body: map[string]string{}},
			HTTPTokenFrom: "token",
		}
		if err := ValidateAuthFlow(af); err != nil {
			t.Fatalf("expected valid, got %v", err)
		}
	})
	t.Run("http_login without http_token_from fails", func(t *testing.T) {
		af := &AuthFlow{
			Login:     AuthLogin{Method: "POST", Path: "/api/dev/setup"},
			TokenFrom: "config.deviceToken",
			InjectAs:  "Authorization: Bearer {token}",
			HTTPLogin: &AuthLogin{Method: "POST", Path: "/api/dev/login"},
		}
		if err := ValidateAuthFlow(af); err == nil {
			t.Fatal("expected error for http_login without http_token_from")
		}
	})
	t.Run("http_token_from without http_login fails", func(t *testing.T) {
		af := &AuthFlow{
			Login:         AuthLogin{Method: "POST", Path: "/api/dev/setup"},
			TokenFrom:     "config.deviceToken",
			InjectAs:      "Authorization: Bearer {token}",
			HTTPTokenFrom: "token",
		}
		if err := ValidateAuthFlow(af); err == nil {
			t.Fatal("expected error for http_token_from without http_login")
		}
	})
	t.Run("http_login empty path fails", func(t *testing.T) {
		af := &AuthFlow{
			Login:         AuthLogin{Method: "POST", Path: "/api/dev/setup"},
			TokenFrom:     "config.deviceToken",
			InjectAs:      "Authorization: Bearer {token}",
			HTTPLogin:     &AuthLogin{Method: "POST", Path: ""},
			HTTPTokenFrom: "token",
		}
		if err := ValidateAuthFlow(af); err == nil {
			t.Fatal("expected error for http_login with empty path")
		}
	})
}
