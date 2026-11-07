package main

import (
	"errors"
	"os/exec"
	"testing"
)

func TestAppOfferAfterInstallRunsInTheNewBinary(t *testing.T) {
	oldRun, oldNotice := runNewBinaryAppOffer, relayUpdateNoticeForUpdate
	t.Cleanup(func() { runNewBinaryAppOffer, relayUpdateNoticeForUpdate = oldRun, oldNotice })
	var exitErr error = &exec.ExitError{}
	cases := []struct {
		name, target  string
		newBinary     error
		wantNewBinary bool
		wantInProcess bool
	}{
		{name: "new version offers itself", target: "0.28.3", wantNewBinary: true},
		{name: "release candidate offers itself", target: "0.28.3-rc1", wantNewBinary: true},
		{name: "new binary ran but failed: no second offer", target: "0.29.0", newBinary: exitErr, wantNewBinary: true},
		{name: "new binary didn't start: this one offers", target: "0.29.0", newBinary: errors.New("exec: not found"), wantNewBinary: true, wantInProcess: true},
		{name: "older target lacks the command", target: "0.28.2", wantInProcess: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ranNew, ranHere bool
			runNewBinaryAppOffer = func(runtimePaths) error { ranNew = true; return tc.newBinary }
			relayUpdateNoticeForUpdate = func(runtimePaths) humanNotice { ranHere = true; return humanNotice{} }
			offerRelayUpdateAfterInstall(runtimePaths{}, tc.target)
			if ranNew != tc.wantNewBinary || ranHere != tc.wantInProcess {
				t.Fatalf("new binary %v, in-process %v; want %v, %v", ranNew, ranHere, tc.wantNewBinary, tc.wantInProcess)
			}
		})
	}
}

func TestInternalOfferAppUpdateCommandOffersAndExitsZero(t *testing.T) {
	oldNotice := relayUpdateNoticeForUpdate
	t.Cleanup(func() { relayUpdateNoticeForUpdate = oldNotice })
	asked := false
	relayUpdateNoticeForUpdate = func(runtimePaths) humanNotice { asked = true; return humanNotice{} }
	paths := guidedUpdatePaths(t)
	t.Cleanup(func() { setServerSelectionOverride("") })
	if code := dispatch(paths, "ha-nova", []string{"internal-offer-app-update", "--server", "default"}); code != 0 || !asked || serverSelectionOverride != "default" {
		t.Fatalf("exit %d, offer asked %v, server flag %q", code, asked, serverSelectionOverride)
	}
}
