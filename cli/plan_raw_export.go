package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// backupAgentDomains: the integrations that provide a backup location
// (homeassistant/components/<domain>/backup.py with an agent), checked
// against Home Assistant's dev branch on 2026-11-07; knx, zha, recorder,
// backup, and hassio only hook into backups. A custom integration is not
// listed (spec → Threat model).
var backupAgentDomains = map[string]bool{
	"aws_s3": true, "azure_storage": true, "backblaze_b2": true, "cloud": true, "cloudflare_r2": true, "dropbox": true,
	"google_drive": true, "idrive_e2": true, "kitchen_sink": true, "onedrive": true, "onedrive_for_business": true,
	"seafile": true, "sftp_storage": true, "synology_dsm": true, "webdav": true,
}

// rawExportTier types what can carry Home Assistant's data out of the house
// or change what it runs from its config folder (relay-typed-gate spec §2):
// a file write (a YAML write plus a reload acts unasked), a change to where
// automatic backups go, and starting a setup, reconfigure, options, or
// subentry flow of an integration that provides a backup location (a new
// remote location plus a backup carries Home Assistant's refresh tokens and
// phone webhook ids out). An entry list that can't be read counts as one.
func rawExportTier(n *rawNamer, endpoint string, payload map[string]any) (tierLevel, string) {
	switch endpoint {
	case "files":
		if payload["action"] == "write_file" {
			return tierTyped, "file_write"
		}
	case "ws":
		create, _ := payload["create_backup"].(map[string]any)
		_, agentIDs := create["agent_ids"]
		_, agents := payload["agents"]
		if payload["type"] == "backup/config/update" && (agentIDs || agents) {
			return tierTyped, "backup_locations"
		}
	case "core":
		method, _ := payload["method"].(string)
		raw, _ := payload["path"].(string)
		target, _ := relayCorePath(raw)
		body, _ := payload["body"].(map[string]any)
		if !strings.EqualFold(strings.TrimSpace(method), http.MethodPost) {
			break
		}
		switch target {
		case "/api/config/config_entries/flow":
			if domain, _ := body["handler"].(string); backupAgentDomains[domain] {
				return tierTyped, "backup_agent"
			}
		case "/api/config/config_entries/options/flow", "/api/config/config_entries/subentries/flow":
			entry := body["handler"]
			if pair, isPair := entry.([]any); isPair && len(pair) > 0 { // subentries: [entry_id, subentry_type]
				entry = pair[0]
			}
			if domain, read := n.entryDomain(entry); !read || backupAgentDomains[domain] {
				return tierTyped, "backup_agent"
			}
		}
	}
	return tierPreview, ""
}

// entryDomain: the integration of a config entry, read live; read is false
// when the list can't be read. An entry it doesn't list has no domain: Home
// Assistant refuses a flow for it.
func (n *rawNamer) entryDomain(entry any) (domain string, read bool) {
	data, err := n.read("ws", map[string]any{"type": "config_entries/get"})
	var rows []struct {
		EntryID string `json:"entry_id"`
		Domain  string `json:"domain"`
	}
	if err != nil || json.Unmarshal(data, &rows) != nil || rows == nil {
		return "", false
	}
	for _, row := range rows {
		if row.EntryID == "" || row.Domain == "" { // null, or a row without its fields: unread
			return "", false
		}
		if row.EntryID == entry {
			return row.Domain, true
		}
	}
	return "", true
}
