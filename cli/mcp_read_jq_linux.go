//go:build linux

package main

import (
	"bufio"
	"bytes"
	"errors"
	"os"
	"strconv"
	"syscall"
)

// limitJQChildMemory caps the jq child's data segment, Go's heap included,
// at what the process already maps plus budget: an allocation past it
// fails and the child dies, not the bridge. The base is measured, not
// assumed: the Go runtime's own mappings at start differ by architecture
// and binary (about 80 MiB for the test binary on linux/arm64), so a fixed
// cap left amd64 almost nothing. If the container's own limit is hit first,
// its OOM killer picks the child.
func limitJQChildMemory(budget uint64) error {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return err
	}
	var used uint64
	for scanner := bufio.NewScanner(bytes.NewReader(status)); scanner.Scan(); {
		fields := bytes.Fields(scanner.Bytes())
		if len(fields) == 3 && string(fields[0]) == "VmData:" && string(fields[2]) == "kB" {
			used, err = strconv.ParseUint(string(fields[1]), 10, 64)
			used <<= 10
		}
	}
	if used == 0 || err != nil {
		return errors.New("cannot read the jq child's memory use")
	}
	limit := used + budget
	if err := syscall.Setrlimit(syscall.RLIMIT_DATA, &syscall.Rlimit{Cur: limit, Max: limit}); err != nil {
		return err
	}
	_ = os.WriteFile("/proc/self/oom_score_adj", []byte("1000"), 0o600)
	return nil
}
