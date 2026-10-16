package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Restore of a deleted automation/script from its pre-delete config snapshot
// (typed-verbs spec, PR5 notes): the snapshot's bytes become a create under
// the original config id. It never overwrites (CONFIG_ID_IN_USE) and is
// classified like any write, so its content decides the tier. The snapshot
// is read once, at plan time; the plan freezes the body, so apply never
// re-reads a file that could have changed since the preview.

const planRestoreUsage = "ha-nova plan restore --snapshot <file> [--id <config_id>] (the delete result's restore names both)"

// restoreSnapshotPattern: a Relay snapshot file in the automations or scripts
// category (the store's own <category>/<name>-<stamp>.json.gz shape).
var restoreSnapshotPattern = regexp.MustCompile(`^(automations|scripts)/[a-z0-9-]{1,64}-\d{8}T\d{9}Z\.json\.gz$`)

// restoreDomain is the domain a snapshot file's category holds ("" if none).
func restoreDomain(file string) string {
	m := restoreSnapshotPattern.FindStringSubmatch(file)
	if m == nil {
		return ""
	}
	return strings.TrimSuffix(m[1], "s")
}

// restoreCommand is the one call that undoes a delete, named in its result.
func restoreCommand(action configAction, snapshot string) string {
	return "ha-nova plan restore --snapshot " + snapshot + " --id " + action.ConfigID
}

func (req *configRequest) parseRestore(positional []string) error {
	req.Domain = restoreDomain(req.Snapshot)
	switch {
	case len(positional) != 0 || req.Snapshot == "":
		return errors.New("Usage: " + planRestoreUsage)
	case req.Domain == "":
		return fmt.Errorf("--snapshot %q is not an automations/ or scripts/ snapshot file", req.Snapshot)
	case req.Domain == "script" && req.ConfigID == "":
		return errors.New("a script snapshot does not carry its key: pass --id (the delete result's restore names it)")
	case req.ConfigID != "" && !validConfigID(req.Domain, req.ConfigID):
		return fmt.Errorf("%q is not a %s config id", req.ConfigID, req.Domain)
	}
	req.Create = true
	return nil
}

// loadRestoreSnapshot reads the snapshot through the Relay and makes its
// config the draft, byte for byte (only whitespace removed, key order kept).
// It must hold one config of the file's domain, under the given id.
func (req *configRequest) loadRestoreSnapshot(s *relaySession) error {
	data, err := s.read("backups", map[string]any{"action": "load", "file": req.Snapshot})
	var call *relayCallError
	if errors.As(err, &call) && call.code == "SNAPSHOT_NOT_FOUND" {
		return &verbFailure{exit: verbExitNotFound, code: "SNAPSHOT_NOT_FOUND", message: "no config snapshot " + req.Snapshot + " in this server's Relay; nothing was planned",
			next: "Pass the snapshot file exactly as the delete result named it, on the same server."}
	}
	if err != nil {
		return err
	}
	var loaded struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &loaded); err != nil {
		return fmt.Errorf("cannot read the snapshot reply: %w", err)
	}
	mismatch := func(why string) error {
		return refusedWith("SNAPSHOT_MISMATCH", "snapshot "+req.Snapshot+" "+why,
			"Pass --snapshot and --id exactly as the delete result's restore names them; otherwise restore by hand (skills/ha-nova/config-snapshots.md).")
	}
	exact, cfg, err := exactConfig(loaded.Data)
	if err != nil {
		return mismatch("holds no single config object")
	}
	rawID, carriesID := cfg["id"]
	id, _ := rawID.(string)
	switch {
	case req.Domain == "script" && carriesID:
		return mismatch("holds an automation config, not a script")
	case req.Domain == "automation" && id == "":
		return mismatch("holds no automation config (it carries no id)")
	case req.Domain == "automation" && req.ConfigID != "" && req.ConfigID != id:
		return mismatch(fmt.Sprintf("holds automation %q, not %q", id, req.ConfigID))
	case req.Domain == "automation":
		req.ConfigID = id
	}
	req.Draft = exact
	if err := req.validate(); err != nil {
		return mismatch("does not fit: " + err.Error())
	}
	return nil
}

// restoreIDInUse: the id exists again — most likely restored already.
func restoreIDInUse(configID string) error {
	return refusedWith("CONFIG_ID_IN_USE", "config id "+configID+" already exists; a restore never overwrites",
		"It may be restored already: read it first (`ha-nova find \"<alias>\"`). Replace the existing item only if the user means it, through plan write.")
}
