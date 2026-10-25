package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeJQChild makes the bridge's jq child a shell script.
func fakeJQChild(t *testing.T, script string) {
	t.Helper()
	prev := jqChildCmd
	jqChildCmd = func(ctx context.Context) (*exec.Cmd, error) { return exec.CommandContext(ctx, "sh", "-c", script), nil }
	t.Cleanup(func() { jqChildCmd = prev })
}

// The bridge maps whatever the child does to a read error, never a crash.
func TestMCPReadJQChildMapping(t *testing.T) {
	prev := mcpJQDeadline
	mcpJQDeadline = 300 * time.Millisecond
	t.Cleanup(func() { mcpJQDeadline = prev })
	input := []byte(`{"ok":true,"data":[1,2,3]}`)
	memory := "JQ_FAILED: jq: the filter needed too much memory or time"
	for script, want := range map[string]string{
		"cat >/dev/null; exit 3":                                 memory,
		"cat >/dev/null; kill -9 $$":                             memory,
		"cat >/dev/null; sleep 30":                               memory,
		"cat >/dev/null; echo not json":                          memory,
		"cat >/dev/null; head -c 300000 /dev/zero | tr '\\0' x":  memory,
		"cat >/dev/null; echo boom >&2; exit 1":                  memory,
		`cat >/dev/null; echo '{"code":"too_large"}'`:            "RESULT_TOO_LARGE",
		`cat >/dev/null; echo '{"code":"deadline"}'`:             "JQ_FAILED: jq: the filter ran longer",
		`cat >/dev/null; echo '{"code":"error","error":"boom"}'`: "JQ_FAILED: jq: boom",
		`cat >/dev/null; echo '{"output":"3"}'`:                  "3",
	} {
		fakeJQChild(t, script)
		start := time.Now()
		result, err := readResult("ws", input, childJQ(".data | length"))
		got := string(result)
		if failure, ok := err.(*verbFailure); ok {
			got = failure.code
			if failure.code == "JQ_FAILED" {
				got += ": " + failure.message
			}
		}
		if !strings.HasPrefix(got, want) || time.Since(start) > 4*time.Second {
			t.Errorf("%s: %q after %s, want %q", script, got, time.Since(start), want)
		}
	}
}

// In ha-nova serve, ha_read hands its filter to the child; local mcp keeps it in process.
func TestMCPReadJQChildInServe(t *testing.T) {
	paths, _ := readRelay(t, func(string, map[string]any) any { return []any{1, 2, 3} })
	fakeJQChild(t, `cat >/dev/null; echo '{"output":"\"from the child\""}'`)
	read := func() string {
		_, out := captureCommandOutput(t, func() int {
			return runMCPRead(paths, []string{"ws", `--body={"type":"get_states"}`, "--jq=.data | length"})
		})
		var env verbTestEnvelope
		_ = json.Unmarshal([]byte(out), &env)
		return string(env.Result)
	}
	if got := read(); got != "3" {
		t.Fatalf("local mcp: %s", got)
	}
	chatCall = &chatPolicy{}
	t.Cleanup(func() { chatCall = nil })
	if got := read(); got != `"from the child"` {
		t.Fatalf("serve: %s", got)
	}
}

// TestMCPReadJQChildHelper is the real child (maybeRunJQChild) for
// TestMCPReadJQChildLimits.
func TestMCPReadJQChildHelper(t *testing.T) {
	if os.Getenv("HA_NOVA_TEST_JQ_CHILD") != "1" {
		t.Skip("child process of TestMCPReadJQChildLimits")
	}
	_, exit := maybeRunJQChild([]string{jqChildCommand}, os.Stdin, os.Stdout)
	os.Exit(exit)
}

// The real child: a normal filter works; on Linux, one that allocates past
// the memory limit dies alone and comes back as JQ_FAILED.
func TestMCPReadJQChildLimits(t *testing.T) {
	prev := jqChildCmd
	t.Cleanup(func() { jqChildCmd = prev })
	child := func(marker bool) {
		jqChildCmd = func(ctx context.Context) (*exec.Cmd, error) {
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPReadJQChildHelper$", "-test.count=1")
			cmd.Env = append(os.Environ(), "HA_NOVA_TEST_JQ_CHILD=1")
			if marker {
				cmd.Env = append(cmd.Env, jqChildEnv+"=1")
			}
			return cmd, nil
		}
	}
	input := []byte(`{"ok":true,"data":[{"entity_id":"light.a"},{"entity_id":"light.b"}]}`)
	child(true)
	if result, err := readResult("ws", input, childJQ("[.data[].entity_id]")); err != nil || string(result) != `["light.a","light.b"]` {
		t.Fatalf("normal filter: %s %v", result, err)
	}
	if _, err := readResult("ws", input, childJQ("repeat(0)")); err == nil || err.(*verbFailure).code != "RESULT_TOO_LARGE" {
		t.Fatalf("endless output: %v", err)
	}
	child(false)
	if _, err := readResult("ws", input, childJQ(".")); err == nil || !strings.Contains(err.Error(), "too much memory or time") {
		t.Fatalf("child without the marker ran: %v", err)
	}
	if runtime.GOOS != "linux" {
		t.Skip("RLIMIT_DATA is Linux only; run in a Linux container")
	}
	child(true)
	for _, filter := range []string{`"x" * 1e9`, "[range(1e8)]"} {
		start := time.Now()
		_, err := readResult("ws", input, childJQ(filter))
		failure, _ := err.(*verbFailure)
		if failure == nil || failure.code != "JQ_FAILED" {
			t.Errorf("%s: %v", filter, err)
		}
		t.Logf("%s: %v after %s", filter, err, time.Since(start).Round(time.Millisecond))
	}
	if result, err := readResult("ws", input, childJQ("[range(100000)] | length")); err != nil || string(result) != "100000" {
		t.Fatalf("after a dead child, a normal filter: %s %v", result, err)
	}
}
