//go:build !linux

package main

// limitJQChildMemory: no RLIMIT_DATA outside Linux (macOS ignores it). The
// bridge runs in the Linux App container; elsewhere the child still keeps a
// runaway filter out of the parent's process, with Go's soft memory limit
// only.
func limitJQChildMemory(uint64) {}
