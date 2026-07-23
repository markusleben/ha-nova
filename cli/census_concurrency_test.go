package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCensusPostDisablesTransportBodyReplay(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusEndpoint(t)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}

	originalClient := censusHTTPClient
	censusHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.GetBody != nil {
			t.Fatal("census POST body is replayable by the HTTP transport")
		}
		if request.ContentLength <= 0 {
			t.Fatalf("census POST ContentLength = %d, want a bounded non-empty body", request.ContentLength)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { censusHTTPClient = originalClient })

	if result := sendCensusPingOnce(paths); !result.Attempted || result.Err != nil {
		t.Fatalf("census send result = %+v", result)
	}
}

func TestCensusMixedConcurrentSendPathsAttemptOnce(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(path int) {
			defer wg.Done()
			<-start
			switch path % 3 {
			case 0:
				maybeCensusPing(paths)
			case 1:
				censusFirstPingAfterYes(paths)
			default:
				_ = sendCensusPingOnce(paths)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if len(*payloads) != 1 {
		t.Fatalf("24 mixed concurrent paths sent %d POSTs, want exactly 1", len(*payloads))
	}
}

func TestCensusWeekBoundaryInterleavingAttemptsAtMostOncePerClientWeek(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}

	w30 := time.Date(2026, 7, 26, 23, 59, 59, 0, time.UTC)
	w31 := time.Date(2026, 7, 27, 0, 0, 1, 0, time.UTC)
	start := make(chan struct{})
	results := make(chan censusPingResult, 4)
	for _, instant := range []time.Time{w30, w31, w31, w31} {
		instant := instant
		go func() {
			<-start
			results <- sendCensusPingOnceWithClock(paths, func() time.Time { return instant })
		}()
	}
	close(start)

	counts := map[string]int{}
	for range 4 {
		result := <-results
		if result.Attempted {
			counts[result.Week]++
		}
	}
	for week, count := range counts {
		if count > 1 {
			t.Fatalf("week %s sent %d attempts, want at most 1", week, count)
		}
	}
	if len(*payloads) != counts["2026-W30"]+counts["2026-W31"] {
		t.Fatalf("transport attempts %d do not match locked week results %v", len(*payloads), counts)
	}
}

func TestCensusLockPreparationFailureFailsClosed(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	// Unix locks the stable HOME directory; point it at a missing path. The
	// Windows named-mutex implementation has no filesystem setup failure, and
	// its contention behavior is covered by the cross-platform lock test.
	if bundlePlatformOS() == "windows" {
		t.Skip("Windows named mutex has no filesystem preparation step")
	}
	t.Setenv("HOME", filepath.Join(t.TempDir(), "missing-home"))

	result := sendCensusPingOnce(paths)
	if result.Err == nil || result.Attempted {
		t.Fatalf("lock setup failure result = %+v, want unattempted error", result)
	}
	if len(*payloads) != 0 {
		t.Fatalf("lock setup failure sent %d POSTs, want 0", len(*payloads))
	}
	if state := loadCensusState(paths); state.LastPingWeek != "" {
		t.Fatalf("lock setup failure stamped week %q", state.LastPingWeek)
	}
}

func TestCensusOffWaitsForInFlightSendThenPreventsFutureSend(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusEndpoint(t)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	releaseSend := make(chan struct{})
	var requests atomic.Int32
	originalClient := censusHTTPClient
	censusHTTPClient = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		requests.Add(1)
		close(started)
		<-releaseSend
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { censusHTTPClient = originalClient })

	sendDone := make(chan censusPingResult, 1)
	go func() { sendDone <- sendCensusPingOnce(paths) }()
	<-started
	offDone := make(chan int, 1)
	go func() { offDone <- runCensusOff(paths) }()

	select {
	case <-offDone:
		t.Fatal("census off returned while the shared send lock was still held")
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseSend)
	if result := <-sendDone; !result.Attempted || result.Err != nil {
		t.Fatalf("in-flight send result = %+v", result)
	}
	if exit := <-offDone; exit != 0 {
		t.Fatalf("census off exit = %d", exit)
	}
	if state := loadCensusState(paths); state.Enabled {
		t.Fatal("census must be disabled after off returns")
	}
	if result := sendCensusPingOnce(paths); result.Attempted {
		t.Fatalf("post-opt-out send attempted: %+v", result)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want only the in-flight request", got)
	}
}

func TestCensusPendingYesCannotOverrideSuccessfulOff(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusTTY(t, true, true)
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	answer, done := startBlockedCensusAsk(t, paths)
	exit := 0
	captureStdout(t, func() { exit = runCensusOff(paths) })
	if exit != 0 {
		t.Fatalf("census off exit = %d, want 0", exit)
	}
	out := finishBlockedCensusAsk(t, answer, done, "y\n")
	state := loadCensusState(paths)
	if state.Enabled || state.Answer != "no" {
		t.Fatalf("stale prompt yes overrode successful opt-out: %+v", state)
	}
	if len(*payloads) != 0 {
		t.Fatalf("stale prompt yes sent %d requests after opt-out", len(*payloads))
	}
	if strings.Contains(out, "Thank you") {
		t.Fatalf("stale prompt must not claim that consent was applied:\n%s", out)
	}
}

func TestCensusPendingYesCannotRecreateStateAfterUninstall(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusTTY(t, true, true)
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	answer, done := startBlockedCensusAsk(t, paths)
	if err := removeManagedConfigArtifacts(paths, &uninstallReport{}, true); err != nil {
		t.Fatalf("uninstall config cleanup: %v", err)
	}
	out := finishBlockedCensusAsk(t, answer, done, "y\n")
	if len(*payloads) != 0 {
		t.Fatalf("stale prompt sent %d requests after uninstall", len(*payloads))
	}
	if strings.Contains(out, "Thank you") {
		t.Fatalf("stale prompt must not claim that consent was applied:\n%s", out)
	}
	for _, path := range []string{paths.StateFile, paths.CensusFile, paths.ConfigDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale prompt recreated %s after uninstall (err=%v)", path, err)
		}
	}
}

func TestStaleInstallStateWriterCannotReactivateCensusAfterUninstall(t *testing.T) {
	paths := setupCensusTest(t)
	if err := removeManagedConfigArtifacts(paths, &uninstallReport{}, true); err != nil {
		t.Fatalf("uninstall config cleanup: %v", err)
	}
	// Model an update/self-heal process that loaded install state before
	// uninstall and saves it only after uninstall returned.
	if err := saveState(paths, defaultInstallState()); err != nil {
		t.Fatalf("stale install-state save: %v", err)
	}
	if err := mutateCensusState(paths, func(state *censusState) { state.Enabled = true }); err == nil {
		t.Fatal("stale state.json reactivated a census writer")
	}
	if out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) }); out != "" {
		t.Fatalf("stale state.json reactivated a census notice: %q", out)
	}
	if _, err := os.Stat(paths.CensusFile); !os.IsNotExist(err) {
		t.Fatalf("stale state writer allowed census.json recreation: %v", err)
	}
}

func TestUninstallWithoutStateStillStopsSetupStartedAgainstInstalledRuntime(t *testing.T) {
	paths := setupCensusTest(t)
	if err := os.Remove(paths.StateFile); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.InstallRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.21.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Setup began before uninstall and therefore captured no stop marker.
	captured := captureCensusLifecycleMarker(paths)
	if len(captured) != 0 {
		t.Fatal("unexpected pre-uninstall lifecycle marker")
	}
	if err := removeManagedConfigArtifacts(paths, &uninstallReport{}, true); err != nil {
		t.Fatal(err)
	}
	if err := saveState(paths, defaultInstallState()); err != nil {
		t.Fatal(err)
	}
	if reactivated, err := reactivateCensusAfterSetup(paths, captured); err != nil || reactivated {
		t.Fatalf("pre-uninstall setup reactivated census: %v, err=%v", reactivated, err)
	}
	if err := mutateCensusState(paths, func(state *censusState) { state.Enabled = true }); err == nil {
		t.Fatal("stale setup enabled census after uninstall")
	}
}

func TestRepeatedUninstallRotatesMarkerAgainstEarlierSetup(t *testing.T) {
	paths := setupCensusTest(t)
	if err := removeManagedConfigArtifacts(paths, &uninstallReport{}, true); err != nil {
		t.Fatal(err)
	}
	earlierSetup := captureCensusLifecycleMarker(paths)
	if len(earlierSetup) == 0 {
		t.Fatal("first uninstall did not write lifecycle marker")
	}
	if err := removeManagedConfigArtifacts(paths, &uninstallReport{}, true); err != nil {
		t.Fatal(err)
	}
	latest := captureCensusLifecycleMarker(paths)
	if len(latest) == 0 || bytes.Equal(latest, earlierSetup) {
		t.Fatal("repeated uninstall did not rotate lifecycle marker")
	}
	if reactivated, err := reactivateCensusAfterSetup(paths, earlierSetup); err != nil || reactivated {
		t.Fatalf("setup predating repeated uninstall cleared marker: %v, err=%v", reactivated, err)
	}
}

func TestCensusUninstallWaitsForSendAndRemovesConsent(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusEndpoint(t)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	releaseSend := make(chan struct{})
	var requests atomic.Int32
	originalClient := censusHTTPClient
	censusHTTPClient = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		requests.Add(1)
		close(started)
		<-releaseSend
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { censusHTTPClient = originalClient })

	sendDone := make(chan censusPingResult, 1)
	go func() { sendDone <- sendCensusPingOnce(paths) }()
	<-started
	uninstallDone := make(chan error, 1)
	go func() { uninstallDone <- removeManagedConfigArtifacts(paths, &uninstallReport{}, true) }()
	select {
	case err := <-uninstallDone:
		t.Fatalf("uninstall returned before the in-flight send ended: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseSend)
	if result := <-sendDone; !result.Attempted || result.Err != nil {
		t.Fatalf("in-flight send result = %+v", result)
	}
	if err := <-uninstallDone; err != nil {
		t.Fatalf("uninstall cleanup: %v", err)
	}
	if result := sendCensusPingOnce(paths); result.Attempted {
		t.Fatalf("post-uninstall carrier attempted a request: %+v", result)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("request count = %d, want only the pre-uninstall request", got)
	}
}

func TestCensusRejectsRedirectWithoutReplayingPost(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}

	var sourceRequests, targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sourceRequests.Add(1)
		w.Header().Set("Location", target.URL+"/ping")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	originalEndpoint, originalClient := censusEndpointURL, censusHTTPClient
	censusEndpointURL = source.URL
	censusHTTPClient = &http.Client{
		Timeout:       censusRequestTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	t.Cleanup(func() {
		censusEndpointURL = originalEndpoint
		censusHTTPClient = originalClient
	})

	result := sendCensusPingOnce(paths)
	if !result.Attempted || result.Err == nil {
		t.Fatalf("redirect result = %+v, want one failed attempt", result)
	}
	if sourceRequests.Load() != 1 || targetRequests.Load() != 0 {
		t.Fatalf("redirect requests source=%d target=%d, want 1/0", sourceRequests.Load(), targetRequests.Load())
	}
	if again := sendCensusPingOnce(paths); again.Attempted {
		t.Fatalf("redirect failure must stay stamped and never retry: %+v", again)
	}
}
