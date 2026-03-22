package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

var writerSupportsTTYForSetup = writerSupportsTTY
var setupSpinnerDebounce = 150 * time.Millisecond
var setupDiscoveryMinimumVisibleDuration = setupSpinnerDebounce
var setupHostCheckMinimumVisibleDuration = setupSpinnerDebounce

func formatSetupCountdownLabel(label string, deadline time.Time) string {
	remaining := time.Until(deadline)
	if remaining < 0 {
		remaining = 0
	}
	seconds := int((remaining + time.Second - 1) / time.Second)
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%s (%ds left)", label, seconds)
}

func runSetupSpinnerWithResult[T any](out io.Writer, label string, minDuration time.Duration, task func() (T, error)) (T, error) {
	session := resolveSetupUISession(out)
	if !session.animatesSpinner() {
		fmt.Fprintf(out, "  %s\n", label)
		return task()
	}

	restoreInputEcho := func() {}
	if guard, err := acquireSetupInputEchoGuard(); err == nil {
		restoreInputEcho = guard
	}
	defer restoreInputEcho()

	type result struct {
		value T
		err   error
	}

	done := make(chan result, 1)
	go func() {
		value, err := task()
		done <- result{value: value, err: err}
	}()

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	debounce := time.NewTimer(minDuration)
	defer debounce.Stop()
	var ticker *time.Ticker
	var tick <-chan time.Time
	shown := false
	idx := 0
	for {
		select {
		case found := <-done:
			if ticker != nil {
				ticker.Stop()
			}
			if shown {
				fmt.Fprint(out, "\r\033[2K")
			}
			return found.value, found.err
		case <-debounce.C:
			shown = true
			fmt.Fprintf(out, "\r  %s %s", frames[0], label)
			ticker = time.NewTicker(100 * time.Millisecond)
			tick = ticker.C
			idx = 1
		case <-tick:
			fmt.Fprintf(out, "\r  %s %s", frames[idx%len(frames)], label)
			idx++
		}
	}
}

func runSetupCountdownSpinnerWithResult[T any](out io.Writer, label string, timeout, minDuration time.Duration, task func() (T, error)) (T, error) {
	session := resolveSetupUISession(out)
	if !session.animatesSpinner() {
		fmt.Fprintf(out, "  %s (up to %ds)\n", label, int((timeout+time.Second-1)/time.Second))
		return task()
	}

	restoreInputEcho := func() {}
	if guard, err := acquireSetupInputEchoGuard(); err == nil {
		restoreInputEcho = guard
	}
	defer restoreInputEcho()

	type result struct {
		value T
		err   error
	}

	done := make(chan result, 1)
	deadline := time.Now().Add(timeout)
	go func() {
		value, err := task()
		done <- result{value: value, err: err}
	}()

	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	debounce := time.NewTimer(minDuration)
	defer debounce.Stop()
	var ticker *time.Ticker
	var tick <-chan time.Time
	shown := false
	idx := 0
	for {
		select {
		case found := <-done:
			if ticker != nil {
				ticker.Stop()
			}
			if shown {
				fmt.Fprint(out, "\r\033[2K")
			}
			return found.value, found.err
		case <-debounce.C:
			shown = true
			fmt.Fprintf(out, "\r  %s %s", frames[0], formatSetupCountdownLabel(label, deadline))
			ticker = time.NewTicker(100 * time.Millisecond)
			tick = ticker.C
			idx = 1
		case <-tick:
			fmt.Fprintf(out, "\r  %s %s", frames[idx%len(frames)], formatSetupCountdownLabel(label, deadline))
			idx++
		}
	}
}

func resolveHAURLBaseWithFeedback(out io.Writer, input string) (string, error) {
	if !resolveSetupUISession(out).animatesSpinner() {
		fmt.Fprintln(out, "  Checking connection to Home Assistant...")
		return resolveHAURLBaseForSetup(input)
	}
	return runSetupSpinnerWithResult(out, "Checking connection to Home Assistant...", setupHostCheckMinimumVisibleDuration, func() (string, error) {
		return resolveHAURLBaseForSetup(input)
	})
}

func runSetupStepWithFeedback(out io.Writer, label string, task func() error) error {
	if !resolveSetupUISession(out).animatesSpinner() {
		fmt.Fprintf(out, "  %s\n", label)
		return task()
	}
	_, err := runSetupSpinnerWithResult(out, label, 350*time.Millisecond, func() (struct{}, error) {
		return struct{}{}, task()
	})
	return err
}

func writerSupportsTTY(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
