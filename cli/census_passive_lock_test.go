package main

import (
	"testing"
	"time"
)

func TestPassiveRelayStampDoesNotDelayUpdateChecksWhenCensusLockIsHeld(t *testing.T) {
	paths := setupCensusTest(t)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("save census state: %v", err)
	}
	censusProcessLock <- struct{}{}
	defer func() { <-censusProcessLock }()

	started := time.Now()
	stampCensusRelayVersion(paths, "0.7.1")
	if elapsed := time.Since(started); elapsed > 25*time.Millisecond {
		t.Fatalf("passive relay stamp blocked for %s", elapsed)
	}
	if got := loadCensusState(paths).RelayVersion; got != "" {
		t.Fatalf("contended passive stamp should defer without mutation, got %q", got)
	}
}
