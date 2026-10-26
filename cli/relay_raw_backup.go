package main

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// Backup writes and the backup encryption key (HA UI parity batch 3a). The
// key, whether a location's backups use it, and the first automatic setup
// (which creates it) stay with the Home Assistant UI: without a saved key,
// Home Assistant uploads every backup unencrypted (backup/manager.py), so a
// raw request never sets, clears, or bypasses it.

const rawNextBackupKey = "set the encryption key in the Home Assistant UI (Settings > System > Backups)"

// rawLocalBackupAgents: the locations on this Home Assistant itself (Core,
// Supervisor); every other agent (a network mount, a cloud) is remote.
var rawLocalBackupAgents = []string{"backup.local", "hassio.local"}

// checkRawBackup: the per-type rules of the backup writes; anything
// unreadable counts as touching the key.
func checkRawBackup(wsType string, message map[string]any) error {
	switch wsType {
	case "backup/generate":
		agents, _ := message["agent_ids"].([]any)
		_, password := message["password"]
		for _, agent := range agents {
			id, _ := agent.(string)
			password = password || !slices.Contains(rawLocalBackupAgents, id)
		}
		if password {
			return rawRefusal("backup with its own password or to a remote location (it would go there unencrypted)", "create it with backup/generate_with_automatic_settings, or to this Home Assistant only; "+rawNextBackupKey)
		}
	case "labs/update":
		if message["create_backup"] == true {
			return rawRefusal("Labs change with create_backup (Home Assistant waits for the whole backup, longer than a request may take)", "create the backup with backup/generate_with_automatic_settings first, then send labs/update without create_backup")
		}
	case "backup/config/update":
		create, createSet := message["create_backup"].(map[string]any)
		_, password := create["password"]
		_, configured := message["automatic_backups_configured"]
		keyed := password || configured || !createSet && message["create_backup"] != nil
		agents, agentsSet := message["agents"].(map[string]any)
		keyed = keyed || !agentsSet && message["agents"] != nil
		partial := !rawWhole(message, "schedule", "recurrence", "days", "time") || !rawWhole(message, "retention", "copies", "days")
		for _, agent := range agents {
			fields, ok := agent.(map[string]any)
			_, protected := fields["protected"]
			keyed = keyed || !ok || protected
			partial = partial || !rawWhole(fields, "retention", "copies", "days")
		}
		if keyed {
			return rawRefusal("change of the backup encryption password, of which backup locations are encrypted, or of the first automatic-backup setup", rawNextBackupKey)
		}
		if partial {
			return rawRefusal("partial schedule or retention (Home Assistant resets every field left out)", "send the whole block from backup/config/info: schedule with recurrence, days, and time; retention with copies and days")
		}
	}
	return nil
}

// rawWhole: message[key] is absent, a null retention (a location's: use
// the global one; Home Assistant rejects it at the top), or an object with
// every one of fields.
func rawWhole(message map[string]any, key string, fields ...string) bool {
	value, set := message[key]
	if !set || value == nil && key == "retention" {
		return true
	}
	block, ok := value.(map[string]any)
	for _, field := range fields {
		_, has := block[field]
		ok = ok && has
	}
	return ok
}

// rawRemoteBackupAgents: the remote locations a backup settings change
// names (they need a saved key first, rawBackupKeySaved).
func rawRemoteBackupAgents(message map[string]any) bool {
	create, _ := message["create_backup"].(map[string]any)
	agents, _ := create["agent_ids"].([]any)
	for _, agent := range agents {
		if id, _ := agent.(string); !slices.Contains(rawLocalBackupAgents, id) {
			return true
		}
	}
	return false
}

// rawBackupKeySaved reads the saved settings live: a remote location is
// added only when an encryption key is saved; an unreadable read sends
// nothing.
func rawBackupKeySaved(s *relaySession) (refusal, err error) {
	var info struct {
		Config struct {
			CreateBackup struct {
				Password *string `json:"password"`
			} `json:"create_backup"`
		} `json:"config"`
	}
	data, err := rawRead(s, "ws", map[string]any{"type": "backup/config/info"})
	if err == nil {
		err = json.Unmarshal(data, &info)
	}
	if err != nil {
		return nil, errors.New("cannot read the backup settings, so nothing was sent: " + err.Error())
	}
	if key := info.Config.CreateBackup.Password; key == nil || strings.TrimSpace(*key) == "" {
		return rawRefusal("remote backup location without a saved encryption key (its backups would go there unencrypted)", rawNextBackupKey), nil
	}
	return nil, nil
}
