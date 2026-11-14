package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// relayNoticeSilentMarkerName: when the quiet first-use App check last got
// its full answer (the App's version and its update entity) with nothing to
// say, for this config and this HA NOVA version. Every AI session starts with
// that check, and over Home Assistant Link it costs several round trips (up
// to firstUseRelayNoticeTimeout): within the hour the quiet check skips it, as
// the release check reuses its cache. A notice, and a check that failed or
// timed out, is never remembered; a changed config, server, or version
// rechecks.
const relayNoticeSilentMarkerName = "relay-notice-silent"

const relayNoticeSilentTTL = time.Hour

// relayNoticeSilentKey is taken before the check, so a config rewritten while
// it ran never inherits its result.
func relayNoticeSilentKey(paths runtimePaths) string {
	config, _ := os.ReadFile(paths.ConfigFile)
	version, _ := os.ReadFile(paths.VersionFile)
	server, _ := requestedServerSelection()
	sum := sha256.New()
	for _, part := range [][]byte{config, version, []byte(server)} {
		sum.Write(part)
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func relayNoticeSilentRecently(paths runtimePaths, key string) bool {
	marker := filepath.Join(paths.CacheDir, relayNoticeSilentMarkerName)
	info, err := os.Stat(marker)
	if err != nil {
		return false
	}
	if age := time.Since(info.ModTime()); age < 0 || age >= relayNoticeSilentTTL {
		return false
	}
	stored, err := os.ReadFile(marker)
	return err == nil && strings.TrimSpace(string(stored)) == key
}

func rememberRelayNoticeSilent(paths runtimePaths, key string) {
	mutateActiveInstallCache(paths, func() {
		if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
			return
		}
		_ = os.WriteFile(filepath.Join(paths.CacheDir, relayNoticeSilentMarkerName), []byte(key+"\n"), 0o644)
	})
}
