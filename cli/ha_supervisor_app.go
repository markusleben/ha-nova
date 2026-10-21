package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Installs, updates, and starts the NOVA App through the Supervisor with the
// owner's sign-in, only after the owner chose "Sign in and install NOVA"
// (docs/work/2026-10-19-setup-app-install-spec.md). Every Supervisor
// call passes allowed(); anything else is refused before it leaves this process.

const haNovaAppName = "NOVA Relay" // nova/config.yaml name

// Budgets; variables so tests can shrink them.
var (
	haAppInstallTimeout  = 10 * time.Minute
	haAppReadyTimeout    = 2 * time.Minute
	haAppJobPollInterval = 2 * time.Second
	haAppReadyPoll       = 2 * time.Second
)

var (
	errAppInstallRefusedCall    = errors.New("Supervisor call outside the NOVA App allow-list")
	errAppInstallNoSupervisor   = errors.New("Home Assistant has no Supervisor")
	errAppInstallSourceMismatch = errors.New("Home Assistant lists a different NOVA source")
	errAppInstallUnsupported    = errors.New("Home Assistant can't run the NOVA App on this system")
)

// novaAppInstallError carries one plain line for the user.
type novaAppInstallError struct {
	line  string
	cause error
}

func (e *novaAppInstallError) Error() string { return e.line }
func (e *novaAppInstallError) Unwrap() error { return e.cause }

// novaAppState is what Home Assistant verifiably holds, so a failure can say
// what exists. busy names a change whose outcome is unknown.
type novaAppState struct {
	sourceListed bool
	installed    bool
	running      bool
	version      string
	busy         string
	known        bool // installed/running/version come from an App read
}

type haWSCaller interface {
	Call(ctx context.Context, commandType string, fields map[string]any, result any) error
}

type novaAppSupervisor struct {
	ws       haWSCaller
	repoSlug string
	appSlug  string
	// removal: uninstall only (uninstall_app_removal.go); the install calls
	// are then refused, and without it the removal calls are.
	removal bool
	// phase, when set, hears each change as it starts: "add", "install",
	// "update", "start" (the setup page's progress rows).
	phase func(name string)
}

func (s novaAppSupervisor) report(name string) {
	if s.phase != nil {
		s.phase(name)
	}
}

// Supervisor names a repository by the first 8 hex digits of the SHA-1 of its
// lowercased URL (supervisor/store/utils.py get_hash_from_repository), and
// prefixes each App slug with it.
func newNOVAAppSupervisor(ws haWSCaller) novaAppSupervisor {
	sum := sha1.Sum([]byte(strings.ToLower(haNovaRepositoryURL)))
	repo := hex.EncodeToString(sum[:])[:8]
	return novaAppSupervisor{ws: ws, repoSlug: repo, appSlug: repo + "_ha_nova_relay"}
}

var (
	supervisorJobIDPattern   = regexp.MustCompile(`^[a-f0-9]{32}$`)
	supervisorVersionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$`)
)

// allowed is the complete list of Supervisor calls this flow may make.
func (s novaAppSupervisor) allowed(method, endpoint string, data map[string]any) bool {
	switch method + " " + endpoint {
	case "get /store/repositories", "get /addons/" + s.appSlug + "/info", "get /store/addons/" + s.appSlug:
		return data == nil
	case "post /addons/" + s.appSlug + "/uninstall", "delete /store/repositories/" + s.repoSlug:
		// No remove_config: the App's own config folder, if any, stays.
		return s.removal && data == nil
	case "post /addons/" + s.appSlug + "/start", "post /store/reload":
		return !s.removal && data == nil
	case "post /store/repositories":
		return !s.removal && len(data) == 1 && data["repository"] == haNovaRepositoryURL
	case "post /store/addons/" + s.appSlug + "/install":
		return !s.removal && len(data) == 1 && data["background"] == true
	case "post /store/addons/" + s.appSlug + "/update": // always with a backup of the App first
		return !s.removal && len(data) == 2 && data["background"] == true && data["backup"] == true
	}
	id, isJob := strings.CutPrefix(endpoint, "/jobs/")
	return !s.removal && method == "get" && data == nil && isJob && supervisorJobIDPattern.MatchString(id)
}

func (s novaAppSupervisor) call(ctx context.Context, method, endpoint string, data map[string]any, timeout time.Duration, result any) error {
	if !s.allowed(method, endpoint, data) {
		line := "HA NOVA stopped: the next step was not part of installing its App."
		if s.removal {
			line = "HA NOVA stopped: the next step was not part of removing its App."
		}
		return &novaAppInstallError{
			line:  line,
			cause: fmt.Errorf("%w: %s %s", errAppInstallRefusedCall, method, endpoint),
		}
	}
	fields := map[string]any{"endpoint": endpoint, "method": method, "timeout": int(timeout / time.Second)}
	if data != nil {
		fields["data"] = data
	}
	return s.ws.Call(ctx, "supervisor/api", fields, result)
}

type supervisorAppInfo struct {
	Name            string `json:"name"`
	Slug            string `json:"slug"`
	Repository      string `json:"repository"`
	State           string `json:"state"`
	Version         string `json:"version"`
	VersionLatest   string `json:"version_latest"`
	UpdateAvailable bool   `json:"update_available"`
	Build           bool   `json:"build"`
	Available       bool   `json:"available"`
	Installed       *bool  `json:"installed"` // store entries only
	// Network maps container ports to host ports ("8792/tcp": 8792; null
	// when disabled).
	Network map[string]*int `json:"network"`
}

const novaAppSecurePortKey = "8792/tcp" // nova/config.yaml ports

// info reads the App and checks it is the one from the pinned source.
func (s novaAppSupervisor) info(ctx context.Context) (supervisorAppInfo, error) {
	var info supervisorAppInfo
	if err := s.call(ctx, "get", "/addons/"+s.appSlug+"/info", nil, 10*time.Second, &info); err != nil {
		return info, err
	}
	if info.Slug != s.appSlug || info.Repository != s.repoSlug || info.Name != haNovaAppName ||
		(info.Version != "" && !supervisorVersionPattern.MatchString(info.Version)) ||
		(info.VersionLatest != "" && !supervisorVersionPattern.MatchString(info.VersionLatest)) {
		return info, &novaAppInstallError{line: "Home Assistant lists a NOVA App that doesn't match the official one. " + s.nothingChanged(), cause: errAppInstallSourceMismatch}
	}
	return info, nil
}

func (s novaAppSupervisor) nothingChanged() string {
	if s.removal {
		return "Nothing was removed."
	}
	return "Nothing was installed."
}

// sourceListed reports whether Home Assistant lists the official repository
// under its own slug; a lookalike source is refused.
func (s novaAppSupervisor) sourceListed(ctx context.Context) (bool, error) {
	var repositories []struct {
		Slug   string `json:"slug"`
		Source string `json:"source"`
	}
	if err := s.call(ctx, "get", "/store/repositories", nil, 10*time.Second, &repositories); err != nil {
		return false, err
	}
	normalize := func(source string) string {
		return strings.TrimSuffix(strings.TrimRight(strings.ToLower(strings.TrimSpace(source)), "/"), ".git")
	}
	for _, repository := range repositories {
		if repository.Source == haNovaRepositoryURL && repository.Slug == s.repoSlug {
			return true, nil
		}
		if repository.Slug == s.repoSlug || normalize(repository.Source) == normalize(haNovaRepositoryURL) {
			return false, &novaAppInstallError{line: "Home Assistant lists a different NOVA source. " + s.nothingChanged(), cause: errAppInstallSourceMismatch}
		}
	}
	return false, nil
}

// ensure brings the App to installed, current (when update is set or
// Supervisor offers an update), at least minimum (min_relay_version, "" for
// none), and started. It prints one line per step.
func (s novaAppSupervisor) ensure(ctx context.Context, out io.Writer, state *novaAppState, update bool, minimum string) error {
	var config struct {
		Components []string `json:"components"`
	}
	if err := s.ws.Call(ctx, "get_config", nil, &config); err != nil {
		return err
	}
	if !slices.Contains(config.Components, "hassio") {
		return errAppInstallNoSupervisor
	}
	listed, err := s.sourceListed(ctx)
	if err != nil {
		return err
	}
	if !listed {
		state.busy = "adding the NOVA App source"
		s.report("add")
		_, err := runSetupSpinnerWithResult(out, "Adding the NOVA App source to Home Assistant...", setupSpinnerDebounce, func() (struct{}, error) {
			return struct{}{}, s.call(ctx, "post", "/store/repositories", map[string]any{"repository": haNovaRepositoryURL}, 2*time.Minute, nil)
		})
		listed, listErr := s.sourceListed(ctx)
		if !listed {
			if err == nil && listErr == nil {
				state.busy = "" // Supervisor answered; its add may still run otherwise
			}
			return &novaAppInstallError{line: "Home Assistant couldn't add the NOVA App source.", cause: errors.Join(err, listErr)}
		}
		renderSetupSuccessLine(out, "NOVA App source added to Home Assistant")
	}
	state.busy, state.sourceListed = "", true
	info, err := s.info(ctx)
	if err != nil {
		return err
	}
	state.observe(info)
	if update && !state.installed {
		return &novaAppInstallError{line: "Home Assistant doesn't list the NOVA App as installed, so there is nothing to update. Nothing was installed."}
	}
	if (update && !info.UpdateAvailable) || (!state.installed && listed) {
		// Like "Check for updates" in Home Assistant: a source listed
		// earlier may not know the current version yet.
		_, _ = runSetupSpinnerWithResult(out, "Checking Home Assistant for a newer NOVA App...", setupSpinnerDebounce, func() (struct{}, error) {
			return struct{}{}, s.call(ctx, "post", "/store/reload", nil, 2*time.Minute, nil)
		})
		if info, err = s.info(ctx); err != nil {
			return err
		}
		state.observe(info)
	}
	switch {
	case !state.installed:
		if !info.Available {
			return &novaAppInstallError{line: "Home Assistant says this system can't run the NOVA App. Nothing was installed.", cause: errAppInstallUnsupported}
		}
		if info.Build {
			// A local build would take long and run code no image check saw.
			return &novaAppInstallError{line: "Home Assistant would have to build the NOVA App on this system instead of downloading it. Nothing was installed."}
		}
		s.report("install")
		if err := s.job(ctx, out, state, "install", "installing the NOVA App",
			"Installing the NOVA App (download, can take a few minutes)..."); err != nil {
			return err
		}
		renderSetupSuccessLine(out, "NOVA App installed (v%s)", state.version)
	case info.UpdateAvailable:
		// The installed manifest's build flag says nothing about the update
		// target: check the store entry Supervisor would update to.
		var target supervisorAppInfo
		if err := s.call(ctx, "get", "/store/addons/"+s.appSlug, nil, 10*time.Second, &target); err != nil {
			return &novaAppInstallError{line: "Home Assistant couldn't show the NOVA App update.", cause: err}
		}
		if target.Slug != s.appSlug || target.Repository != s.repoSlug || target.Name != haNovaAppName ||
			target.Build || !target.Available || !supervisorVersionPattern.MatchString(target.VersionLatest) {
			return &novaAppInstallError{line: "Home Assistant can't download the NOVA App update for this system. Nothing was updated."}
		}
		s.report("update")
		if err := s.job(ctx, out, state, "update", "updating the NOVA App",
			fmt.Sprintf("Updating the NOVA App to v%s...", target.VersionLatest)); err != nil {
			return err
		}
		renderSetupSuccessLine(out, "NOVA App updated (v%s)", state.version)
	case update:
		return &novaAppInstallError{line: "Home Assistant doesn't list a newer NOVA App yet."}
	}
	if minimum != "" {
		cmp, err := compareReleaseVersions(state.version, minimum)
		if err != nil {
			return &novaAppInstallError{line: fmt.Sprintf("HA NOVA can't tell whether NOVA App v%s is new enough (it needs v%s or newer).", state.version, minimum), cause: err}
		}
		if cmp < 0 {
			return &novaAppInstallError{line: fmt.Sprintf("Home Assistant only offers NOVA App v%s; this HA NOVA needs v%s or newer.", state.version, minimum)}
		}
	}
	if state.running {
		return nil
	}
	state.busy = "starting the NOVA App"
	s.report("start")
	_, err = runSetupCountdownSpinnerWithResult(out, "Starting the NOVA App...", 3*time.Minute, setupSpinnerDebounce, func() (struct{}, error) {
		return struct{}{}, s.call(ctx, "post", "/addons/"+s.appSlug+"/start", nil, 3*time.Minute, nil)
	})
	info, infoErr := s.info(ctx)
	if infoErr == nil {
		state.observe(info)
	}
	if !state.running {
		if err == nil && infoErr == nil {
			state.busy = "" // a start Supervisor reports done, yet the App is not running
		}
		return &novaAppInstallError{line: "Home Assistant couldn't start the NOVA App.", cause: errors.Join(err, infoErr)}
	}
	state.busy = ""
	renderSetupSuccessLine(out, "NOVA App started")
	return nil
}

func (state *novaAppState) observe(info supervisorAppInfo) {
	state.known = true
	state.installed = info.Version != ""
	state.version = info.Version
	// "startup": Supervisor is starting it (e.g. after an update); the
	// readiness wait covers the rest.
	state.running = info.State == "started" || info.State == "startup"
}

// job runs install or update in the background and waits for it. The App
// info decides success; until Supervisor reports the job done, its outcome
// stays unknown (busy), because Supervisor keeps going without us.
func (s novaAppSupervisor) job(ctx context.Context, out io.Writer, state *novaAppState, action, busy, label string) error {
	before := state.version
	reached := func(info supervisorAppInfo) bool {
		return info.Version != "" && (action == "install" || (info.Version != before && !info.UpdateAvailable))
	}
	state.busy = busy
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(haAppInstallTimeout)
	}
	type jobResult struct {
		done   bool
		reason string
	}
	result, err := runSetupCountdownSpinnerWithResult(out, label, time.Until(deadline), setupSpinnerDebounce, func() (jobResult, error) {
		var started struct {
			JobID string `json:"job_id"`
		}
		data := map[string]any{"background": true}
		if action == "update" { // Home Assistant saves a backup of the App first
			data["backup"] = true
		}
		if err := s.call(ctx, "post", "/store/addons/"+s.appSlug+"/"+action, data, time.Minute, &started); err != nil {
			return jobResult{}, err
		}
		if started.JobID == "" { // Supervisor finished within the call
			return jobResult{done: true}, nil
		}
		for {
			var job supervisorJob
			err := s.call(ctx, "get", "/jobs/"+started.JobID, nil, 10*time.Second, &job)
			switch {
			case errors.Is(err, errAppInstallRefusedCall):
				return jobResult{}, err
			case err == nil && job.Done:
				return jobResult{done: true, reason: job.firstError()}, nil
			case err != nil: // a slow or vanished job read: the App itself may say
				if info, infoErr := s.info(ctx); infoErr == nil && reached(info) {
					return jobResult{done: true}, nil
				}
			}
			select {
			case <-ctx.Done():
				return jobResult{}, ctx.Err()
			case <-time.After(haAppJobPollInterval):
			}
		}
	})
	info, infoErr := s.info(ctx)
	if infoErr == nil {
		state.observe(info)
		if reached(info) {
			state.busy = ""
			return nil
		}
		if result.done {
			state.busy = "" // finished without the change
		}
	}
	line := fmt.Sprintf("Home Assistant didn't confirm the %s of the NOVA App.", action)
	switch {
	case result.reason != "":
		line = fmt.Sprintf("Home Assistant couldn't %s the NOVA App: %s", action, result.reason)
	case result.done:
		line = fmt.Sprintf("Home Assistant couldn't %s the NOVA App.", action)
	case errors.Is(err, context.DeadlineExceeded):
		line = fmt.Sprintf("Home Assistant didn't finish the %s within %d minutes.", action, int(haAppInstallTimeout/time.Minute))
	}
	return &novaAppInstallError{line: line, cause: errors.Join(err, infoErr)}
}

type supervisorJob struct {
	Done   bool `json:"done"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
	ChildJobs []supervisorJob `json:"child_jobs"`
}

// firstError returns the first Supervisor job error as one safe line.
func (job supervisorJob) firstError() string {
	for _, failure := range job.Errors {
		if message := terminalSafeName(failure.Message); message != "" {
			if runes := []rune(message); len(runes) > 200 {
				message = string(runes[:200]) + "…"
			}
			return message
		}
	}
	for _, child := range job.ChildJobs {
		if message := child.firstError(); message != "" {
			return message
		}
	}
	return ""
}
