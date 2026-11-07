package main

import "testing"

// Writes that can carry data out of the house are typed in the plans and at
// the gate alike (spec §2): file writes, backup locations, and flows of an
// integration that provides one.
func TestGateExportWritesAreTyped(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"config_entries/get": func(map[string]any) any {
			return []any{map[string]any{"entry_id": "E1", "domain": "webdav"}, map[string]any{"entry_id": "E2", "domain": "hue"}}
		},
	})
	session := func() (*relaySession, error) { return newRelaySession(paths, verbOptions{}) }
	for _, tc := range []struct {
		name, endpoint, body string
		want                 gateDecision
	}{
		{"file write", "files", `{"action":"write_file","path":"automations.yaml","content":"[]"}`, gateTyped},
		{"backup locations", "ws", `{"type":"backup/config/update","create_backup":{"agent_ids":["backup.local","webdav.E1"]}}`, gateTyped},
		{"backup encryption, refused by the raw guard", "ws", `{"type":"backup/config/update","agents":{"webdav.E1":{"protected":true}}}`, gateRefuse},
		{"backup schedule only", "ws", `{"type":"backup/config/update","schedule":{"recurrence":"daily","time":"04:00","days":[]}}`, gatePass},
		{"backup location setup flow", "core", `{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"webdav"}}`, gateTyped},
		{"other setup flow", "core", `{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"hue"}}`, gatePass},
		{"backup location options flow", "core", `{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"E1"}}`, gateTyped},
		{"other options flow", "core", `{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"E2"}}`, gatePass},
		{"unlisted entry's options flow (Home Assistant refuses it)", "core", `{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"E9"}}`, gatePass},
		{"backup location subentry flow", "core", `{"method":"POST","path":"/api/config/config_entries/subentries/flow","body":{"handler":["E1","target"]}}`, gateTyped},
	} {
		var opened []*relaySession
		check, err := gateVerdict(tc.endpoint, []byte(tc.body), func() (*relaySession, error) {
			s, err := session()
			if s != nil {
				opened = append(opened, s)
			}
			return s, err
		})
		for _, s := range opened {
			s.close()
		}
		if err != nil || check.Verdict != tc.want {
			t.Errorf("%s: gate %+v err %v, want %s", tc.name, check, err, tc.want)
		}
	}
}
