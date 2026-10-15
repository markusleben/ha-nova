package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const getUsage = "ha-nova get <entity_id> [--config] [--related]"

var entityIDPattern = regexp.MustCompile(`^[a-z0-9_]+\.[a-z0-9_]+$`)

type getResult struct {
	State    json.RawMessage `json:"state"`
	ConfigID string          `json:"config_id,omitempty"`
	Config   json.RawMessage `json:"config,omitempty"`
	// Managed is "yaml" when Config is the config Home Assistant loaded from
	// YAML or a package: readable, not editable through plan write.
	Managed string          `json:"managed,omitempty"`
	Note    string          `json:"note,omitempty"`
	Related json.RawMessage `json:"related,omitempty"`
}

func runGetCommand(paths runtimePaths, args []string) int {
	var opts verbOptions
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	withConfig := fs.Bool("config", false, "also read the stored config (automation/script/scene) by its config id")
	withRelated := fs.Bool("related", false, "also read search/related for the entity")
	positional, err := parseVerbArgs(fs, &opts, args, getUsage)
	entityID := ""
	if err == nil {
		entityID, err = singleEntityID(positional, getUsage)
	}
	domain, _, _ := strings.Cut(entityID, ".")
	if err == nil && *withConfig && domain != "automation" && domain != "script" && domain != "scene" {
		err = errors.New("--config supports only automation, script, and scene entities")
	}
	if err != nil {
		return verbUsageError("get", err)
	}
	return runReadVerb(paths, "get", opts, func(s *relaySession, env *verbEnvelope) error {
		state, target, err := readEntityState(s, entityID)
		if err != nil {
			// A registry-disabled (or unloaded) automation/script/scene has no
			// state but a stored config: resolve it through its registry row.
			var missing *verbFailure
			if !*withConfig || !errors.As(err, &missing) || (missing.code != "NOT_FOUND" && missing.code != "ENTITY_DISABLED" && missing.code != "ENTITY_NOT_LOADED") {
				return err
			}
			if target, err = registryConfigTarget(s, entityID, err); err != nil {
				return err
			}
		}
		env.Resolved = []verbTarget{target}
		result := getResult{State: state}
		if *withConfig {
			result.ConfigID, err = resolveConfigID(s, entityID, domain, state)
			if err == nil {
				result.Config, err = readStoredConfig(s, domain, result.ConfigID)
			}
			var missing *verbFailure
			if errors.As(err, &missing) && missing.code == "CONFIG_NOT_FOUND" {
				// No UI config: a YAML or package item. Read what Home
				// Assistant loaded; it stays read-only for the typed verbs.
				if loaded, ok, loadErr := loadedConfig(s, entityID, domain); loadErr != nil {
					err = loadErr
				} else if ok {
					result.Config, result.Managed, err = loaded, "yaml", nil
					result.Note = "defined in YAML or a package: readable here, not editable through ha-nova plan write (edit it with ha-nova:yaml-config)"
				}
			}
			if err != nil {
				return err
			}
		}
		if *withRelated {
			if result.Related, err = s.ws(map[string]any{"type": "search/related", "item_type": "entity", "item_id": entityID}); err != nil {
				return err
			}
		}
		env.Result = result
		return nil
	})
}

func singleEntityID(positional []string, usage string) (string, error) {
	if len(positional) != 1 {
		return "", errors.New("Usage: " + usage)
	}
	id := strings.TrimSpace(positional[0])
	if !entityIDPattern.MatchString(id) {
		return "", fmt.Errorf("%q is not an entity_id (domain.object_id)", id)
	}
	return id, nil
}

// readEntityState reads one exact state. A missing entity exits 5 with
// registry candidates for the same domain.
func readEntityState(s *relaySession, entityID string) (json.RawMessage, verbTarget, error) {
	status, body, err := s.coreGET("/api/states/" + entityID)
	if err != nil {
		return nil, verbTarget{}, err
	}
	if status == http.StatusNotFound {
		var entry struct {
			DisabledBy string `json:"disabled_by"`
		}
		err := s.wsDecode(map[string]any{"type": "config/entity_registry/get", "entity_id": entityID}, &entry, "the registry entry of "+entityID)
		switch {
		case err == nil && entry.DisabledBy != "": // registered but disabled: no state, not missing
			return nil, verbTarget{}, &verbFailure{exit: verbExitNotFound, code: "ENTITY_DISABLED",
				message: fmt.Sprintf("%s is disabled (by %s)", entityID, entry.DisabledBy), next: enableNext(entry.DisabledBy)}
		case err == nil: // registered and enabled, yet no state: never a "did you mean" loop on itself
			return nil, verbTarget{}, &verbFailure{exit: verbExitNotFound, code: "ENTITY_NOT_LOADED",
				message: entityID + " is registered but has no state; its integration may not be loaded",
				next:    "Check the integration (ha-nova:health, or Settings → Devices & services in Home Assistant)."}
		case !isUpstreamNotFound(err):
			return nil, verbTarget{}, err
		}
		domain, objectID, _ := strings.Cut(entityID, ".")
		similar, err := findEntities(s, findQuery{phrase: strings.ReplaceAll(objectID, "_", " "), domain: domain, limit: 10})
		if err != nil {
			return nil, verbTarget{}, err
		}
		return nil, verbTarget{}, &verbFailure{
			exit: verbExitNotFound, code: "NOT_FOUND",
			message:    fmt.Sprintf("%s does not exist", entityID),
			candidates: similar.Matches,
			next:       `Pick one candidate, or search with: ha-nova find "<phrase>"`,
		}
	}
	if status >= 400 {
		return nil, verbTarget{}, fmt.Errorf("Home Assistant answered HTTP %d for %s", status, entityID)
	}
	var parsed struct {
		Attributes struct {
			FriendlyName string `json:"friendly_name"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, verbTarget{}, fmt.Errorf("cannot parse the state of %s: %w", entityID, err)
	}
	return body, verbTarget{EntityID: entityID, Name: parsed.Attributes.FriendlyName, By: "exact"}, nil
}

// enableNext says how to re-enable by who disabled it, always with the plain
// Home Assistant UI path (an MCP-only client has no organize skill).
func enableNext(disabledBy string) string {
	switch disabledBy {
	case "device":
		return "Its device is disabled: re-enable the device (Settings → Devices & services → Devices) or pick another entity."
	case "config_entry": // "integration" only means disabled by default: the user may enable it
		return "Its integration is disabled: re-enable it in Settings → Devices & services, or pick another entity."
	}
	return "Enable it first (ha-nova:organize, or Settings → Devices & services → Entities) or pick another entity."
}

// resolveConfigID never derives the config id from the entity_id slug
// (relay-api.md "ID Types & Resolution"): automations use attributes.id,
// scripts and scenes the registry unique_id.
func resolveConfigID(s *relaySession, entityID, domain string, state json.RawMessage) (string, error) {
	configID := ""
	if domain == "automation" && state != nil {
		var parsed struct {
			Attributes struct {
				ID string `json:"id"`
			} `json:"attributes"`
		}
		_ = json.Unmarshal(state, &parsed) // readEntityState already validated this JSON
		configID = parsed.Attributes.ID
	} else {
		var entry struct {
			UniqueID string `json:"unique_id"`
		}
		// A YAML item without id has no registry row: HA answers not_found.
		if err := s.wsDecode(map[string]any{"type": "config/entity_registry/get", "entity_id": entityID}, &entry, "the registry entry of "+entityID); err != nil && !isUpstreamNotFound(err) {
			return "", err
		}
		configID = entry.UniqueID
	}
	if strings.TrimSpace(configID) == "" {
		return "", &verbFailure{
			exit: verbExitNotFound, code: "CONFIG_NOT_FOUND",
			message: fmt.Sprintf("%s has no config id, so its stored config cannot be read (YAML item without id?)", entityID),
			next:    "Do not address a config by its entity_id slug; give the YAML item an id first.",
		}
	}
	return configID, nil
}

func readStoredConfig(s *relaySession, domain, configID string) (json.RawMessage, error) {
	status, body, err := s.coreGET("/api/config/" + domain + "/config/" + url.PathEscape(configID))
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, &verbFailure{exit: verbExitNotFound, code: "CONFIG_NOT_FOUND", message: fmt.Sprintf("no stored %s config with id %q", domain, configID)}
	}
	if status >= 400 {
		return nil, fmt.Errorf("Home Assistant answered HTTP %d for the %s config %q", status, domain, configID)
	}
	return body, nil
}

// registryConfigTarget resolves a config entity without state — disabled or
// not loaded — through its registry row; with no row the state read's own
// error stands.
func registryConfigTarget(s *relaySession, entityID string, stateErr error) (verbTarget, error) {
	var entry struct {
		DisabledBy *string `json:"disabled_by"`
		Name       *string `json:"name"`
		Original   *string `json:"original_name"`
	}
	if err := s.wsDecode(map[string]any{"type": "config/entity_registry/get", "entity_id": entityID}, &entry, "the registry entry of "+entityID); err != nil {
		if isUpstreamNotFound(err) {
			return verbTarget{}, stateErr // the state read's own NOT_FOUND, candidates included
		}
		return verbTarget{}, err
	}
	name := ""
	if entry.Name != nil {
		name = *entry.Name
	} else if entry.Original != nil {
		name = *entry.Original
	}
	return verbTarget{EntityID: entityID, Name: name, Disabled: entry.DisabledBy != nil, By: "registry"}, nil
}
