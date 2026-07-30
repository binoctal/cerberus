package project

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uuidRE matches a canonical hyphenated v4 uuid (8-4-4-4-12 hex). Used to assert
// the generator produces a well-formed id without coupling to a fixed value.
var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestResolveGeneratedPathParams_UUID(t *testing.T) {
	out, err := ResolveGeneratedPathParams(map[string]string{"clientId": "uuid"})
	require.NoError(t, err)
	v, ok := out["clientId"]
	require.True(t, ok, "clientId missing")
	assert.True(t, uuidRE.MatchString(v), "clientId %q is not a uuid", v)
}

func TestResolveGeneratedPathParams_UUIDFreshPerCall(t *testing.T) {
	a, _ := ResolveGeneratedPathParams(map[string]string{"clientId": "uuid"})
	b, _ := ResolveGeneratedPathParams(map[string]string{"clientId": "uuid"})
	// A fresh uuid per resolution is the whole point — a stale/repeated value
	// would defeat modeling a client-chosen id.
	assert.NotEqual(t, a["clientId"], b["clientId"], "generator must produce a fresh value each call")
}

func TestResolveGeneratedPathParams_MultipleParams(t *testing.T) {
	out, err := ResolveGeneratedPathParams(map[string]string{"clientId": "uuid", "sessionId": "uuid"})
	require.NoError(t, err)
	assert.True(t, uuidRE.MatchString(out["clientId"]))
	assert.True(t, uuidRE.MatchString(out["sessionId"]))
	assert.NotEqual(t, out["clientId"], out["sessionId"], "two params in one call must differ")
}

func TestResolveGeneratedPathParams_UnknownGenerator(t *testing.T) {
	_, err := ResolveGeneratedPathParams(map[string]string{"x": "nanoid"})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unknown generator"), "err should name the unknown generator: %v", err)
	assert.True(t, strings.Contains(err.Error(), "uuid"), "err should list supported generators: %v", err)
}

func TestResolveGeneratedPathParams_Empty(t *testing.T) {
	out, err := ResolveGeneratedPathParams(nil)
	require.NoError(t, err)
	assert.Empty(t, out)
}

// Validation: declared generated_path_params are checked at config load.
func TestValidateGeneratedPathParams(t *testing.T) {
	cases := []struct {
		name string
		a    Actor
		want string // substring expected in the validation message, "" means no error
	}{
		{
			name: "valid uuid generator",
			a:    Actor{Name: "u", GeneratedPathParams: map[string]string{"clientId": "uuid"}},
			want: "",
		},
		{
			name: "unknown generator rejected",
			a:    Actor{Name: "u", GeneratedPathParams: map[string]string{"clientId": "nanoid"}},
			want: "unknown generator",
		},
		{
			name: "bad param name rejected",
			a:    Actor{Name: "u", GeneratedPathParams: map[string]string{"client id": "uuid"}},
			want: "not a valid param name",
		},
		{
			name: "none declared is fine",
			a:    Actor{Name: "u"},
			want: "",
		},
		{
			name: "name also captured is ambiguous",
			a: Actor{
				Name:                "u",
				GeneratedPathParams: map[string]string{"userId": "uuid"},
				Auth:                &AuthFlow{PathParams: map[string]string{"userId": "config.userId"}},
			},
			want: "not both",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := validateGeneratedPathParams(0, c.a)
			if c.want == "" {
				assert.Empty(t, got, "expected no validation error")
				return
			}
			assert.True(t, strings.Contains(got, c.want), "got %q, want substring %q", got, c.want)
		})
	}
}
