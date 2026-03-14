package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "ha-nova")
	os.MkdirAll(configDir, 0o755)

	content := `{
  "schema_version": 1,
  "ha_host": "192.168.1.5",
  "ha_url": "http://192.168.1.5:8123",
  "relay_base_url": "http://192.168.1.5:8791"
}`
	os.WriteFile(filepath.Join(configDir, "config.json"), []byte(content), 0o644)

	t.Setenv("HOME", home)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.RelayBaseURL != "http://192.168.1.5:8791" {
		t.Errorf("RelayBaseURL = %q, want %q", cfg.RelayBaseURL, "http://192.168.1.5:8791")
	}
}

func TestLoadConfigRejectsLegacyOnboardingEnv(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "ha-nova")
	os.MkdirAll(configDir, 0o755)

	content := "RELAY_BASE_URL='http://10.0.0.1:8791'\n"
	os.WriteFile(filepath.Join(configDir, "onboarding.env"), []byte(content), 0o644)

	t.Setenv("HOME", home)
	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for legacy onboarding.env-only config")
	}
}

func TestLoadConfigMissingURL(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "ha-nova")
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"schema_version":1,"ha_host":"192.168.1.5"}`), 0o644)
	t.Setenv("HOME", home)
	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for missing relay_base_url")
	}
}

func TestLoadConfigMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		a, b string
		want int // -1 = a<b, 0 = a==b, 1 = a>b
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.1.0", "0.2.0", -1},
		{"0.2.0", "0.1.0", 1},
		{"1.0.0", "0.9.9", 1},
		{"0.1.0", "0.1.1", -1},
	}
	for _, tt := range tests {
		got := compareSemver(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseVersionJSON(t *testing.T) {
	dir := t.TempDir()
	content := `{"skill_version":"0.1.11","min_relay_version":"0.1.0"}`
	os.WriteFile(filepath.Join(dir, "version.json"), []byte(content), 0o644)

	minV := readMinRelayVersion(dir)
	if minV != "0.1.0" {
		t.Errorf("minRelayVersion = %q, want %q", minV, "0.1.0")
	}
}

func TestRunJQFilter(t *testing.T) {
	input := `{"data":{"entities":[{"ei":"automation.test","en":"Test Auto"},{"ei":"script.foo","en":"Foo Script"}]}}`
	filter := `.data.entities[] | select(.ei | startswith("automation.")) | {entity_id: .ei, name: .en}`

	res, err := applyJQFilter(filter, []byte(input), false)
	if err != nil {
		t.Fatalf("applyJQFilter error: %v", err)
	}
	if !strings.Contains(res.output, "automation.test") {
		t.Errorf("expected automation.test in output, got: %s", res.output)
	}
	if strings.Contains(res.output, "script.foo") {
		t.Errorf("expected script.foo to be filtered out, got: %s", res.output)
	}
}

func TestRunJQRawOutput(t *testing.T) {
	input := `{"data":{"unique_id":"abc123"}}`
	filter := `.data.unique_id`

	res, err := applyJQFilter(filter, []byte(input), true)
	if err != nil {
		t.Fatalf("applyJQFilter error: %v", err)
	}
	trimmed := strings.TrimSpace(res.output)
	if trimmed != "abc123" {
		t.Errorf("raw output = %q, want %q", trimmed, "abc123")
	}
}

func TestRunJQErrorFilter(t *testing.T) {
	input := `{"ok":false,"error":{"message":"not found"}}`
	filter := `if .ok then .data else error("relay error: \(.error.message // "unknown")") end`

	_, err := applyJQFilter(filter, []byte(input), false)
	if err == nil {
		t.Fatal("expected error from jq error() call")
	}
}

func TestRunJQSelectWithTest(t *testing.T) {
	input := `{"data":{"entities":[{"ei":"automation.kitchen_light","en":"Kitchen Light"},{"ei":"automation.bedroom_fan","en":"Bedroom Fan"}]}}`
	filter := `[.data.entities[] | select((.ei + " " + (.en // "")) | test("kitchen";"i")) | {entity_id: .ei, name: .en}]`

	res, err := applyJQFilter(filter, []byte(input), false)
	if err != nil {
		t.Fatalf("applyJQFilter error: %v", err)
	}
	if !strings.Contains(res.output, "kitchen_light") {
		t.Errorf("expected kitchen_light in output, got: %s", res.output)
	}
	if strings.Contains(res.output, "bedroom_fan") {
		t.Errorf("expected bedroom_fan filtered out, got: %s", res.output)
	}
}

func TestRunJQExitStatusLastValue(t *testing.T) {
	input := `{"a":true,"b":false}`

	// Last value is false → should signal exit status
	res, err := applyJQFilter(".a, .b", []byte(input), false)
	if err != nil {
		t.Fatalf("applyJQFilter error: %v", err)
	}
	if res.lastValue != false {
		t.Errorf("lastValue = %v, want false", res.lastValue)
	}

	// Last value is true → should NOT signal exit status
	res2, err := applyJQFilter(".b, .a", []byte(input), false)
	if err != nil {
		t.Fatalf("applyJQFilter error: %v", err)
	}
	if res2.lastValue != true {
		t.Errorf("lastValue = %v, want true", res2.lastValue)
	}

	// Null last value
	res3, err := applyJQFilter(".missing", []byte(input), false)
	if err != nil {
		t.Fatalf("applyJQFilter error: %v", err)
	}
	if res3.lastValue != nil {
		t.Errorf("lastValue = %v, want nil", res3.lastValue)
	}
}

func TestExtractPayload(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"-d", `{"type":"get_states"}`}, `{"type":"get_states"}`},
		{[]string{}, ""},
		{[]string{"-d"}, ""},
		{[]string{"-H", "X-Custom: foo", "-d", `{"x":1}`}, `{"x":1}`},
	}
	for _, tt := range tests {
		got := extractPayload(tt.args)
		if got != tt.want {
			t.Errorf("extractPayload(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestDispatchDoesNotTreatArgv0RelayAsSpecial(t *testing.T) {
	paths := runtimePaths{}
	if exitCode := dispatch(paths, "relay", []string{"health"}); exitCode == 0 {
		t.Fatal("expected argv0 relay without subcommand support to fail")
	}
}

func TestNormalizeSetupArgsMovesTargetAfterFlags(t *testing.T) {
	got := normalizeSetupArgs([]string{
		"all",
		"--host", "127.0.0.1",
		"--relay-token", "test-relay-token",
		"--non-interactive",
	})

	want := []string{
		"--host", "127.0.0.1",
		"--relay-token", "test-relay-token",
		"--non-interactive",
		"all",
	}

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("normalizeSetupArgs() = %v, want %v", got, want)
	}
}
