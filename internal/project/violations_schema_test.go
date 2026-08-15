package project

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func validViolationsProtocol() *Protocol {
	return &Protocol{
		Roles: map[string]*ProtocolRole{
			"web":    {CredentialRef: "web"},
			"bridge": {CredentialRef: "b1"},
		},
		Violations: []Violation{
			{ID: "oversize-message", Family: ViolationFamilyOversize, Role: "web",
				Trigger: ViolationTrigger{Bytes: 1048577, Type: "chat:message"},
				Expect:  ViolationExpect{CloseCode: 1009}},
			{ID: "bridge-rate-limit", Family: ViolationFamilyRateLimit, Role: "bridge",
				Trigger: ViolationTrigger{Messages: 205, Type: "chat:message"},
				Expect:  ViolationExpect{FrameType: "error", Code: "RATE_LIMIT_EXCEEDED", CloseCode: 1008}},
			{ID: "missing-device-id", Family: ViolationFamilyRouteMissing, Role: "web",
				Trigger: ViolationTrigger{Type: "session:start", OmitFields: []string{"deviceId"}},
				Expect:  ViolationExpect{FrameType: "error", Code: "MISSING_DEVICE_ID"}},
			{ID: "csrf-no-origin", Family: ViolationFamilyHTTPAuth, Role: "web",
				Trigger: ViolationTrigger{Method: "POST", Path: "/api/dev/setup", DropHeaders: []string{"Origin"}},
				Expect:  ViolationExpect{HTTPStatus: 403}},
		},
	}
}

func TestValidateViolations(t *testing.T) {
	t.Run("valid matrix passes", func(t *testing.T) {
		assert.NoError(t, ValidateProtocol(validViolationsProtocol(), []Actor{{Name: "web"}, {Name: "b1"}}))
	})
	t.Run("unknown family rejected", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[0].Family = "nonsense"
		assert.ErrorContains(t, ValidateProtocol(p, []Actor{{Name: "web"}, {Name: "b1"}}), "violations[0].family")
	})
	t.Run("role must be declared", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[0].Role = "ghost"
		assert.ErrorContains(t, ValidateProtocol(p, []Actor{{Name: "web"}, {Name: "b1"}}), `violations[0].role "ghost" does not match`)
	})
	t.Run("oversize needs bytes and close_code", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[0].Trigger.Bytes = 0
		assert.ErrorContains(t, ValidateProtocol(p, []Actor{{Name: "web"}, {Name: "b1"}}), "violations[0].trigger.bytes")
	})
	t.Run("rate_limit needs messages, frame and close", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[1].Trigger.Messages = 0
		assert.ErrorContains(t, ValidateProtocol(p, []Actor{{Name: "web"}, {Name: "b1"}}), "violations[1].trigger.messages")
		p = validViolationsProtocol()
		p.Violations[1].Expect.Code = ""
		assert.ErrorContains(t, ValidateProtocol(p, []Actor{{Name: "web"}, {Name: "b1"}}), "violations[1].expect.frame_type")
	})
	t.Run("route_missing needs type, omit_fields, frame+code", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[2].Trigger.OmitFields = nil
		assert.ErrorContains(t, ValidateProtocol(p, []Actor{{Name: "web"}, {Name: "b1"}}), "violations[2].trigger.type and .omit_fields")
	})
	t.Run("http_auth needs method, path, status", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[3].Trigger.Path = ""
		assert.ErrorContains(t, ValidateProtocol(p, []Actor{{Name: "web"}, {Name: "b1"}}), "violations[3].trigger.method and .path")
	})
	t.Run("empty id rejected", func(t *testing.T) {
		p := validViolationsProtocol()
		p.Violations[0].ID = ""
		assert.ErrorContains(t, ValidateProtocol(p, []Actor{{Name: "web"}, {Name: "b1"}}), "violations[0].id")
	})
}
