//go:build linux

package main

import (
	"os"
	"syscall"
)

// limitJQChildMemory caps the jq child's data segment, Go's heap included:
// an allocation past it fails and the child dies, not the bridge. If the
// container's own limit is hit first, its OOM killer picks the child.
func limitJQChildMemory(limit uint64) {
	_ = syscall.Setrlimit(syscall.RLIMIT_DATA, &syscall.Rlimit{Cur: limit, Max: limit})
	_ = os.WriteFile("/proc/self/oom_score_adj", []byte("1000"), 0o600)
}
