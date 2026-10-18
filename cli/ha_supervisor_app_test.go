package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeSupervisor is Home Assistant's WebSocket with the hassio component and
// a Supervisor that holds at most the NOVA App. It records every call.
type fakeSupervisor struct {
	t           *testing.T
	noHassio    bool
	repos       []map[string]string
	info        map[string]any // the App as /addons/<slug>/info returns it
	jobErr      string         // the install/update job fails with this message
	jobNever    bool           // the job never finishes
	startFails  bool
	addFails    bool
	targetBuild bool   // the update target would need a local build
	reloadTo    string // a store reload makes this version available
	calls       []string
}

const fakeJobID = "0123456789abcdef0123456789abcdef"

func newFakeSupervisor(t *testing.T) *fakeSupervisor {
	return &fakeSupervisor{t: t, info: map[string]any{
		"name": haNovaAppName, "slug": HAOfficialNOVAAppSlug, "repository": "2368fcfa",
		"state": "unknown", "version": nil, "version_latest": "0.10.0",
		"update_available": false, "build": false, "available": true,
	}}
}

func (f *fakeSupervisor) installedApp(version, state string, updateTo string) {
	f.repos = []map[string]string{{"slug": "2368fcfa", "source": haNovaRepositoryURL}}
	f.info["version"], f.info["state"] = version, state
	if updateTo != "" {
		f.info["version_latest"], f.info["update_available"] = updateTo, true
	}
}

func (f *fakeSupervisor) writes() []string {
	var writes []string
	for _, call := range f.calls {
		if strings.HasPrefix(call, "post ") {
			writes = append(writes, call)
		}
	}
	return writes
}

func (f *fakeSupervisor) Call(ctx context.Context, commandType string, fields map[string]any, result any) error {
	reply := func(value any) error {
		data, _ := json.Marshal(value)
		return json.Unmarshal(data, result)
	}
	if commandType == "get_config" {
		f.calls = append(f.calls, "get_config")
		components := []string{"http", "hassio"}
		if f.noHassio {
			components = []string{"http"}
		}
		return reply(map[string]any{"components": components})
	}
	if commandType == "auth/current_user" {
		return nil // the readiness wait keeps the socket busy
	}
	if commandType != "supervisor/api" {
		f.t.Fatalf("unexpected WebSocket command %s", commandType)
	}
	method, endpoint := fields["method"].(string), fields["endpoint"].(string)
	f.calls = append(f.calls, method+" "+endpoint)
	fail := newCloudError(CloudErrHAProtocol, "execute Home Assistant WebSocket command", nil)
	slug := HAOfficialNOVAAppSlug
	switch method + " " + endpoint {
	case "get /store/repositories":
		return reply(f.repos)
	case "post /store/repositories":
		if f.addFails {
			return fail
		}
		f.repos = append(f.repos, map[string]string{"slug": "2368fcfa", "source": fields["data"].(map[string]any)["repository"].(string)})
		return nil
	case "get /addons/" + slug + "/info":
		return reply(f.info)
	case "post /store/addons/" + slug + "/install", "post /store/addons/" + slug + "/update":
		if f.jobErr == "" && !f.jobNever {
			f.info["version"], f.info["update_available"] = f.info["version_latest"], false
			if f.info["state"] == "unknown" {
				f.info["state"] = "stopped"
			}
		}
		return reply(map[string]string{"job_id": fakeJobID})
	case "get /jobs/" + fakeJobID:
		job := map[string]any{"done": !f.jobNever, "errors": []any{}, "child_jobs": []any{}}
		if f.jobErr != "" {
			job["child_jobs"] = []any{map[string]any{"done": true, "errors": []any{map[string]string{"message": f.jobErr}}}}
		}
		return reply(job)
	case "get /store/addons/" + slug:
		target := map[string]any{}
		for key, value := range f.info {
			target[key] = value
		}
		target["build"] = f.targetBuild
		return reply(target)
	case "post /store/reload":
		if f.reloadTo != "" {
			f.info["version_latest"], f.info["update_available"] = f.reloadTo, true
		}
		return nil
	case "post /addons/" + slug + "/start":
		if f.startFails {
			return fail
		}
		f.info["state"] = "started"
		return nil
	}
	f.t.Fatalf("the fake Supervisor got a call the allow-list should have refused: %s %s", method, endpoint)
	return nil
}

func ensureWithFake(t *testing.T, f *fakeSupervisor, update bool) (novaAppState, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var state novaAppState
	err := newNOVAAppSupervisor(f).ensure(ctx, io.Discard, &state, update, "")
	return state, err
}

func TestNOVAAppSlugDerivesFromThePinnedRepository(t *testing.T) {
	if got := newNOVAAppSupervisor(nil).appSlug; got != HAOfficialNOVAAppSlug || haNovaRelayAppSlug != HAOfficialNOVAAppSlug {
		t.Fatalf("derived slug %q, pinned %q", got, HAOfficialNOVAAppSlug)
	}
}

func TestNOVAAppSupervisorRefusesEverythingOffTheAllowList(t *testing.T) {
	f := newFakeSupervisor(t)
	s := newNOVAAppSupervisor(f)
	other := "2368fcfa_other"
	for _, refused := range []struct {
		method, endpoint string
		data             map[string]any
	}{
		{"post", "/store/repositories", map[string]any{"repository": "https://github.com/evil/ha-nova"}},
		{"post", "/store/repositories", map[string]any{"repository": haNovaRepositoryURL + ".git"}},
		{"post", "/store/repositories", map[string]any{"repository": haNovaRepositoryURL, "x": 1}},
		{"post", "/store/addons/" + other + "/install", map[string]any{"background": true}},
		{"post", "/store/addons/local_ha_nova_relay/install", map[string]any{"background": true}},
		{"post", "/store/addons/" + HAOfficialNOVAAppSlug + "/install", nil},
		{"post", "/store/addons/" + HAOfficialNOVAAppSlug + "/install/0.9.0", map[string]any{"background": true}},
		{"post", "/addons/" + HAOfficialNOVAAppSlug + "/uninstall", nil},
		{"post", "/addons/" + HAOfficialNOVAAppSlug + "/options", map[string]any{"options": map[string]any{}}},
		{"post", "/addons/" + HAOfficialNOVAAppSlug + "/stop", nil},
		{"post", "/addons/" + other + "/start", nil},
		{"get", "/addons/" + other + "/info", nil},
		{"get", "/addons/" + HAOfficialNOVAAppSlug + "/logs", nil},
		{"delete", "/store/repositories/2368fcfa", nil},
		{"get", "/jobs/info", nil},
		{"get", "/jobs/" + fakeJobID + "/../../host/reboot", nil},
		{"post", "/host/reboot", nil},
		{"post", "/backups/new/full", nil},
		{"GET", "/store/repositories", nil},
	} {
		err := s.call(context.Background(), refused.method, refused.endpoint, refused.data, time.Second, nil)
		if !errors.Is(err, errAppInstallRefusedCall) {
			t.Errorf("%s %s %v: err = %v, want refused", refused.method, refused.endpoint, refused.data, err)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("refused calls reached Home Assistant: %v", f.calls)
	}
}

func TestEnsureNOVAAppInstallsFromThePinnedSourceAndStarts(t *testing.T) {
	f := newFakeSupervisor(t)
	state, err := ensureWithFake(t, f, false)
	if err != nil {
		t.Fatal(err)
	}
	slug := HAOfficialNOVAAppSlug
	want := []string{"post /store/repositories", "post /store/addons/" + slug + "/install", "post /addons/" + slug + "/start"}
	if got := f.writes(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("writes = %v, want %v", got, want)
	}
	if !state.running || state.version != "0.10.0" || state.busy != "" {
		t.Fatalf("state = %+v", state)
	}
}

func TestEnsureNOVAAppLeavesARunningCurrentAppAlone(t *testing.T) {
	f := newFakeSupervisor(t)
	f.installedApp("0.10.0", "started", "")
	if _, err := ensureWithFake(t, f, false); err != nil || len(f.writes()) != 0 {
		t.Fatalf("err = %v, writes = %v", err, f.writes())
	}
}

func TestEnsureNOVAAppUpdatesAnOutdatedApp(t *testing.T) {
	f := newFakeSupervisor(t)
	f.installedApp("0.9.2", "started", "0.10.0")
	state, err := ensureWithFake(t, f, true)
	if err != nil || state.version != "0.10.0" {
		t.Fatalf("err = %v, state = %+v", err, state)
	}
	if got := f.writes(); len(got) != 1 || got[0] != "post /store/addons/"+HAOfficialNOVAAppSlug+"/update" {
		t.Fatalf("writes = %v, want only the update", got)
	}

	// Asked to update, but Home Assistant has nothing newer: say so, change nothing.
	f = newFakeSupervisor(t)
	f.installedApp("0.9.2", "started", "")
	_, err = ensureWithFake(t, f, true)
	var failure *novaAppInstallError
	if !errors.As(err, &failure) || !strings.Contains(failure.line, "doesn't list a newer") ||
		strings.Join(f.writes(), ",") != "post /store/reload" {
		t.Fatalf("err = %v, writes = %v", err, f.writes())
	}
}

func TestEnsureNOVAAppRefusesAnythingButTheOfficialApp(t *testing.T) {
	for name, setup := range map[string]func(*fakeSupervisor){
		"lookalike source": func(f *fakeSupervisor) {
			f.repos = []map[string]string{{"slug": "9f1c0a77", "source": haNovaRepositoryURL + ".git"}}
		},
		"source case": func(f *fakeSupervisor) {
			f.repos = []map[string]string{{"slug": "aa11bb22", "source": "https://GitHub.com/markusleben/ha-nova/"}}
		},
		"slug taken": func(f *fakeSupervisor) {
			f.repos = []map[string]string{{"slug": "2368fcfa", "source": "https://example.com/other"}}
		},
		"other App name":     func(f *fakeSupervisor) { f.info["name"] = "NOVA Relay (fork)" },
		"other repository":   func(f *fakeSupervisor) { f.info["repository"] = "local" },
		"other slug":         func(f *fakeSupervisor) { f.info["slug"] = "2368fcfa_other" },
		"local build":        func(f *fakeSupervisor) { f.info["build"] = true },
		"unsupported system": func(f *fakeSupervisor) { f.info["available"] = false },
		"hostile version":    func(f *fakeSupervisor) { f.info["version_latest"] = "1.0\x1b[2J" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeSupervisor(t)
			setup(f)
			state, err := ensureWithFake(t, f, false)
			var failure *novaAppInstallError
			if !errors.As(err, &failure) || !strings.Contains(failure.line, "Nothing was installed") {
				t.Fatalf("err = %v", err)
			}
			for _, write := range f.writes() {
				if write != "post /store/repositories" {
					t.Fatalf("refused App still got %q", write)
				}
			}
			if state.installed || state.running {
				t.Fatalf("state = %+v", state)
			}
		})
	}
}

func TestEnsureNOVAAppNeedsTheSupervisor(t *testing.T) {
	f := newFakeSupervisor(t)
	f.noHassio = true
	if _, err := ensureWithFake(t, f, false); !errors.Is(err, errAppInstallNoSupervisor) ||
		strings.Join(f.calls, ",") != "get_config" {
		t.Fatalf("err = %v, calls = %v", err, f.calls)
	}
}

func TestEnsureNOVAAppFailuresSayWhatExists(t *testing.T) {
	previous := haAppJobPollInterval
	haAppJobPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { haAppJobPollInterval = previous })
	for name, test := range map[string]struct {
		setup func(*fakeSupervisor)
		line  string
		state string
	}{
		"add fails": {func(f *fakeSupervisor) { f.addFails = true },
			"couldn't add the NOVA App source", "may still be adding the NOVA App source"},
		"install job fails": {func(f *fakeSupervisor) { f.jobErr = "Can't pull image\x1b[31m: 500" },
			"couldn't install the NOVA App: Can't pull image[31m: 500", "lists the NOVA App source; the App is not installed"},
		"start fails": {func(f *fakeSupervisor) { f.startFails = true },
			"couldn't start the NOVA App", "may still be starting the NOVA App"},
		"update job fails": {func(f *fakeSupervisor) { f.installedApp("0.9.2", "stopped", "0.10.0"); f.jobErr = "No space left" },
			"couldn't update the NOVA App: No space left", "NOVA App (v0.9.2) is installed but not running"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeSupervisor(t)
			test.setup(f)
			state, err := ensureWithFake(t, f, false)
			var failure *novaAppInstallError
			if !errors.As(err, &failure) || !strings.Contains(failure.line, test.line) {
				t.Fatalf("err = %v, want %q", err, test.line)
			}
			if got := describeNOVAAppState(state); !strings.Contains(got, test.state) {
				t.Fatalf("state line %q, want %q", got, test.state)
			}
		})
	}
}

func TestEnsureNOVAAppJobTimeoutNeverClaimsNothingHappened(t *testing.T) {
	previous := haAppJobPollInterval
	haAppJobPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { haAppJobPollInterval = previous })
	f := newFakeSupervisor(t)
	f.jobNever = true
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	var state novaAppState
	err := newNOVAAppSupervisor(f).ensure(ctx, io.Discard, &state, false, "")
	var failure *novaAppInstallError
	if !errors.As(err, &failure) || !strings.Contains(failure.line, "didn't finish the install") {
		t.Fatalf("err = %v", err)
	}
	if got := describeNOVAAppState(state); got != "Home Assistant may still be installing the NOVA App." {
		t.Fatalf("state line = %q", got)
	}
}

func TestSupervisorJobErrorIsOneBoundedLine(t *testing.T) {
	job := supervisorJob{ChildJobs: []supervisorJob{{Errors: []struct {
		Message string `json:"message"`
	}{{Message: "‮" + strings.Repeat("x", 300) + "\n"}}}}}
	if got := job.firstError(); len([]rune(got)) != 201 || strings.ContainsAny(got, "\n‮") {
		t.Fatalf("line = %q", got)
	}
	if fmt.Sprint(supervisorJob{Done: true}.firstError()) != "" {
		t.Fatal("a clean job has no error")
	}
}

func TestEnsureNOVAAppUpdateRules(t *testing.T) {
	update := "post /store/addons/" + HAOfficialNOVAAppSlug + "/update"
	for name, test := range map[string]struct {
		setup   func(*fakeSupervisor)
		update  bool
		minimum string
		line    string // "" = success
		writes  []string
	}{
		"update consent never installs": {
			setup: func(*fakeSupervisor) {}, update: true,
			line: "nothing to update", writes: []string{"post /store/repositories"},
		},
		"target would build locally": {
			setup:  func(f *fakeSupervisor) { f.installedApp("0.9.2", "started", "0.10.0"); f.targetBuild = true },
			update: true, line: "can't download the NOVA App update",
		},
		"store reload finds the update": {
			setup:  func(f *fakeSupervisor) { f.installedApp("0.9.2", "started", ""); f.reloadTo = "0.10.0" },
			update: true, writes: []string{"post /store/reload", update},
		},
		"stale listed source is reloaded before install": {
			setup: func(f *fakeSupervisor) {
				f.repos = []map[string]string{{"slug": "2368fcfa", "source": haNovaRepositoryURL}}
			},
			writes: []string{"post /store/reload", "post /store/addons/" + HAOfficialNOVAAppSlug + "/install", "post /addons/" + HAOfficialNOVAAppSlug + "/start"},
		},
		"installed version below the minimum": {
			setup: func(f *fakeSupervisor) { f.info["version_latest"] = "0.9.0" }, minimum: "0.10.0",
			line: "only offers NOVA App v0.9.0; this HA NOVA needs v0.10.0",
		},
		"starting after an update is not started twice": {
			setup:  func(f *fakeSupervisor) { f.installedApp("0.9.2", "startup", "0.10.0") },
			update: true, writes: []string{update},
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeSupervisor(t)
			test.setup(f)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var state novaAppState
			err := newNOVAAppSupervisor(f).ensure(ctx, io.Discard, &state, test.update, test.minimum)
			var failure *novaAppInstallError
			switch {
			case test.line == "" && err != nil:
				t.Fatalf("err = %v", err)
			case test.line != "" && (!errors.As(err, &failure) || !strings.Contains(failure.line, test.line)):
				t.Fatalf("err = %v, want %q", err, test.line)
			}
			if test.writes != nil && strings.Join(f.writes(), ",") != strings.Join(test.writes, ",") {
				t.Fatalf("writes = %v, want %v", f.writes(), test.writes)
			}
			if test.line != "" && test.writes == nil {
				for _, write := range f.writes() {
					if write == update {
						t.Fatalf("refused update still sent: %v", f.writes())
					}
				}
			}
		})
	}
}

// A write whose answer was lost keeps its outcome unknown: Supervisor
// shields add, start, and background jobs and keeps going without us.
func TestEnsureNOVAAppLostAnswersStayUnknown(t *testing.T) {
	f := newFakeSupervisor(t)
	f.startFails = true
	f.info["state"] = "stopped"
	f.info["version"] = "0.10.0"
	f.repos = []map[string]string{{"slug": "2368fcfa", "source": haNovaRepositoryURL}}
	state, err := ensureWithFake(t, f, false)
	if err == nil || describeNOVAAppState(state) != "Home Assistant may still be starting the NOVA App." {
		t.Fatalf("err = %v, state line %q", err, describeNOVAAppState(state))
	}
}
