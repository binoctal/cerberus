package scout

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// realResponderCases emits request-response exchanges against a REAL-process
// role: the emulated client sends T (routed at the real role via deviceId),
// and the real process itself replies U — no responder driver, the reply is
// the SUT's own behavior. Pairs come from the real role's `responses`
// declaration; payload defaults from its `request_payload` for T.
//
// Distinct from wsRequestResponseCases: that generator drives an EMULATED
// responder through the role's test driver; here the responder is a real
// process cerberus never impersonates. Also unlike realE2ECases (which
// hardcodes the session:start/send/stop lifecycle), the exchange family is
// fully declarative.
func realResponderCases(svc project.Service, realRoles map[string]bool) []agent.TestCase {
	if len(realRoles) == 0 || svc.Protocol == nil {
		return nil
	}
	client := ""
	for _, roleName := range slices.Sorted(maps.Keys(svc.Protocol.Roles)) {
		if realRoles[roleName] {
			continue
		}
		// HTTP-only roles carry a credential but never connect over WS —
		// they exist for AuthRole injection, not as the emulated client.
		if r := svc.Protocol.Roles[roleName]; r != nil && r.CredentialRef != "" && !r.HTTPOnly {
			client = roleName
			break
		}
	}
	if client == "" {
		return nil
	}
	var cases []agent.TestCase
	for _, roleName := range slices.Sorted(maps.Keys(svc.Protocol.Roles)) {
		role := svc.Protocol.Roles[roleName]
		if !realRoles[roleName] || role == nil || len(role.Responses) == 0 {
			continue
		}
		for _, recvType := range slices.Sorted(maps.Keys(role.Responses)) {
			replyType := role.Responses[recvType]
			// request_payload values parse as raw JSON when they look like it
			// (e.g. "[]" for the sync families whose handlers type-assert
			// arrays); otherwise they ship as strings.
			payload := map[string]any{
				"deviceId": "{{" + roleName + ".deviceId}}",
			}
			for k, v := range role.RequestPayload[recvType] {
				payload[k] = rawJSONOrString(v)
			}
			cases = append(cases, agent.TestCase{
				ID:      wsCaseID(svc.Name, roleName+"-realresp", sanitizeTypeID(recvType)),
				Name:    fmt.Sprintf("%s %s over real %s: %s -> %s", svc.Name, client, roleName, recvType, replyType),
				Service: svc.Name,
				Target:  svc.URL,
				Action:  "ws_flow",
				Expectation: fmt.Sprintf("%s: the REAL %s process receives %s and replies %s over the relay",
					svc.Name, roleName, recvType, replyType),
				Priority: 0.7,
				Claims:   roleClaimBindings(role),
				Steps: []agent.TestStep{
					{Action: "ws_connect", ConnectionID: client, Role: client},
					{Action: "ws_send", ConnectionID: client, Message: wsSendBodyAny(recvType, payload)},
					{Action: "ws_receive", ConnectionID: client, Type: replyType, Timeout: 15},
				},
			})
		}
	}
	return cases
}

// wsSendBodyAny marshals a send frame with an arbitrary-typed payload map
// (wsSendBody stringifies every value; the sync families need real arrays).
func wsSendBodyAny(typ string, payload map[string]any) string {
	b, err := json.Marshal(map[string]any{"type": typ, "payload": payload})
	if err != nil {
		return fmt.Sprintf(`{"type":%q}`, typ)
	}
	return string(b)
}

// rawJSONOrString parses v as JSON when possible (arrays/objects/numbers),
// else returns it verbatim as a string.
func rawJSONOrString(v string) any {
	var out any
	if err := json.Unmarshal([]byte(v), &out); err == nil {
		return out
	}
	return v
}
