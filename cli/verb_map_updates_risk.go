package main

import (
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The update advisor's risk flags (update-advisor spec → Risk flags).

func (a *updateAdvisor) flagRisks() {
	if months := haMonthsAhead(a.coreFrom, a.coreTo); months >= 2 {
		a.risk = append(a.risk, map[string]any{"code": "CORE_SKIPS_RELEASES", "monthly_releases": months})
	}
	if haMonthsAhead(a.coreFrom, a.coreTo) >= 1 {
		a.flagCustomIntegrations()
	}
	for _, row := range a.rows {
		from, okFrom := majorVersion(row.Installed)
		to, okTo := majorVersion(row.Latest)
		if row.Kind != "core" && row.Kind != "firmware" && okFrom && okTo && to > from {
			a.risk = append(a.risk, map[string]any{"code": "MAJOR_VERSION", "entity_id": row.EntityID, "name": row.Name, "from": row.Installed, "to": row.Latest})
		}
		if row.Kind == "firmware" && row.device != "" {
			if access := a.accessOnDevice(row.device); len(access) > 0 {
				a.risk = append(a.risk, map[string]any{"code": "ACCESS_DEVICE_FIRMWARE", "entity_id": row.EntityID, "name": row.Name, "controls": access})
			}
		}
	}
}

// flagCustomIntegrations: integrations that don't ship with Home Assistant
// and are loaded here may lag behind a new monthly Core.
func (a *updateAdvisor) flagCustomIntegrations() {
	var manifests []struct {
		Domain  string `json:"domain"`
		Name    string `json:"name"`
		BuiltIn *bool  `json:"is_built_in"`
	}
	if err := a.s.wsDecode(manifestList, &manifests, "the integration manifests"); err != nil {
		a.gap("custom_integrations", err.Error())
		return
	}
	names := []string{}
	for _, m := range manifests {
		if m.BuiltIn != nil && !*m.BuiltIn && a.domains[m.Domain] {
			names = append(names, sayText(firstNonEmpty(m.Name, m.Domain), false))
		}
	}
	if len(names) > 0 {
		sort.Strings(names)
		a.risk = append(a.risk, map[string]any{"code": "CUSTOM_INTEGRATIONS", "names": names})
	}
}

// accessOnDevice: the access controls on a device (a lock, an alarm panel, a
// garage, gate, or door cover): a firmware update can leave them unmanaged.
func (a *updateAdvisor) accessOnDevice(device string) []string {
	var out []string
	for _, id := range a.x.enabled {
		if a.x.entityDevice[id] != device {
			continue
		}
		domain, _ := splitID(id)
		st := a.x.state[id]
		cover := domain == "cover" && st != nil && slices.Contains([]string{"garage", "gate", "door"}, st.deviceClass())
		if domain == "lock" || domain == "alarm_control_panel" || cover {
			out = append(out, a.x.name(id))
		}
	}
	return out
}

// haMonthsAhead: monthly releases from one Core version to another (0 for a
// patch, or when either is unreadable).
func haMonthsAhead(from, to string) int {
	f, okF := haVersion(from)
	t, okT := haVersion(to)
	if !okF || !okT {
		return 0
	}
	return max(0, (t[0]*12+t[1])-(f[0]*12+f[1]))
}

func majorVersion(v string) (int, bool) {
	digits, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	n, err := strconv.Atoi(digits)
	return n, err == nil
}
