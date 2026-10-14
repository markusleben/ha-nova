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
	Related  json.RawMessage `json:"related,omitempty"`
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
			return err
		}
		env.Resolved = []verbTarget{target}
		result := getResult{State: state}
		if *withConfig {
			if result.ConfigID, err = resolveConfigID(s, entityID, domain, state); err != nil {
				return err
			}
			if result.Config, err = readStoredConfig(s, domain, result.ConfigID); err != nil {
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

// resolveConfigID never derives the config id from the entity_id slug
// (relay-api.md "ID Types & Resolution"): automations use attributes.id,
// scripts and scenes the registry unique_id.
func resolveConfigID(s *relaySession, entityID, domain string, state json.RawMessage) (string, error) {
	configID := ""
	if domain == "automation" {
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
		if err := s.wsDecode(map[string]any{"type": "config/entity_registry/get", "entity_id": entityID}, &entry, "the registry entry of "+entityID); err != nil {
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
