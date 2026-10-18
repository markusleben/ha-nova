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
// (docs/work/2026-10-18-onboarding-0.27-spec.md §6, §7.1). Every Supervisor
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
}

type haWSCaller interface {
	Call(ctx context.Context, commandType string, fields map[string]any, result any) error
}

type novaAppSupervisor struct {
	ws       haWSCaller
	repoSlug string
	appSlug  string
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
	case "get /store/repositories", "get /addons/" + s.appSlug + "/info", "post /addons/" + s.appSlug + "/start":
		return data == nil
	case "post /store/repositories":
		return len(data) == 1 && data["repository"] == haNovaRepositoryURL
	case "post /store/addons/" + s.appSlug + "/install", "post /store/addons/" + s.appSlug + "/update":
		return len(data) == 1 && data["background"] == true
	}
	id, isJob := strings.CutPrefix(endpoint, "/jobs/")
	return method == "get" && data == nil && isJob && supervisorJobIDPattern.MatchString(id)
}

func (s novaAppSupervisor) call(ctx context.Context, method, endpoint string, data map[string]any, timeout time.Duration, result any) error {
	if !s.allowed(method, endpoint, data) {
		return fmt.Errorf("%w: %s %s", errAppInstallRefusedCall, method, endpoint)
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
}

// info reads the App and checks it is the one from the pinned source.
func (s novaAppSupervisor) info(ctx context.Context) (supervisorAppInfo, error) {
	var info supervisorAppInfo
	if err := s.call(ctx, "get", "/addons/"+s.appSlug+"/info", nil, 10*time.Second, &info); err != nil {
		return info, err
	}
	if info.Slug != s.appSlug || info.Repository != s.repoSlug || info.Name != haNovaAppName ||
		(info.Version != "" && !supervisorVersionPattern.MatchString(info.Version)) ||
		(info.VersionLatest != "" && !supervisorVersionPattern.MatchString(info.VersionLatest)) {
		return info, &novaAppInstallError{line: "Home Assistant lists a NOVA App that doesn't match the official one. Nothing was installed.", cause: errAppInstallSourceMismatch}
	}
	return info, nil
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
			return false, &novaAppInstallError{line: "Home Assistant lists a different NOVA source. Nothing was installed.", cause: errAppInstallSourceMismatch}
		}
	}
	return false, nil
}

// ensure brings the App to installed, current (when update is set or
// Supervisor offers an update), and started. It prints one line per step.
func (s novaAppSupervisor) ensure(ctx context.Context, out io.Writer, state *novaAppState, update bool) error {
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
		_, err := runSetupSpinnerWithResult(out, "Adding the NOVA App source to Home Assistant...", setupSpinnerDebounce, func() (struct{}, error) {
			return struct{}{}, s.call(ctx, "post", "/store/repositories", map[string]any{"repository": haNovaRepositoryURL}, 2*time.Minute, nil)
		})
		listed, listErr := s.sourceListed(ctx)
		if !listed {
			if listErr == nil && !changeOutcomeUnknown(err) {
				state.busy = ""
			}
			return &novaAppInstallError{line: "Home Assistant couldn't add the NOVA App source.", cause: err}
		}
		renderSetupSuccessLine(out, "NOVA App source added to Home Assistant")
	}
	state.busy, state.sourceListed = "", true
	info, err := s.info(ctx)
	if err != nil {
		return err
	}
	state.observe(info)
	switch {
	case !state.installed:
		if info.Build || !info.Available {
			// A local build would take long and use code the image check never saw.
			return &novaAppInstallError{line: "Home Assistant can't install the prebuilt NOVA App on this system. Nothing was installed."}
		}
		if err := s.job(ctx, out, state, "install", "installing the NOVA App",
			"Installing the NOVA App (download, can take a few minutes)..."); err != nil {
			return err
		}
		renderSetupSuccessLine(out, "NOVA App installed (v%s)", state.version)
	case info.UpdateAvailable:
		if info.Build {
			return &novaAppInstallError{line: "Home Assistant can't update to a prebuilt NOVA App on this system."}
		}
		if err := s.job(ctx, out, state, "update", "updating the NOVA App",
			fmt.Sprintf("Updating the NOVA App to v%s...", info.VersionLatest)); err != nil {
			return err
		}
		renderSetupSuccessLine(out, "NOVA App updated (v%s)", state.version)
	case update:
		return &novaAppInstallError{line: "Home Assistant doesn't list a newer NOVA App yet."}
	}
	if state.running {
		return nil
	}
	state.busy = "starting the NOVA App"
	_, err = runSetupCountdownSpinnerWithResult(out, "Starting the NOVA App...", 3*time.Minute, setupSpinnerDebounce, func() (struct{}, error) {
		return struct{}{}, s.call(ctx, "post", "/addons/"+s.appSlug+"/start", nil, 3*time.Minute, nil)
	})
	info, infoErr := s.info(ctx)
	if infoErr == nil {
		state.observe(info)
	}
	if !state.running {
		if infoErr == nil && !changeOutcomeUnknown(err) {
			state.busy = ""
		}
		return &novaAppInstallError{line: "Home Assistant couldn't start the NOVA App.", cause: err}
	}
	state.busy = ""
	renderSetupSuccessLine(out, "NOVA App started")
	return nil
}

// changeOutcomeUnknown: the write may still be running in Home Assistant.
func changeOutcomeUnknown(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) ||
		IsCloudErrorCode(err, CloudErrTimeout) || IsCloudErrorCode(err, CloudErrNetwork)
}

func (state *novaAppState) observe(info supervisorAppInfo) {
	state.installed = info.Version != ""
	state.version = info.Version
	state.running = info.State == "started"
}

// job runs install or update in the background and waits for it; the App
// info afterwards decides success, the job only supplies the reason.
func (s novaAppSupervisor) job(ctx context.Context, out io.Writer, state *novaAppState, action, busy, label string) error {
	before := state.version
	state.busy = busy
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(haAppInstallTimeout)
	}
	reason, err := runSetupCountdownSpinnerWithResult(out, label, time.Until(deadline), setupSpinnerDebounce, func() (string, error) {
		var started struct {
			JobID string `json:"job_id"`
		}
		if err := s.call(ctx, "post", "/store/addons/"+s.appSlug+"/"+action, map[string]any{"background": true}, time.Minute, &started); err != nil {
			return "", err
		}
		for started.JobID != "" { // empty: Supervisor finished within the call
			var job supervisorJob
			if err := s.call(ctx, "get", "/jobs/"+started.JobID, nil, 10*time.Second, &job); err != nil {
				return "", err
			}
			if reason := job.firstError(); reason != "" || job.Done {
				return reason, nil
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(haAppJobPollInterval):
			}
		}
		return "", nil
	})
	if info, infoErr := s.info(ctx); infoErr == nil {
		state.observe(info)
		if state.installed && (action == "install" || (state.version != before && !info.UpdateAvailable)) {
			state.busy = ""
			return nil
		}
		if !changeOutcomeUnknown(err) {
			state.busy = "" // Supervisor answered and nothing changed
		}
	}
	line := fmt.Sprintf("Home Assistant couldn't %s the NOVA App.", action)
	switch {
	case reason != "":
		line = fmt.Sprintf("Home Assistant couldn't %s the NOVA App: %s", action, reason)
	case changeOutcomeUnknown(err):
		line = fmt.Sprintf("Home Assistant didn't finish the %s within %d minutes.", action, int(haAppInstallTimeout/time.Minute))
	}
	return &novaAppInstallError{line: line, cause: err}
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

// waitForNOVAAppSignIn waits until the started App serves sign-in pairing.
func waitForNOVAAppSignIn(ctx context.Context, out io.Writer, relayURL string) error {
	ready, _ := runSetupCountdownSpinnerWithResult(out, "Waiting for the NOVA App to get ready...", haAppReadyTimeout, setupSpinnerDebounce, func() (bool, error) {
		deadline := time.After(haAppReadyTimeout)
		for !relayAdvertisesSignInForSetup(relayURL) {
			select {
			case <-ctx.Done():
				return false, nil
			case <-deadline:
				return false, nil
			case <-time.After(haAppReadyPoll):
			}
		}
		return true, nil
	})
	if !ready {
		return &novaAppInstallError{line: "The NOVA App started but didn't get ready to connect computers within two minutes."}
	}
	return nil
}

var relayAdvertisesSignInForSetup = relayAdvertisesSignIn

// describeNOVAAppState says what Home Assistant holds after a failure.
func describeNOVAAppState(state novaAppState) string {
	version := ""
	if state.version != "" {
		version = " (v" + state.version + ")"
	}
	switch {
	case state.busy != "":
		return "Home Assistant may still be " + state.busy + "."
	case state.running:
		return "The NOVA App" + version + " is installed and running."
	case state.installed:
		return "The NOVA App" + version + " is installed but not running."
	case state.sourceListed:
		return "Home Assistant lists the NOVA App source; the App is not installed."
	}
	return "Nothing was changed in Home Assistant."
}
