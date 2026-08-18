package scout

import (
	"maps"
	"slices"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// deviceRestartCases emits the bridge reconnect pair: web→bridge device:restart
// (the REAL bridge exits on it — bridge.go handleDeviceRestart calls
// os.Exit(0) after matching its own deviceId) followed by the harness
// relaunch (process_restart step), with the reconnected bridge's
// device:online broadcast observed on the web connection. The relaunch does
// NOT re-run pairing: the isolated-HOME config persists, so the bridge
// returns with the same deviceId.
//
// Sacrificial-role rule: only emitted when TWO OR MORE real bridge roles
// exist (roles bound to real-process actors with a deviceId param). The
// alphabetically-LAST one is restarted — the primary "bridge" role carries
// every other case's {{bridge.deviceId}} routing, so it must stay up; with a
// single bridge there is no spare and the pair stays uncovered. Emitted LAST
// so the sequential plan restarts the bridge after every other case ran
// (process_restart kills the child mid-run).
func deviceRestartCases(svc project.Service, realRoles map[string]bool) []agent.TestCase {
	if svc.Protocol == nil || svc.Vocabulary == nil {
		return nil
	}
	var bridgeRoles []string
	for name := range maps.Keys(svc.Protocol.Roles) {
		if !realRoles[name] {
			continue
		}
		r := svc.Protocol.Roles[name]
		if r == nil || r.Params == nil || r.Params["deviceId"] == "" {
			continue
		}
		bridgeRoles = append(bridgeRoles, name)
	}
	if len(bridgeRoles) < 2 {
		return nil
	}
	slices.Sort(bridgeRoles)
	victim := bridgeRoles[len(bridgeRoles)-1]
	return []agent.TestCase{{
		ID:      wsCaseID(svc.Name, victim, "restart-pair"),
		Name:    svc.Name + " web restarts the real " + victim + " bridge and observes its reconnect broadcast",
		Service: svc.Name, Target: svc.URL, Action: "ws_flow",
		Expectation: svc.Name + ": web sends device:restart at the REAL " + victim + " bridge via its deviceId (the process exits), the harness relaunches it (same identity — pairing persists), and the reconnected bridge's device:online broadcast reaches the web connection",
		Priority:    0.5,
		Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "web", Role: "web"},
			// device:restart is handler-validated per device: only the victim
			// (matching deviceId) exits; the primary bridge ignores it.
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny("device:restart", map[string]any{
				"deviceId": "{{" + victim + ".deviceId}}",
			})},
			// Relaunch without re-pairing; ready_pattern gates the next step.
			{Action: "process_restart", Role: victim},
			{Action: "ws_receive", ConnectionID: "web", Type: "device:online", Timeout: 45},
		},
	}}
}
