package main

import (
	"context"
	"os/exec"
	"time"
)

func nativeIdleTime() (time.Duration, bool) {
	root, err := ioregRead("-n", "Root", "-d", "1")
	if err != nil {
		return 0, false
	}
	hid, err := ioregRead("-c", "IOHIDSystem", "-d", "4", "-k", "HIDIdleTime")
	if err != nil {
		return 0, false
	}
	return parseHIDIdle(root, hid)
}

func ioregRead(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/sbin/ioreg", args...)
	cmd.Env = []string{}
	out, err := cmd.Output()
	return string(out), err
}
