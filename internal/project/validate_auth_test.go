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

func TestValidateAuthFlow_ProvisioningOnlyViaHTTPLogin(t *testing.T) {
	// An actor with neither a static token nor a token_from is provisioning-only
	// on the primary login; that is valid when http_login supplies the real
	// credential (e.g. the dogfood admin-actor: /api/dev/setup seeds role:admin,
	// /api/dev/login returns the JWT).
	actor := Actor{
		Name: "admin-actor",
		Auth: &AuthFlow{
			Login: AuthLogin{Method: "POST", Path: "/api/dev/setup", Body: map[string]string{
				"email": "{email}", "password": "{password}", "role": "admin",
			}},
			InjectAs: "Authorization: Bearer {token}",
			HTTPLogin: &AuthLogin{Method: "POST", Path: "/api/dev/login", Body: map[string]string{
				"email": "{email}", "password": "{password}",
			}},
			HTTPTokenFrom: "token",
		},
		Credentials: CredentialRef{Email: "admin@openagents.local", Password: "admin123456"},
	}
	if got := validateAuthFlow(0, actor); got != "" {
		t.Fatalf("expected provisioning-only + http_login to validate, got %q", got)
	}

	// Without http_login and without a static token it must still fail — there
	// would be no credential at all.
	noHTTP := *actor.Auth
	noHTTP.HTTPLogin = nil
	noHTTP.HTTPTokenFrom = ""
	actor.Auth = &noHTTP
	if got := validateAuthFlow(0, actor); got == "" {
		t.Fatal("expected error: provisioning-only flow with no http_login and no static token")
	}
}
