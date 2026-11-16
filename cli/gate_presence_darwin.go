package main

import (
	"context"
	"os/exec"
	"time"
)

func nativeIdleTime() (time.Duration, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/sbin/ioreg", "-c", "IOHIDSystem", "-d", "4", "-k", "HIDIdleTime")
	cmd.Env = []string{}
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	return parseHIDIdle(string(out))
}
