package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Prompts expose the skills (ha-nova and ha-nova-<name>, from
// skills/<name>/SKILL.md of the installed bundle); resources expose the
// shared reference files as ha-nova://skills/<path>.md. Read-only, and no
// path outside the bundle's skills directory is ever opened.

const mcpPromptPreamble = `You are running HA NOVA through MCP tools, not a shell. Where the
instructions below say "ha-nova find|get|history|trace", call ha_find,
ha_get, ha_history, ha_trace; "ha-nova call" is ha_call; "ha-nova plan
call|write|delete|revert" is ha_plan with that kind; "ha-nova apply" is
ha_apply. A raw "ha-nova relay core --method GET" or a read "ha-nova relay
ws" is ha_read; there is no raw write — plan it with ha_plan. Pass a
confirmation code to ha_apply only when the user typed it.

---

`

const mcpResourcePrefix = "ha-nova://skills/"

func skillsRoot(paths runtimePaths) string {
	return filepath.Join(resolveSourceRoot(paths), "skills")
}

// skillPrompts maps prompt name → SKILL.md path, sorted by name.
func skillPrompts(paths runtimePaths) ([]string, map[string]string) {
	root := skillsRoot(paths)
	entries, _ := os.ReadDir(root)
	files := map[string]string{}
	var names []string
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name(), "SKILL.md")
		if _, err := os.Stat(path); !entry.IsDir() || err != nil {
			continue
		}
		name := "ha-nova-" + entry.Name()
		if entry.Name() == "ha-nova" {
			name = "ha-nova"
		}
		files[name] = path
		names = append(names, name)
	}
	sort.Strings(names)
	return names, files
}

func (srv *mcpServer) listPrompts() (map[string]any, *rpcError) {
	names, _ := skillPrompts(srv.paths)
	prompts := []map[string]any{}
	for _, name := range names {
		prompts = append(prompts, map[string]any{"name": name, "description": "HA NOVA skill " + name + " (MCP tool mapping included)"})
	}
	return map[string]any{"prompts": prompts}, nil
}

func (srv *mcpServer) getPrompt(raw json.RawMessage) (map[string]any, *rpcError) {
	var params struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &params)
	_, files := skillPrompts(srv.paths)
	path, known := files[params.Name]
	body, err := os.ReadFile(path)
	if !known || err != nil {
		return nil, &rpcError{Code: rpcInvalidParams, Message: "unknown prompt: " + params.Name}
	}
	return map[string]any{"description": "HA NOVA skill " + params.Name, "messages": []map[string]any{
		{"role": "user", "content": map[string]any{"type": "text", "text": mcpPromptPreamble + string(body)}},
	}}, nil
}

// referenceFiles lists the shared markdown references (every .md under
// skills/ except the SKILL.md prompts), as slash paths relative to skills/.
func referenceFiles(paths runtimePaths) []string {
	root := skillsRoot(paths)
	var files []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && entry.Type().IsRegular() && strings.HasSuffix(path, ".md") && entry.Name() != "SKILL.md" {
			if rel, relErr := filepath.Rel(root, path); relErr == nil {
				files = append(files, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func (srv *mcpServer) listResources() (map[string]any, *rpcError) {
	resources := []map[string]any{}
	for _, rel := range referenceFiles(srv.paths) {
		resources = append(resources, map[string]any{"uri": mcpResourcePrefix + rel, "name": rel, "mimeType": "text/markdown"})
	}
	return map[string]any{"resources": resources}, nil
}

// readResource serves only a URI that names one listed reference file:
// the list itself is the allowlist, so no traversal or symlink trick can
// reach anything else.
func (srv *mcpServer) readResource(raw json.RawMessage) (map[string]any, *rpcError) {
	var params struct {
		URI string `json:"uri"`
	}
	_ = json.Unmarshal(raw, &params)
	rel, prefixed := strings.CutPrefix(params.URI, mcpResourcePrefix)
	notFound := &rpcError{Code: rpcInvalidParams, Message: "resource not found", Data: map[string]any{"uri": params.URI}}
	if !prefixed || !containsText(referenceFiles(srv.paths), rel) {
		return nil, notFound
	}
	body, err := os.ReadFile(filepath.Join(skillsRoot(srv.paths), filepath.FromSlash(rel)))
	if err != nil {
		return nil, notFound
	}
	return map[string]any{"contents": []map[string]any{{"uri": params.URI, "mimeType": "text/markdown", "text": string(body)}}}, nil
}
