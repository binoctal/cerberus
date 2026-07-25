package scout

// ws_relay.go previously expanded LLM-authored ws_relay intents (a JSON body
// on a ws_relay test case) into multi-connection Steps cases, plus the covered
// map for WSCasesCovered. The S2 tool-calling migration replaced this path: the
// LLM now emits begin_case + ws_connect/ws_send/ws_receive/ws_disconnect tool
// calls directly, and assemblePlan (assembly.go) authors the Steps case plus the
// covered map in one pass. This file is intentionally empty; the deletion lands
// here so the git history records the removal at its original location.
