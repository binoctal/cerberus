package project

import "fmt"

// Violation families supported by the deterministic generator.
const (
	ViolationFamilyOversize     = "oversize"
	ViolationFamilyRateLimit    = "rate_limit"
	ViolationFamilyRouteMissing = "route_missing"
	ViolationFamilyHTTPAuth     = "http_auth"
)

var validViolationFamilies = map[string]bool{
	ViolationFamilyOversize:     true,
	ViolationFamilyRateLimit:    true,
	ViolationFamilyRouteMissing: true,
	ViolationFamilyHTTPAuth:     true,
}

// Violation is one declared negative behavior: triggering it from Role must
// provoke Expect. SUT facts only — thresholds and codes live here, never in
// generator or executor source (cerberus stays SUT-generic).
type Violation struct {
	ID      string           `yaml:"id"`
	Family  string           `yaml:"family"`
	Role    string           `yaml:"role"`
	Trigger ViolationTrigger `yaml:"trigger"`
	Expect  ViolationExpect  `yaml:"expect"`
}

// ViolationTrigger carries the family-specific trigger fields; only the
// subset the family names is meaningful (validated).
type ViolationTrigger struct {
	Bytes       int               `yaml:"bytes"`             // oversize: payload size to send
	Messages    int               `yaml:"messages"`          // rate_limit: burst size (max + threshold; violations count per denied message)
	Type        string            `yaml:"type"`              // frame type to send
	OmitFields  []string          `yaml:"omit_fields"`       // route_missing: payload keys to drop
	Method      string            `yaml:"method"`            // http_auth
	Path        string            `yaml:"path"`              // http_auth
	DropHeaders []string          `yaml:"drop_headers"`      // http_auth: headers to drop
	Headers     map[string]string `yaml:"headers,omitempty"` // http_auth: explicit headers (e.g. a bad token)
}

// ViolationExpect: FrameType+Code for error frames, CloseCode for closes,
// HTTPStatus for HTTP rejections. rate_limit carries both a frame and a
// close expectation — first reaction then threshold close.
type ViolationExpect struct {
	FrameType  string `yaml:"frame_type"`
	Code       string `yaml:"code"`
	CloseCode  int    `yaml:"close_code"`
	HTTPStatus int    `yaml:"http_status"`
}

// validateViolations checks the declarations in isolation; ValidateProtocol
// wires it in next to validateProtocolHTTPTriggers.
func validateViolations(p *Protocol) error {
	for i, v := range p.Violations {
		prefix := fmt.Sprintf("violations[%d]", i)
		if v.ID == "" {
			return fmt.Errorf("%s.id is required", prefix)
		}
		if !validViolationFamilies[v.Family] {
			return fmt.Errorf("%s.family %q must be oversize, rate_limit, route_missing, or http_auth", prefix, v.Family)
		}
		if p.Roles[v.Role] == nil {
			return fmt.Errorf("%s.role %q does not match a declared role", prefix, v.Role)
		}
		switch v.Family {
		case ViolationFamilyOversize:
			if v.Trigger.Bytes <= 0 {
				return fmt.Errorf("%s.trigger.bytes is required for oversize", prefix)
			}
			if v.Expect.CloseCode == 0 {
				return fmt.Errorf("%s.expect.close_code is required for oversize", prefix)
			}
		case ViolationFamilyRateLimit:
			if v.Trigger.Messages <= 0 {
				return fmt.Errorf("%s.trigger.messages is required for rate_limit", prefix)
			}
			if v.Expect.FrameType == "" || v.Expect.Code == "" || v.Expect.CloseCode == 0 {
				return fmt.Errorf("%s.expect.frame_type, .code and .close_code are required for rate_limit", prefix)
			}
		case ViolationFamilyRouteMissing:
			if v.Trigger.Type == "" || len(v.Trigger.OmitFields) == 0 {
				return fmt.Errorf("%s.trigger.type and .omit_fields are required for route_missing", prefix)
			}
			if v.Expect.FrameType == "" || v.Expect.Code == "" {
				return fmt.Errorf("%s.expect.frame_type and .code are required for route_missing", prefix)
			}
		case ViolationFamilyHTTPAuth:
			if v.Trigger.Method == "" || v.Trigger.Path == "" {
				return fmt.Errorf("%s.trigger.method and .path are required for http_auth", prefix)
			}
			if v.Expect.HTTPStatus == 0 {
				return fmt.Errorf("%s.expect.http_status is required for http_auth", prefix)
			}
		}
	}
	return nil
}
