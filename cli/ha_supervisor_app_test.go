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
	t                   *testing.T
	noHassio            bool
	repos               []map[string]string
	info                map[string]any // the App as /addons/<slug>/info returns it
	jobErr              string         // the install/update job fails with this message
	jobNever            bool           // the job never finishes
	startFails          bool
	addFails            bool
	uninstallFails      bool // the App uninstall fails and the App stays
	deleteRepoFails     bool
	storeOmitsInstalled bool
	calls               []string
}

const fakeJobID = "0123456789abcdef0123456789abcdef"

func newFakeSupervisor(t *testing.T) *fakeSupervisor {
	return &fakeSupervisor{t: t, info: map[string]any{
		"name": haNovaAppName, "slug": HAOfficialNOVAAppSlug, "repository": "2368fcfa",
		"state": "unknown", "version": nil, "version_latest": "0.10.0",
		"update_available": false, "build": false, "available": true,
		"network": map[string]any{"8791/tcp": 8791, "8792/tcp": 8792, "8793/tcp": nil},
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
	case "post /store/addons/" + slug + "/install":
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
		target["installed"] = f.info["version"] != nil
		if f.storeOmitsInstalled {
			delete(target, "installed")
		}
		return reply(target)
	case "post /store/reload":
		return nil
	case "post /addons/" + slug + "/uninstall":
		if f.uninstallFails {
			return fail
		}
		f.info["version"], f.info["state"] = nil, "unknown"
		return nil
	case "delete /store/repositories/2368fcfa":
		if f.deleteRepoFails {
			return fail
		}
		f.repos = nil
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

func ensureWithFake(t *testing.T, f *fakeSupervisor) (novaAppState, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var state novaAppState
	err := newNOVAAppSupervisor(f).ensure(ctx, io.Discard, &state)
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
		{"post", "/store/addons/" + HAOfficialNOVAAppSlug + "/update", map[string]any{"background": true}},
		{"post", "/store/addons/" + HAOfficialNOVAAppSlug + "/update", map[string]any{"background": true, "backup": true}},
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
	state, err := ensureWithFake(t, f)
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
	if _, err := ensureWithFake(t, f); err != nil || len(f.writes()) != 0 {
		t.Fatalf("err = %v, writes = %v", err, f.writes())
	}
}

// The update job and its store reload change the App's update entity, which
// an automation can react to (a state trigger on it that runs lock.unlock).
// This sign-in can't classify them like `ha-nova call`, so it never sends
// them to an installed App: an outdated one stays as it is, and is only
// started when stopped (#1017).
func TestEnsureNOVAAppNeverUpdatesAnInstalledApp(t *testing.T) {
	start := "post /addons/" + HAOfficialNOVAAppSlug + "/start"
	for name, test := range map[string]struct {
		state, updateTo string
		writes          []string
	}{
		"running, update offered":  {"started", "0.10.0", nil},
		"running, no update yet":   {"started", "", nil},
		"starting, update offered": {"startup", "0.10.0", nil},
		"stopped, update offered":  {"stopped", "0.10.0", []string{start}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeSupervisor(t)
			f.installedApp("0.9.2", test.state, test.updateTo)
			state, err := ensureWithFake(t, f)
			if err != nil || state.version != "0.9.2" || !state.running {
				t.Fatalf("err = %v, state = %+v", err, state)
			}
			if got := f.writes(); strings.Join(got, ",") != strings.Join(test.writes, ",") {
				t.Fatalf("writes = %v, want %v", got, test.writes)
			}
		})
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
			state, err := ensureWithFake(t, f)
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
	if _, err := ensureWithFake(t, f); !errors.Is(err, errAppInstallNoSupervisor) ||
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
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeSupervisor(t)
			test.setup(f)
			state, err := ensureWithFake(t, f)
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
	err := newNOVAAppSupervisor(f).ensure(ctx, io.Discard, &state)
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

// A source listed before this run may not know the current version yet:
// like "Check for updates", the store reloads before a first install only.
func TestEnsureNOVAAppReloadsAStaleSourceBeforeAFirstInstall(t *testing.T) {
	f := newFakeSupervisor(t)
	f.repos = []map[string]string{{"slug": "2368fcfa", "source": haNovaRepositoryURL}}
	if _, err := ensureWithFake(t, f); err != nil {
		t.Fatal(err)
	}
	want := []string{"post /store/reload", "post /store/addons/" + HAOfficialNOVAAppSlug + "/install", "post /addons/" + HAOfficialNOVAAppSlug + "/start"}
	if got := f.writes(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("writes = %v, want %v", got, want)
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
	state, err := ensureWithFake(t, f)
	if err == nil || describeNOVAAppState(state) != "Home Assistant may still be starting the NOVA App." {
		t.Fatalf("err = %v, state line %q", err, describeNOVAAppState(state))
	}
}
