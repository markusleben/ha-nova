package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// relayNoticeSilentMarkerName: when the quiet first-use App check last came
// back with nothing to say, for this config and this HA NOVA version. Every
// AI session starts with that check, and over Home Assistant Link it costs
// several round trips (up to firstUseRelayNoticeTimeout): within the hour
// the quiet check skips it, as the release check reuses its cache. A notice
// is never remembered, so a pending App update or an App below the floor is
// rechecked every session; a changed config, server, or version rechecks too.
const relayNoticeSilentMarkerName = "relay-notice-silent"

const relayNoticeSilentTTL = time.Hour

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

func relayNoticeSilentRecently(paths runtimePaths) bool {
	marker := filepath.Join(paths.CacheDir, relayNoticeSilentMarkerName)
	info, err := os.Stat(marker)
	if err != nil || time.Since(info.ModTime()) >= relayNoticeSilentTTL {
		return false
	}
	stored, err := os.ReadFile(marker)
	return err == nil && strings.TrimSpace(string(stored)) == relayNoticeSilentKey(paths)
}

func rememberRelayNoticeSilent(paths runtimePaths) {
	mutateActiveInstallCache(paths, func() {
		if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
			return
		}
		_ = os.WriteFile(filepath.Join(paths.CacheDir, relayNoticeSilentMarkerName), []byte(relayNoticeSilentKey(paths)+"\n"), 0o644)
	})
}
