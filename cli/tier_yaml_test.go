package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
)

// yamlFilesRequest: the tier engine's /config reads behind
// yamlHelpersEndpoint (tier_yaml.go), which raw reads never reach.
func yamlFilesRequest(endpoint string, payload map[string]any) bool {
	p, _ := payload["path"].(string)
	yamlFile := strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".yml") || p == yamlLock
	return endpoint == "files" && len(payload) == 2 && strings.HasPrefix(p+"/", "/config/") &&
		(payload["action"] == "list_dir" || payload["action"] == "read_file" && yamlFile)
}

// fakeFiles answers the Relay's files endpoint from an in-memory /config
// (MAR-245). Every entry is older than the fake Home Assistant start unless
// changed says otherwise; the run lock records that start.
type fakeFiles struct {
	files    map[string]string             // path → content
	links    map[string]bool               // path → listed as a symlink
	changed  map[string]string             // path → ctime (files and directories)
	modified map[string]string             // path → mtime
	off      bool                          // file access off
	bare     string                        // a directory listed without entries
	reads    []string                      // files read, in order
	onRead   func(ff *fakeFiles, p string) // runs after each read_file
	mu       sync.Mutex
}

const (
	yamlTestStart  = "2026-11-11T09:20:52.840Z"
	yamlTestBefore = "2026-11-01T08:00:00.000Z"
	yamlTestAfter  = "2026-11-19T22:37:16.540Z"
)

func newFakeFiles(files map[string]string) *fakeFiles {
	ff := &fakeFiles{files: map[string]string{"/config/.ha_run.lock": `{"pid": 73, "version": 1, "ha_version": "2026.11.1", "start_ts": 1794388852.84}`},
		links: map[string]bool{}, changed: map[string]string{"/config/.ha_run.lock": yamlTestStart}, modified: map[string]string{}}
	for p, content := range files {
		ff.files[p] = content
	}
	return ff
}

func (ff *fakeFiles) reply(endpoint string, p map[string]any) (any, bool) {
	if endpoint != "files" {
		return nil, false
	}
	ff.mu.Lock()
	defer ff.mu.Unlock()
	if ff.off {
		return fakeRelayFailure{"FILE_ACCESS_DISABLED", "file access is off"}, true
	}
	target := fmt.Sprint(p["path"])
	if p["action"] == "read_file" {
		content, ok := ff.files[target]
		if !ok || ff.links[target] {
			return fakeRelayFailure{"FILE_NOT_FOUND", "not found"}, true
		}
		ff.reads = append(ff.reads, target)
		if ff.onRead != nil {
			ff.onRead(ff, target)
		}
		return map[string]any{"content": content, "size": len(content)}, true
	}
	seen := map[string]bool{}
	entries := []map[string]any{}
	for file := range ff.files {
		for child := file; child != "/config" && child != "/"; child = path.Dir(child) {
			if path.Dir(child) != target || seen[child] {
				continue
			}
			seen[child] = true
			kind, size := "dir", 4096
			if child == file {
				kind, size = "file", len(ff.files[file])
			}
			entry := map[string]any{"name": path.Base(child), "type": kind, "size": size, "modified": ff.time(ff.modified, child), "changed": ff.time(ff.changed, child)}
			if ff.links[child] {
				entry = map[string]any{"name": path.Base(child), "type": "symlink", "size": nil, "modified": nil, "changed": nil}
			}
			if v, set := ff.changed[child]; set && v == "" {
				delete(entry, "changed") // a Relay without the field
			}
			if v, set := ff.modified[child]; set && v == "" {
				entry["modified"] = nil
			}
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 && target != "/config" {
		return fakeRelayFailure{"FILE_NOT_FOUND", "not found"}, true
	}
	slices.SortFunc(entries, func(a, b map[string]any) int { return strings.Compare(a["name"].(string), b["name"].(string)) })
	if target == ff.bare {
		return map[string]any{}, true
	}
	return map[string]any{"entries": entries}, true
}

func (ff *fakeFiles) time(set map[string]string, p string) string {
	if v, ok := set[p]; ok && v != "" {
		return v
	}
	return yamlTestBefore
}

// mar245Files: the sanitized shape of the maintainer's /config: a YAML
// thermostat whose heater is a YAML template switch around a plug, among
// template sensors, a package, and backups Home Assistant never loads.
func mar245Files() map[string]string {
	return map[string]string{
		"/config/configuration.yaml": `default_config:
recorder:
  db_url: !secret recorder_db_url
homeassistant:
  customize: !include customize.yaml
  packages: !include_dir_named packages
automation: !include automations.yaml
switch: !include switch.yaml
template: !include_dir_merge_list templates
climate: !include climate.yaml
`,
		"/config/climate.yaml": `- platform: generic_thermostat
  name: Bath Thermostat
  heater: switch.bath_heater_safe
  target_sensor: sensor.bath_temperature_safe
  min_temp: 15
  away_temp: 16
`,
		"/config/switch.yaml": "# migrated to template\n[]\n",
		"/config/templates/00-existing.yaml": `- sensor:
  - name: Bath Temperature Safe
    unique_id: bath_temperature_safe
    state: "{{ states('sensor.bath_temperature') }}"
- switch:
  - name: Bath Heater Safe
    unique_id: bath_heater_safe
    state: "{{ states('switch.bath_plug') == 'on' }}"
    turn_on:
      - action: switch.turn_on
        target:
          entity_id: switch.bath_plug
    turn_off:
      - action: switch.turn_off
        target:
          entity_id: switch.bath_plug
- triggers:
    - trigger: state
      entity_id: binary_sensor.doorbell
  sensor:
    - name: Delivery
      unique_id: delivery
      state: "on"
`,
		"/config/templates/00-existing.yaml.bak":    "- switch:\n  - unique_id: bath_heater_safe\n    turn_on: {action: cover.open_cover, target: {entity_id: cover.garage_door}}\n",
		"/config/templates/sensor__pool.yaml":       "- sensor:\n  - name: Pool\n    unique_id: pool\n    state: \"1\"\n",
		"/config/packages/energy.yaml":              "template:\n  - sensor:\n    - name: Energy\n      unique_id: energy\n      state: \"1\"\nutility_meter:\n  daily:\n    source: sensor.energy\n",
		"/config/packages/energy.yaml.bak.20260108": "climate:\n  - platform: generic_thermostat\n    heater: switch.other\n",
		"/config/customize.yaml":                    "{}\n",
		"/config/automations.yaml":                  "- id: never_read\n",
	}
}

func readYAMLProjection(t *testing.T, ff *fakeFiles) yamlProjection {
	t.Helper()
	paths, _ := setupVerbRelayTest(t, func(_ *http.Request, endpoint string, p map[string]any) any {
		reply, _ := ff.reply(endpoint, p)
		return reply
	})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.read(yamlHelpersEndpoint, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var proj yamlProjection
	if err := json.Unmarshal(data, &proj); err != nil {
		t.Fatal(err)
	}
	return proj
}

func TestYAMLHelpersReadTheHome(t *testing.T) {
	ff := newFakeFiles(mar245Files())
	proj := readYAMLProjection(t, ff)
	if proj.Failure != "" || !slices.Equal(proj.Helpers["generic_thermostat"], []string{"switch.bath_heater_safe"}) || len(proj.Helpers["generic_hygrostat"]) != 0 {
		t.Fatalf("projection %+v", proj)
	}
	heater := proj.Switches["bath_heater_safe"]
	want := []yamlStep{{"switch.turn_on", []string{"switch.bath_plug"}}, {"switch.turn_off", []string{"switch.bath_plug"}}}
	if heater.Count != 1 || !heater.Plain || fmt.Sprint(heater.Steps) != fmt.Sprint(want) || proj.Unreadable {
		t.Fatalf("heater %+v", heater)
	}
	for _, never := range []string{"/config/automations.yaml", "/config/templates/00-existing.yaml.bak", "/config/packages/energy.yaml.bak.20260108", "/config/customize.yaml", "/config/secrets.yaml"} {
		if slices.Contains(ff.reads, never) {
			t.Fatalf("read %s (reads %v)", never, ff.reads)
		}
	}
}

func TestYAMLHelpersIncludeForms(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[string]string
		want  []string // generic_thermostat heaters, then generic_hygrostat humidifiers
	}{
		"a labeled key, an inline package, a hygrostat": {map[string]string{
			"/config/configuration.yaml": "climate upstairs:\n  - {platform: generic_thermostat, heater: switch.a}\nhomeassistant:\n  packages:\n    bath:\n      generic_hygrostat:\n        name: Bath\n        humidifier: switch.fan_relay\n"},
			[]string{"switch.a", "switch.fan_relay"}},
		"a labeled hygrostat Home Assistant ignores": {map[string]string{
			"/config/configuration.yaml": "climate: [{platform: generic_thermostat, heater: switch.a}]\ngeneric_hygrostat other:\n  humidifier: switch.y\n"},
			[]string{"switch.a"}},
		"merge_named packages": {map[string]string{
			"/config/configuration.yaml": "homeassistant:\n  packages: !include_dir_merge_named pk\n",
			"/config/pk/one.yaml":        "first:\n  climate:\n    - {platform: generic_thermostat, heater: switch.one}\n",
			"/config/pk/two.yaml":        "second:\n  climate: !include two_climate.yml\n",
			"/config/pk/two_climate.yml": "- {platform: generic_thermostat, heater: switch.two}\n"},
			[]string{"switch.one", "switch.two"}},
	} {
		t.Run(name, func(t *testing.T) {
			proj := readYAMLProjection(t, newFakeFiles(tc.files))
			got := append(slices.Clone(proj.Helpers["generic_thermostat"]), proj.Helpers["generic_hygrostat"]...)
			if proj.Failure != "" || !slices.Equal(got, tc.want) {
				t.Fatalf("projection %+v, want %v", proj, tc.want)
			}
		})
	}
	t.Run("dir_list walks subdirectories and skips hidden, secrets, and .yml", func(t *testing.T) {
		proj := readYAMLProjection(t, newFakeFiles(map[string]string{
			"/config/configuration.yaml":   "climate: !include_dir_list climate\n",
			"/config/climate/a.yaml":       "{platform: generic_thermostat, heater: switch.a}\n",
			"/config/climate/sub/b.yaml":   "{platform: generic_thermostat, heater: switch.b}\n",
			"/config/climate/.hidden.yaml": "{platform: generic_thermostat, heater: switch.hidden}\n",
			"/config/climate/.old/c.yaml":  "{platform: generic_thermostat, heater: switch.old}\n",
			"/config/climate/secrets.yaml": "{platform: generic_thermostat, heater: switch.secret}\n",
			"/config/climate/empty.yaml":   "",
			"/config/climate/other.yml":    "{platform: generic_thermostat, heater: switch.yml}\n",
		}))
		if proj.Failure != "" || !slices.Equal(proj.Helpers["generic_thermostat"], []string{"switch.a", "switch.b"}) {
			t.Fatalf("projection %+v", proj)
		}
	})
}

// Every gap answers a failure code, so the engine keeps the set unknown.
func TestYAMLHelpersFailClosed(t *testing.T) {
	edit := func(p, content string) func(*fakeFiles) {
		return func(ff *fakeFiles) { ff.files[p] = content }
	}
	conf := func(extra string) func(*fakeFiles) {
		return edit("/config/configuration.yaml", mar245Files()["/config/configuration.yaml"]+extra)
	}
	for name, tc := range map[string]struct {
		change func(*fakeFiles)
		want   string
	}{
		"file access off":             {func(ff *fakeFiles) { ff.off = true }, yamlFilesOff},
		"an unknown tag":              {conf("logger: !custom x\n"), yamlUnread},
		"a parse error":               {edit("/config/climate.yaml", "- platform: [\n"), yamlUnread},
		"two documents":               {edit("/config/climate.yaml", "[]\n---\n[]\n"), yamlUnread},
		"a duplicate key":             {edit("/config/climate.yaml", "- platform: generic_thermostat\n  heater: switch.a\n  heater: switch.b\n"), yamlUnread},
		"a duplicate root key":        {conf("climate: []\n"), yamlUnread},
		"a secret heater":             {edit("/config/climate.yaml", "- platform: generic_thermostat\n  heater: !secret heater\n"), yamlUnread},
		"a YAML 1.1 boolean as a key": {edit("/config/climate.yaml", "- platform: generic_thermostat\n  heater: switch.a\n  on: 1\n"), yamlUnread},
		"an include leaving its dir":  {conf("climate 2: !include ../etc/climate.yaml\n"), yamlUnread},
		"a percent-encoded include":   {conf("climate 2: !include heat%20cfg.yaml\n"), yamlUnread},
		"an absolute include":         {conf("climate 2: !include /config/climate.yaml\n"), yamlUnread},
		"a symlinked included file":   {func(ff *fakeFiles) { ff.links["/config/climate.yaml"] = true }, yamlUnread},
		"a symlinked ancestor":        {func(ff *fakeFiles) { ff.links["/config/templates"] = true }, yamlUnread},
		"a symlinked yaml in a walked directory": {func(ff *fakeFiles) {
			ff.files["/config/templates/linked.yaml"] = "[]\n"
			ff.links["/config/templates/linked.yaml"] = true
		}, yamlUnread},
		"an alias in a used subtree": {edit("/config/climate.yaml", "- &t {platform: generic_thermostat, heater: switch.a}\n- *t\n"), yamlUnread},
		"a merge key":                {edit("/config/climate.yaml", "- <<: {platform: generic_thermostat}\n  heater: switch.a\n"), yamlUnread},
		"a named collision": {func(ff *fakeFiles) {
			ff.files["/config/packages/sub/energy.yaml"] = "climate: []\n"
		}, yamlUnread},
		"an include cycle":    {func(ff *fakeFiles) { ff.files["/config/climate.yaml"] = "!include climate.yaml\n" }, yamlUnread},
		"a missing include":   {conf("climate 2: !include nowhere.yaml\n"), yamlUnread},
		"no run lock":         {func(ff *fakeFiles) { delete(ff.files, "/config/.ha_run.lock") }, yamlUnread},
		"a lock copied since": {func(ff *fakeFiles) { ff.changed["/config/.ha_run.lock"] = yamlTestAfter }, yamlChanged},
		"an edited file": {func(ff *fakeFiles) {
			ff.changed["/config/climate.yaml"], ff.modified["/config/climate.yaml"] = yamlTestAfter, yamlTestAfter
		}, yamlChanged},
		"an older backup moved over a file": {func(ff *fakeFiles) {
			ff.changed["/config/templates/00-existing.yaml"] = yamlTestAfter // mv keeps the mtime, never the ctime
		}, yamlChanged},
		"a hard link":                     {func(ff *fakeFiles) { ff.changed["/config/climate.yaml"] = yamlTestAfter }, yamlChanged},
		"a file added to a walked dir":    {func(ff *fakeFiles) { ff.modified["/config/templates"] = yamlTestAfter }, yamlChanged},
		"a walked dir renamed into place": {func(ff *fakeFiles) { ff.changed["/config/templates"] = yamlTestAfter }, yamlChanged},
		"a Relay without ctime":           {func(ff *fakeFiles) { ff.changed["/config/climate.yaml"] = "" }, yamlChanged},
		"a zero ctime":                    {func(ff *fakeFiles) { ff.changed["/config/climate.yaml"] = "1970-01-01T00:00:00.000Z" }, yamlChanged},
		"a ctime in the future":           {func(ff *fakeFiles) { ff.changed["/config/climate.yaml"] = "2099-01-01T00:00:00.000Z" }, yamlChanged},
		"a change while reading": {func(ff *fakeFiles) {
			ff.onRead = func(ff *fakeFiles, p string) {
				if p == "/config/templates/00-existing.yaml" { // after its directory was listed
					ff.files["/config/templates/new.yaml"] = "[]\n"
				}
			}
		}, yamlChanged},
		"a directory on the way renamed into place": {func(ff *fakeFiles) {
			ff.files["/config/configuration.yaml"] += "climate 2: !include heating/climate.yaml\n"
			ff.files["/config/heating/climate.yaml"] = "[]\n"
			ff.changed["/config/heating"] = yamlTestAfter // its files keep their ctime
		}, yamlChanged},
		"a name the Relay would decode": {func(ff *fakeFiles) { ff.files["/config/templates/%2Eheater.yaml"] = "[]\n" }, yamlUnread},
		"a walk of /config itself":      {conf("template 2: !include_dir_merge_list .\n"), yamlUnread},
		"a template key split by a tab": {conf("\"template\\tgarage\": []\n"), yamlUnread},
		"a walked dir without an mtime": {func(ff *fakeFiles) { ff.modified["/config/templates"] = "" }, yamlChanged},
		"a listing without entries":     {func(ff *fakeFiles) { ff.bare = "/config/templates" }, yamlUnread},
		"includes nested past the cap through mappings": {func(ff *fakeFiles) {
			ff.files["/config/configuration.yaml"] += "climate 2: !include n0.yaml\n"
			ff.files["/config/n0.yaml"] = "[{platform: generic_thermostat, heater: switch.a, x: !include n1.yaml}]\n"
			for i := 1; i <= yamlMaxDepth+1; i++ {
				ff.files[fmt.Sprintf("/config/n%d.yaml", i)] = fmt.Sprintf("{x: !include n%d.yaml}\n", i+1)
			}
			ff.files[fmt.Sprintf("/config/n%d.yaml", yamlMaxDepth+2)] = "{}\n"
		}, yamlUnread},
		"too many files": {func(ff *fakeFiles) {
			for i := range yamlMaxFiles {
				ff.files[fmt.Sprintf("/config/templates/f%03d.yaml", i)] = "[]\n"
			}
		}, yamlUnread},
	} {
		t.Run(name, func(t *testing.T) {
			ff := newFakeFiles(mar245Files())
			tc.change(ff)
			if proj := readYAMLProjection(t, ff); proj.Failure != tc.want {
				t.Fatalf("projection %+v, want failure %q", proj, tc.want)
			}
		})
	}
}

// Template switches the engine must not bound: they still count, flagged.
func TestYAMLTemplateSwitchShapes(t *testing.T) {
	for name, tc := range map[string]struct {
		heater     string // the switch entry under a plain block
		count      int
		plain      bool
		unreadable bool
	}{
		"an entity_id at the step": {"unique_id: h\n    turn_on: {action: switch.turn_on, entity_id: [switch.p, switch.q]}", 1, true, false},
		"a data key":               {"unique_id: h\n    turn_on: {action: switch.turn_on, target: {entity_id: switch.p}, data: {x: 1}}", 1, false, false},
		"a templated target":       {"unique_id: h\n    turn_on: {action: switch.turn_on, target: {entity_id: \"{{ x }}\"}}", 1, false, false},
		"a cover call":             {"unique_id: h\n    turn_on: {action: cover.open_cover, target: {entity_id: cover.garage_door}}", 1, false, false},
		"an area target":           {"unique_id: h\n    turn_on: {action: switch.turn_on, target: {area_id: bath}}", 1, false, false},
		"a delay":                  {"unique_id: h\n    turn_on: [{delay: 5}]", 1, false, false},
		"an integer unique id":     {"unique_id: 12\n    turn_on: []", 0, false, true},
		"no unique id":             {"name: x\n    turn_on: {action: cover.open_cover, target: {entity_id: cover.garage_door}}", 0, false, false},
		"a blueprint switch":       {"use_blueprint: {path: heater.yaml}\n    unique_id: h", 0, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			files := mar245Files()
			files["/config/templates/zz.yaml"] = "- switch:\n  - " + tc.heater + "\n"
			proj := readYAMLProjection(t, newFakeFiles(files))
			sw := proj.Switches["h"]
			if proj.Failure != "" || sw.Count != tc.count || sw.Plain != tc.plain || proj.Unreadable != tc.unreadable {
				t.Fatalf("projection %+v", proj)
			}
		})
	}
	t.Run("a block prefix, a trigger block, and a legacy duplicate", func(t *testing.T) {
		files := mar245Files()
		files["/config/templates/zz.yaml"] = "- unique_id: blk\n  switch:\n    - {unique_id: h, turn_on: []}\n- triggers: [{trigger: time, at: '07:00'}]\n  switch:\n    - {unique_id: t, turn_on: []}\n"
		files["/config/switch.yaml"] = "- platform: template\n  switches:\n    legacy:\n      unique_id: bath_heater_safe\n      turn_on: {action: cover.open_cover, target: {entity_id: cover.garage_door}}\n"
		proj := readYAMLProjection(t, newFakeFiles(files))
		if proj.Switches["blk-h"].Count != 1 || !proj.Switches["blk-h"].Plain || proj.Switches["t"].Plain || proj.Switches["bath_heater_safe"].Count != 2 {
			t.Fatalf("projection %+v", proj)
		}
	})
}
