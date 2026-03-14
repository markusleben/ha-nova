# Go Relay CLI Implementation Plan

> Historical implementation note: this plan predates the Go-first runtime cutover. Current public contract is `ha-nova setup`, `ha-nova relay ...`, `ha-nova check-update`, and `ha-nova update`; raw relay file paths are compatibility-only.

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `scripts/relay.sh` with a single Go binary (`cli/`) that provides relay HTTP proxy + built-in jq filtering, eliminating Bash/curl/jq dependencies.

**Architecture:** Single Go package in `cli/` with platform-specific keychain via build tags. goreleaser builds binaries for 5 targets. Skills pipe JSON through `relay jq` instead of external `jq`.

**Tech Stack:** Go 1.22+, gojq, go-keyring, goreleaser

**Spec:** `docs/superpowers/specs/2026-03-13-go-relay-cli-design.md`

---

## File Structure

### New Files (Create)

| File | Responsibility |
|------|---------------|
| `cli/main.go` | Entry point, command routing (health/ws/core/jq/version) |
| `cli/relay.go` | HTTP client: POST to ws/core, GET for health |
| `cli/config.go` | Parse `~/.config/ha-nova/onboarding.env` |
| `cli/keyring_darwin.go` | macOS: exec `security find-generic-password` |
| `cli/keyring_windows.go` | Windows: `go-keyring` Credential Manager |
| `cli/keyring_linux.go` | Linux: `go-keyring` Secret Service |
| `cli/version.go` | `version` subcommand + semver comparison for health |
| `cli/jq.go` | gojq wrapper: stdin → filter → stdout |
| `cli/go.mod` | Go module definition |
| `cli/main_test.go` | Go unit tests for config, jq, version logic |
| `.goreleaser.yml` | Cross-compilation config for 5 targets |

### Modified Files

| File | Change |
|------|--------|
| `scripts/onboarding/install-local-skills.sh:225,246-251` | Download Go binary instead of copying relay.sh |
| `scripts/update.sh:323` | Download Go binary instead of copying relay.sh |
| `scripts/dev-sync.sh` | Update `sync_shared_tools()` to download Go binary instead of copying relay.sh |
| `install.sh:233` | Download Go binary instead of copying relay.sh |
| `tests/onboarding/relay-cli-contract.test.ts` | Test Go binary instead of bash relay.sh |
| `tests/skills/ha-nova-contract.test.ts:251-256` | Assert `cli/main.go` exists instead of `scripts/relay.sh` |
| `tests/onboarding/self-update-contract.test.ts:82` | Update relay.sh assertion |
| 9 skill files (32 pipe occurrences + 2 standalone) | `jq` → `relay jq` |

### Files Removed

| File | Reason |
|------|--------|
| `scripts/relay.sh` | Replaced by Go binary |

---

## Chunk 1: Go Binary Core

### Task 1: Initialize Go Module

**Files:**
- Create: `cli/go.mod`
- Create: `cli/main.go`

- [ ] **Step 1: Create Go module**

```bash
mkdir -p cli
cd cli
go mod init github.com/markusleben/ha-nova/cli
```

- [ ] **Step 2: Create minimal main.go with command routing**

Create `cli/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

// Version is set by goreleaser via ldflags.
var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: relay <health|ws|core|jq|version> [args...]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "health":
		runHealth(os.Args[2:])
	case "ws":
		runProxy("ws", os.Args[2:])
	case "core":
		runProxy("core", os.Args[2:])
	case "jq":
		runJQ(os.Args[2:])
	case "version":
		fmt.Println(Version)
	default:
		runProxy(os.Args[1], os.Args[2:])
	}
}
```

- [ ] **Step 3: Verify it compiles**

```bash
cd cli && go build -o /dev/null . && echo "OK"
```

Expected: Will NOT compile yet — `runHealth`, `runProxy`, `runJQ` are undefined. This is expected and resolved in Tasks 4-6.

- [ ] **Step 4: Commit**

```bash
git add cli/go.mod cli/main.go
git commit -m "feat(cli): initialize Go module with command routing"
```

---

### Task 2: Config Loading

**Files:**
- Create: `cli/config.go`
- Create: `cli/main_test.go`

- [ ] **Step 1: Write the test**

Create `cli/main_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "ha-nova")
	os.MkdirAll(configDir, 0o755)

	content := "RELAY_BASE_URL=http://192.168.1.5:8791\nHA_HOST=192.168.1.5\n"
	os.WriteFile(filepath.Join(configDir, "onboarding.env"), []byte(content), 0o644)

	t.Setenv("HOME", home)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.RelayBaseURL != "http://192.168.1.5:8791" {
		t.Errorf("RelayBaseURL = %q, want %q", cfg.RelayBaseURL, "http://192.168.1.5:8791")
	}
}

func TestLoadConfigQuotedValues(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "ha-nova")
	os.MkdirAll(configDir, 0o755)

	content := "RELAY_BASE_URL='http://10.0.0.1:8791'\n"
	os.WriteFile(filepath.Join(configDir, "onboarding.env"), []byte(content), 0o644)

	t.Setenv("HOME", home)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.RelayBaseURL != "http://10.0.0.1:8791" {
		t.Errorf("RelayBaseURL = %q, want %q", cfg.RelayBaseURL, "http://10.0.0.1:8791")
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd cli && go test -run TestLoadConfig -v
```

Expected: FAIL — `loadConfig` undefined.

- [ ] **Step 3: Implement config loading**

Create `cli/config.go`:

```go
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type config struct {
	RelayBaseURL string
}

func loadConfig() (config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config{}, fmt.Errorf("cannot determine home directory: %w", err)
	}

	path := filepath.Join(home, ".config", "ha-nova", "onboarding.env")
	f, err := os.Open(path)
	if err != nil {
		return config{}, fmt.Errorf("HA NOVA is not set up yet. Run: ha-nova setup")
	}
	defer f.Close()

	var cfg config
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, "'\"")
		switch key {
		case "RELAY_BASE_URL":
			cfg.RelayBaseURL = value
		}
	}
	return cfg, scanner.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd cli && go test -run TestLoadConfig -v
```

Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add cli/config.go cli/main_test.go
git commit -m "feat(cli): add config loading from onboarding.env"
```

---

### Task 3: Keychain Access (Platform-Specific)

**Files:**
- Create: `cli/keyring_darwin.go`
- Create: `cli/keyring_windows.go`
- Create: `cli/keyring_linux.go`

- [ ] **Step 1: Implement macOS keychain (security CLI)**

Create `cli/keyring_darwin.go`:

```go
//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"
)

func readKeychainToken() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}

	out, err := exec.Command(
		"security", "find-generic-password",
		"-a", u.Username,
		"-s", "ha-nova.relay-auth-token",
		"-w",
	).Output()
	if err != nil {
		return "", fmt.Errorf("missing relay auth token (ha-nova.relay-auth-token)")
	}
	return strings.TrimSpace(string(out)), nil
}
```

- [ ] **Step 2: Implement Windows keychain (go-keyring)**

Create `cli/keyring_windows.go`:

```go
//go:build windows

package main

import (
	"fmt"
	"os/user"

	"github.com/zalando/go-keyring"
)

func readKeychainToken() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}

	token, err := keyring.Get("ha-nova.relay-auth-token", u.Username)
	if err != nil {
		return "", fmt.Errorf("missing relay auth token (ha-nova.relay-auth-token)")
	}
	return token, nil
}
```

- [ ] **Step 3: Implement Linux keychain (go-keyring)**

Create `cli/keyring_linux.go`:

```go
//go:build linux

package main

import (
	"fmt"
	"os/user"

	"github.com/zalando/go-keyring"
)

func readKeychainToken() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("cannot determine current user: %w", err)
	}

	token, err := keyring.Get("ha-nova.relay-auth-token", u.Username)
	if err != nil {
		return "", fmt.Errorf("missing relay auth token (ha-nova.relay-auth-token)")
	}
	return token, nil
}
```

- [ ] **Step 4: Add go-keyring dependency**

```bash
cd cli && go get github.com/zalando/go-keyring
```

- [ ] **Step 5: Verify compilation on macOS**

```bash
cd cli && go build -o /dev/null .
```

Expected: still fails (missing runHealth, runProxy, runJQ). That's expected.

- [ ] **Step 6: Commit**

```bash
git add cli/keyring_darwin.go cli/keyring_windows.go cli/keyring_linux.go cli/go.mod cli/go.sum
git commit -m "feat(cli): add platform-specific keychain access"
```

---

### Task 4: HTTP Client (ws/core/health)

**Files:**
- Create: `cli/relay.go`

- [ ] **Step 1: Implement HTTP proxy for ws/core**

Create `cli/relay.go`:

```go
package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

var httpClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
	},
}

// extractPayload finds the -d flag value in args.
func extractPayload(args []string) string {
	for i, arg := range args {
		if arg == "-d" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func runProxy(endpoint string, args []string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	token, err := readKeychainToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	url := strings.TrimRight(cfg.RelayBaseURL, "/") + "/" + endpoint
	payload := extractPayload(args)

	var body io.Reader
	if payload != "" {
		body = strings.NewReader(payload)
	}

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	io.Copy(os.Stdout, resp.Body)

	if resp.StatusCode >= 400 {
		os.Exit(1)
	}
}

func runHealth(args []string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	token, err := readKeychainToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}

	url := strings.TrimRight(cfg.RelayBaseURL, "/") + "/health"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil || len(bodyBytes) == 0 {
		os.Exit(1)
	}

	// Run optional version-check hook before JSON output
	runVersionCheckHook()

	// Output health JSON
	os.Stdout.Write(bodyBytes)
	if len(bodyBytes) > 0 && bodyBytes[len(bodyBytes)-1] != '\n' {
		fmt.Fprintln(os.Stdout)
	}

	// Check relay version against min_relay_version
	checkRelayVersion(bodyBytes)
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd cli && go build -o /dev/null .
```

Expected: still fails (missing runJQ, runVersionCheckHook, checkRelayVersion). Expected.

- [ ] **Step 3: Commit**

```bash
git add cli/relay.go
git commit -m "feat(cli): add HTTP client for ws/core/health endpoints"
```

---

### Task 5: Version Check Logic

**Files:**
- Create: `cli/version.go`
- Modify: `cli/main_test.go`

- [ ] **Step 1: Write semver comparison test**

Append to `cli/main_test.go`:

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd cli && go test -run "TestCompareSemver|TestParseVersionJSON" -v
```

Expected: FAIL — functions undefined.

- [ ] **Step 3: Implement version.go**

Create `cli/version.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// compareSemver compares two semver strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareSemver(a, b string) int {
	ap := parseSemver(a)
	bp := parseSemver(b)
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(s string) [3]int {
	parts := strings.SplitN(s, ".", 3)
	var v [3]int
	for i := range parts {
		if i < 3 {
			v[i], _ = strconv.Atoi(parts[i])
		}
	}
	return v
}

type versionJSON struct {
	SkillVersion    string `json:"skill_version"`
	MinRelayVersion string `json:"min_relay_version"`
}

// readMinRelayVersion reads min_relay_version from version.json in the given directory.
func readMinRelayVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "version.json"))
	if err != nil {
		return ""
	}
	var v versionJSON
	if json.Unmarshal(data, &v) != nil {
		return ""
	}
	return v.MinRelayVersion
}

// findVersionJSON searches for version.json in git root, then ~/.config/ha-nova/.
func findVersionJSON() string {
	// Try git root first
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err == nil {
		dir := strings.TrimSpace(string(out))
		if _, err := os.Stat(filepath.Join(dir, "version.json")); err == nil {
			return dir
		}
	}

	// Fall back to config dir
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	configDir := filepath.Join(home, ".config", "ha-nova")
	if _, err := os.Stat(filepath.Join(configDir, "version.json")); err == nil {
		return configDir
	}
	return ""
}

// runVersionCheckHook executes ~/.config/ha-nova/version-check if it exists and is executable.
func runVersionCheckHook() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	hook := filepath.Join(home, ".config", "ha-nova", "version-check")
	info, err := os.Stat(hook)
	if err != nil || info.Mode()&0o111 == 0 {
		return
	}
	cmd := exec.Command(hook)
	cmd.Stdout = os.Stdout // version-check notices appear before health JSON (matches relay.sh)
	cmd.Stderr = os.Stderr
	cmd.Run() // ignore errors — hook is optional
}

// checkRelayVersion compares relay version from health JSON against min_relay_version.
func checkRelayVersion(healthBody []byte) {
	var health struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(healthBody, &health) != nil || health.Version == "" {
		return
	}

	dir := findVersionJSON()
	if dir == "" {
		return
	}

	minV := readMinRelayVersion(dir)
	if minV == "" {
		return
	}

	if compareSemver(health.Version, minV) < 0 {
		fmt.Fprintf(os.Stdout, "⚠️ RELAY OUTDATED: v%s is below minimum v%s — Inform the user: update the NOVA Relay App in Home Assistant.\n", health.Version, minV)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd cli && go test -run "TestCompareSemver|TestParseVersionJSON" -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cli/version.go cli/main_test.go
git commit -m "feat(cli): add semver comparison and version.json parsing"
```

---

### Task 6: jq Subcommand

**Files:**
- Create: `cli/jq.go`
- Modify: `cli/main_test.go`

- [ ] **Step 1: Add gojq dependency**

```bash
cd cli && go get github.com/itchyny/gojq
```

- [ ] **Step 2: Write jq tests**

Append to `cli/main_test.go` (add `"strings"` to the import block at the top of the file):

```go
func TestRunJQFilter(t *testing.T) {
	input := `{"data":{"entities":[{"ei":"automation.test","en":"Test Auto"},{"ei":"script.foo","en":"Foo Script"}]}}`
	filter := `.data.entities[] | select(.ei | startswith("automation.")) | {entity_id: .ei, name: .en}`

	result, err := applyJQFilter(filter, []byte(input), false)
	if err != nil {
		t.Fatalf("applyJQFilter error: %v", err)
	}
	if !strings.Contains(result, "automation.test") {
		t.Errorf("expected automation.test in output, got: %s", result)
	}
	if strings.Contains(result, "script.foo") {
		t.Errorf("expected script.foo to be filtered out, got: %s", result)
	}
}

func TestRunJQRawOutput(t *testing.T) {
	input := `{"data":{"unique_id":"abc123"}}`
	filter := `.data.unique_id`

	result, err := applyJQFilter(filter, []byte(input), true)
	if err != nil {
		t.Fatalf("applyJQFilter error: %v", err)
	}
	trimmed := strings.TrimSpace(result)
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

	result, err := applyJQFilter(filter, []byte(input), false)
	if err != nil {
		t.Fatalf("applyJQFilter error: %v", err)
	}
	if !strings.Contains(result, "kitchen_light") {
		t.Errorf("expected kitchen_light in output, got: %s", result)
	}
	if strings.Contains(result, "bedroom_fan") {
		t.Errorf("expected bedroom_fan filtered out, got: %s", result)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd cli && go test -run "TestRunJQ" -v
```

Expected: FAIL — `applyJQFilter` undefined.

- [ ] **Step 4: Implement jq.go**

Create `cli/jq.go` (supports `-r` for raw output and `-e` for exit-status on false/null):

```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/itchyny/gojq"
)

// applyJQFilter runs a jq filter on input bytes. Returns output string.
func applyJQFilter(filter string, input []byte, raw bool) (string, error) {
	query, err := gojq.Parse(filter)
	if err != nil {
		return "", fmt.Errorf("jq parse error: %w", err)
	}

	code, err := gojq.Compile(query)
	if err != nil {
		return "", fmt.Errorf("jq compile error: %w", err)
	}

	var inputVal interface{}
	if err := json.Unmarshal(input, &inputVal); err != nil {
		return "", fmt.Errorf("invalid JSON input: %w", err)
	}

	var out strings.Builder
	iter := code.Run(inputVal)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			return "", err
		}
		if raw {
			if s, ok := v.(string); ok {
				fmt.Fprintln(&out, s)
				continue
			}
		}
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		fmt.Fprintln(&out, string(b))
	}
	return out.String(), nil
}

func runJQ(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: relay jq [-r] [-e] '<filter>'")
		os.Exit(1)
	}

	raw := false
	exitStatus := false // -e: exit 1 if last output is false or null
	remaining := args
	for len(remaining) > 0 && strings.HasPrefix(remaining[0], "-") {
		switch remaining[0] {
		case "-r":
			raw = true
		case "-e":
			exitStatus = true
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", remaining[0])
			os.Exit(1)
		}
		remaining = remaining[1:]
	}
	if len(remaining) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: relay jq [-r] [-e] '<filter>'")
		os.Exit(1)
	}
	filter := remaining[0]

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading stdin: %s\n", err)
		os.Exit(1)
	}

	result, err := applyJQFilter(filter, input, raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	fmt.Print(result)

	// -e flag: exit 1 if last output value is false or null
	if exitStatus {
		trimmed := strings.TrimSpace(result)
		if trimmed == "false" || trimmed == "null" || trimmed == "" {
			os.Exit(1)
		}
	}
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd cli && go test -run "TestRunJQ" -v
```

Expected: PASS (4 tests).

- [ ] **Step 6: Build the complete binary**

```bash
cd cli && go build -o relay .
```

Expected: SUCCESS — all functions now defined.

- [ ] **Step 7: Smoke test the binary locally**

```bash
echo '{"data":[1,2,3]}' | ./cli/relay jq '.data[]'
```

Expected output:
```
1
2
3
```

- [ ] **Step 8: Commit**

```bash
git add cli/jq.go cli/main_test.go cli/go.mod cli/go.sum
git commit -m "feat(cli): add jq subcommand with gojq"
```

---

### Task 7: Full Go Test Suite

**Files:**
- Modify: `cli/main_test.go`

- [ ] **Step 1: Run complete Go test suite**

```bash
cd cli && go test -v ./...
```

Expected: All 9 tests pass (config: 3, semver: 1, version.json: 1, jq: 4). The extractPayload test is added in Step 2 below.

- [ ] **Step 2: Add extractPayload test**

Append to `cli/main_test.go`:

```go
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
```

- [ ] **Step 3: Run tests**

```bash
cd cli && go test -v ./...
```

Expected: PASS (10 tests).

- [ ] **Step 4: Commit**

```bash
git add cli/main_test.go
git commit -m "test(cli): add extractPayload test"
```

---

## Chunk 2: goreleaser & CI

### Task 8: goreleaser Configuration

**Files:**
- Create: `.goreleaser.yml`

- [ ] **Step 1: Create goreleaser config**

Create `.goreleaser.yml`:

```yaml
version: 2

project_name: ha-nova-relay

builds:
  - id: relay-darwin
    dir: cli
    binary: relay
    env:
      - CGO_ENABLED=0
    goos:
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.Version={{.Version}}

  - id: relay-other
    dir: cli
    binary: relay
    env:
      - CGO_ENABLED=0
    goos:
      - windows
      - linux
    goarch:
      - amd64
      - arm64
    ignore:
      - goos: windows
        goarch: arm64
    ldflags:
      - -s -w -X main.Version={{.Version}}

archives:
  - format: binary
    name_template: "relay-{{ .Os }}-{{ .Arch }}"

checksum:
  name_template: "checksums.txt"

snapshot:
  name_template: "{{ incpatch .Version }}-snapshot"

release:
  github:
    owner: markusleben
    name: ha-nova
  draft: true
  prerelease: auto

signs:
  - cmd: codesign
    args:
      - "--sign"
      - "Developer ID Application: {{ .Env.APPLE_IDENTITY }}"
      - "--options"
      - "runtime"
      - "${artifact}"
    artifacts: binary
    ids:
      - relay-darwin
    output: false

# Notarization runs as a post-hook after signing.
# Requires APPLE_ID, APPLE_PASSWORD (app-specific), and APPLE_TEAM_ID env vars.
after:
  hooks:
    - cmd: |
        bash -c '
        for f in dist/relay-darwin-*; do
          [ -f "$f" ] || continue;
          ditto -c -k --keepParent "$f" "${f}.zip";
          xcrun notarytool submit "${f}.zip" --apple-id "{{ .Env.APPLE_ID }}" --password "{{ .Env.APPLE_PASSWORD }}" --team-id "{{ .Env.APPLE_TEAM_ID }}" --wait;
          rm "${f}.zip";
        done
        '
```

**Note:** The `signs` block uses `ids: [relay-darwin]` to restrict codesign to macOS binaries only. Notarization runs as a post-hook on the signed darwin artifacts.

- [ ] **Step 2: Verify goreleaser config (dry run)**

```bash
goreleaser check
```

Expected: config is valid. (Install goreleaser first if needed: `brew install goreleaser`)

- [ ] **Step 3: Test local build**

```bash
goreleaser build --snapshot --clean
```

Expected: Binaries for all 5 targets in `dist/`.

- [ ] **Step 4: Commit**

```bash
git add .goreleaser.yml
git commit -m "build: add goreleaser config for cross-platform relay binary"
```

---

### Task 9: CI Go Build Step

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add Go build verification to CI**

Add a new job after `ci-gate` in `.github/workflows/ci.yml`:

```yaml
  go-build:
    name: go-build
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v5

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: "1.22"
          cache-dependency-path: cli/go.sum

      - name: Test
        run: cd cli && go test -v ./...

      - name: Build (all targets)
        run: |
          cd cli
          GOOS=darwin GOARCH=arm64 go build -o /dev/null .
          GOOS=darwin GOARCH=amd64 go build -o /dev/null .
          GOOS=windows GOARCH=amd64 go build -o /dev/null .
          GOOS=linux GOARCH=amd64 go build -o /dev/null .
          GOOS=linux GOARCH=arm64 go build -o /dev/null .
```

- [ ] **Step 2: Verify CI config syntax**

```bash
cd .github/workflows && python3 -c "import yaml; yaml.safe_load(open('ci.yml'))" && echo "valid YAML"
```

Expected: "valid YAML"

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add Go build + test verification"
```

---

## Chunk 3: Skill Migration & Script Updates

### Task 10: Skill Migration (jq → relay jq)

**Files:**
- Modify: `skills/read/SKILL.md` (6 occurrences)
- Modify: `skills/review/SKILL.md` (8 occurrences)
- Modify: `skills/helper/SKILL.md` (7 occurrences)
- Modify: `skills/fallback/SKILL.md` (4 occurrences)
- Modify: `skills/entity-discovery/SKILL.md` (3 occurrences)
- Modify: `skills/write/SKILL.md` (1 occurrence)
- Modify: `skills/review/checks.md` (1 occurrence)
- Modify: `skills/ha-nova/relay-api.md` (1 occurrence)
- Modify: `skills/ha-nova/safe-refactoring.md` (1 occurrence)
- Modify: `skills/ha-nova/agents/resolve-agent.md` (1 example text)

- [ ] **Step 1: Perform the replacement across all skill files**

In each file, replace all occurrences of:
- `| jq ` → `| ~/.config/ha-nova/relay jq `
- `| jq -r ` → `| ~/.config/ha-nova/relay jq -r `

Exact pattern for each file (the pipe-to-jq pattern):

**Pattern A** (inline pipe): `| jq '...` → `| ~/.config/ha-nova/relay jq '...`
**Pattern B** (inline pipe with -r): `| jq -r '...` → `| ~/.config/ha-nova/relay jq -r '...`
**Pattern C** (resolve-agent example text): `` `jq '[.data. `` → `` `~/.config/ha-nova/relay jq '[.data. ``

Use sed for the bulk replacement:

```bash
# Pattern A + B: pipe-to-jq in skills
find skills -name '*.md' -exec sed -i '' \
  -e 's|| jq |~/.config/ha-nova/relay jq |g' \
  -e "s|| jq -r |~/.config/ha-nova/relay jq -r |g" \
  {} +
```

**Important:** Verify no false positives. The only `jq` references in skills are pipe-to-jq patterns. There's one backtick-wrapped example in `resolve-agent.md` (line 50) that needs manual editing:

In `skills/ha-nova/agents/resolve-agent.md:50`, change:
```
Example: `jq '[.data.entities[]
```
to:
```
Example: `~/.config/ha-nova/relay jq '[.data.entities[]
```

**Standalone jq calls** (no pipe — these validate JSON files directly):

In `skills/read/SKILL.md`, there are 2 standalone `jq` calls that must also be converted:

Line ~75: `jq -e 'type == "object"' /tmp/ha-config-{slug}.json > /dev/null`
→ `cat /tmp/ha-config-{slug}.json | ~/.config/ha-nova/relay jq -e 'type == "object"' > /dev/null`

Line ~143: `jq empty /tmp/ha-trace-{run_id}.json`
→ `cat /tmp/ha-trace-{run_id}.json | ~/.config/ha-nova/relay jq 'empty'`

**Note:** The `relay jq` subcommand reads from stdin only. File-argument jq calls must be converted to `cat file | relay jq`.

Also add `-e` flag support to `cli/jq.go` (exit 1 if output is false/null). See Task 6 Step 4 — add this flag alongside `-r`.

- [ ] **Step 2: Verify the count matches**

```bash
grep -r '| jq ' skills/ | wc -l
```

Expected: 0 (all replaced).

```bash
grep -r 'relay jq' skills/ | wc -l
```

Expected: 32 (all occurrences now use relay jq).

- [ ] **Step 3: Run existing vitest skill tests**

```bash
npm test -- --run tests/skills/
```

Expected: Some assertion failures (tests expect `jq`, now see `relay jq`). Note which tests fail — they'll be fixed in Task 13.

- [ ] **Step 4: Commit**

```bash
git add skills/
git commit -m "feat(skills): migrate jq calls to relay jq (32 occurrences)"
```

---

### Task 11: Update Installer Scripts (Binary Download)

**Files:**
- Modify: `scripts/onboarding/install-local-skills.sh:225,246-251`
- Modify: `scripts/update.sh:323`
- Modify: `install.sh:233`

- [ ] **Step 1: Update install-local-skills.sh**

At line 225, change:
```bash
  local relay_cli_source="${REPO_ROOT}/scripts/relay.sh"
```
to:
```bash
  local relay_cli_target="${HOME}/.config/ha-nova/relay"
```

Replace lines 246-251 (the relay copy block):
```bash
  if [[ -f "${relay_cli_source}" ]]; then
    mkdir -p "${HOME}/.config/ha-nova"
    cp "${relay_cli_source}" "${relay_cli_target}"
    chmod 755 "${relay_cli_target}"
    log "[${target}] Installed relay CLI: ${relay_cli_target}"
  fi
```
with:
```bash
  # Download pre-built relay binary from GitHub Releases
  if [[ ! -x "${relay_cli_target}" ]] || [[ "${FORCE_RELAY_UPDATE:-}" == "1" ]]; then
    mkdir -p "${HOME}/.config/ha-nova"
    local os_name arch_name
    os_name="$(uname -s | tr '[:upper:]' '[:lower:]')"
    arch_name="$(uname -m)"
    case "$arch_name" in
      x86_64)        arch_name="amd64" ;;
      aarch64|arm64) arch_name="arm64" ;;
    esac
    local version
    version="$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${REPO_ROOT}/version.json" | head -1)"
    # Go binary releases are tagged alongside skill_version (same GitHub release).
    # If the binary needs its own release cadence later, add a cli_version field to version.json.
    local download_url="https://github.com/markusleben/ha-nova/releases/download/v${version}/relay-${os_name}-${arch_name}"
    log "[${target}] Downloading relay CLI v${version}..."
    if curl -fsSL "${download_url}" -o "${relay_cli_target}"; then
      chmod 755 "${relay_cli_target}"
      log "[${target}] Installed relay CLI: ${relay_cli_target}"
    else
      log "[${target}] Warning: could not download relay binary. Skills will not work until relay CLI is installed."
    fi
  fi
```

- [ ] **Step 2: Update update.sh line 323**

Change:
```bash
  [[ -f "${src}/scripts/relay.sh" ]]        && cp "${src}/scripts/relay.sh" "${CONFIG_DIR}/relay" && chmod 755 "${CONFIG_DIR}/relay"
```
to:
```bash
  # Download updated relay binary
  local os_name arch_name version download_url
  os_name="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch_name="$(uname -m)"
  case "$arch_name" in
    x86_64)        arch_name="amd64" ;;
    aarch64|arm64) arch_name="arm64" ;;
  esac
  version="$(sed -n 's/.*"skill_version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${src}/version.json" | head -1)"
  download_url="https://github.com/markusleben/ha-nova/releases/download/v${version}/relay-${os_name}-${arch_name}"
  if curl -fsSL "${download_url}" -o "${CONFIG_DIR}/relay"; then
    chmod 755 "${CONFIG_DIR}/relay"
  fi
```

- [ ] **Step 3: Update install.sh line 233**

Apply the same binary download pattern as install-local-skills.sh (replacing the `cp` of relay.sh).

- [ ] **Step 4: Commit**

```bash
git add scripts/onboarding/install-local-skills.sh scripts/update.sh install.sh
git commit -m "feat(scripts): download Go relay binary instead of copying relay.sh"
```

---

### Task 11b: Update dev-sync.sh

**Files:**
- Modify: `scripts/dev-sync.sh`

- [ ] **Step 1: Find relay.sh reference in dev-sync.sh**

```bash
grep -n "relay.sh" scripts/dev-sync.sh
```

- [ ] **Step 2: Update sync_shared_tools() to build Go binary locally**

Replace the `cp scripts/relay.sh` line with:

```bash
  # Build relay binary from local Go source (dev workflow — no GitHub download)
  if command -v go &>/dev/null && [[ -d "${REPO_ROOT}/cli" ]]; then
    (cd "${REPO_ROOT}/cli" && go build -o "${CONFIG_DIR}/relay" .)
    chmod 755 "${CONFIG_DIR}/relay"
    log "Built and deployed relay CLI from local Go source"
  else
    log "Warning: Go not installed or cli/ missing — relay CLI not updated"
  fi
```

**Rationale:** dev-sync is for local development. It should build from source (fast, ~2s) rather than downloading a release binary.

- [ ] **Step 3: Commit**

```bash
git add scripts/dev-sync.sh
git commit -m "fix(dev-sync): build Go relay binary from local source"
```

---

### Task 12: Remove relay.sh

**Files:**
- Delete: `scripts/relay.sh`

- [ ] **Step 1: Remove relay.sh**

```bash
trash scripts/relay.sh
```

- [ ] **Step 2: Verify no dangling references in non-test code**

```bash
grep -r "relay\.sh" scripts/ install.sh skills/ --include='*.sh' --include='*.md' | grep -v 'lib/relay\.sh' | grep -v node_modules
```

Expected: 0 results. Note: `scripts/onboarding/lib/relay.sh` (onboarding probe library) is a different file and stays.

- [ ] **Step 3: Commit**

```bash
git add -u scripts/relay.sh
git commit -m "refactor: remove scripts/relay.sh (replaced by Go binary)"
```

---

## Chunk 4: Test Updates

### Task 13: Update TypeScript Tests

**Files:**
- Modify: `tests/onboarding/relay-cli-contract.test.ts`
- Modify: `tests/skills/ha-nova-contract.test.ts:251-256`
- Modify: `tests/onboarding/self-update-contract.test.ts:82`

- [ ] **Step 1: Update relay-cli-contract.test.ts**

Replace the entire test file with:

```typescript
import { existsSync } from "node:fs";
import { spawnSync } from "node:child_process";

import { describe, expect, it } from "vitest";

import { createMockHome, REPO_ROOT } from "./_helpers.js";

describe("relay cli contract", () => {
  it("Go relay binary source exists", () => {
    expect(existsSync("cli/main.go")).toBe(true);
    expect(existsSync("cli/go.mod")).toBe(true);
    expect(existsSync("cli/relay.go")).toBe(true);
    expect(existsSync("cli/config.go")).toBe(true);
    expect(existsSync("cli/jq.go")).toBe(true);
    expect(existsSync("cli/version.go")).toBe(true);
  });

  it("guides the user to setup when onboarding config is missing", () => {
    // Build the binary first
    const build = spawnSync("go", ["build", "-o", "/tmp/ha-nova-relay-test", "."], {
      cwd: `${REPO_ROOT}/cli`,
      encoding: "utf8",
      timeout: 30000,
    });
    if (build.status !== 0) {
      throw new Error(`Go build failed: ${build.stderr}`);
    }

    const home = createMockHome();
    const result = spawnSync(
      "/tmp/ha-nova-relay-test",
      ["health"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 15000,
        env: { ...process.env, HOME: home },
      },
    );

    const output = (result.stdout ?? "") + (result.stderr ?? "");
    expect(result.status).not.toBe(0);
    expect(output).toContain("HA NOVA is not set up yet");
    expect(output).toContain("ha-nova setup");
  });
});
```

This preserves the behavioral contract test (error message on missing config) while testing the actual Go binary.

- [ ] **Step 2: Update ha-nova-contract.test.ts**

At lines 251-256, change the "keeps relay wrapper script present and executable" test:

```typescript
  it("keeps relay Go binary source present", () => {
    expect(existsSync("cli/main.go")).toBe(true);
    expect(existsSync("cli/relay.go")).toBe(true);
  });
```

- [ ] **Step 3: Update skill contract assertions**

In `tests/skills/ha-nova-contract.test.ts`, if any test asserts `| jq ` in skill content, update to assert `relay jq` instead. Search for:

```bash
grep -n "jq" tests/skills/ha-nova-contract.test.ts
```

Update any matching assertions.

- [ ] **Step 4: Update self-update-contract.test.ts line 82**

Change:
```typescript
      expect(updateScript).toContain("relay.sh");
```
to:
```typescript
      expect(updateScript).toContain("relay");  // Go binary download
```

- [ ] **Step 5: Run all tests**

```bash
npm test
```

Expected: All tests pass.

- [ ] **Step 6: Commit**

```bash
git add tests/
git commit -m "test: update assertions for Go relay binary"
```

---

### Task 14: Remove jq Prerequisite Checks

**Files:**
- Modify: `install.sh` (remove jq check if present)
- Modify: `scripts/onboarding/lib/ui.sh` (remove jq check if present)

- [ ] **Step 1: Check if jq prerequisite exists in install.sh**

```bash
grep -n "jq" install.sh
```

If present, remove the jq check block.

- [ ] **Step 2: Check if jq prerequisite exists in ui.sh**

```bash
grep -n "jq" scripts/onboarding/lib/ui.sh
```

If present, remove the jq check block from `check_prerequisites()`.

- [ ] **Step 3: Run tests**

```bash
npm test
```

Expected: All pass.

- [ ] **Step 4: Commit (only if changes were made)**

```bash
git add install.sh scripts/onboarding/lib/ui.sh
git commit -m "chore: remove jq prerequisite checks (bundled in relay binary)"
```

---

### Task 15: Final Verification

- [ ] **Step 1: Run complete Go test suite**

```bash
cd cli && go test -v ./...
```

Expected: All Go tests pass.

- [ ] **Step 2: Run complete vitest suite**

```bash
npm test
```

Expected: All 292+ tests pass.

- [ ] **Step 3: Build all platform binaries**

```bash
cd cli && GOOS=darwin GOARCH=arm64 go build -o /tmp/relay-darwin-arm64 . && echo "darwin/arm64 OK"
cd cli && GOOS=windows GOARCH=amd64 go build -o /tmp/relay-windows-amd64.exe . && echo "windows/amd64 OK"
cd cli && GOOS=linux GOARCH=amd64 go build -o /tmp/relay-linux-amd64 . && echo "linux/amd64 OK"
```

Expected: All 3 compile successfully.

- [ ] **Step 4: Smoke test on macOS**

```bash
cd cli && go build -o /tmp/relay . && /tmp/relay version
```

Expected: Prints "dev" (Version not set without ldflags).

```bash
echo '{"data":[1,2,3]}' | /tmp/relay jq '.data[]'
```

Expected: `1\n2\n3`

- [ ] **Step 5: Verify skill jq migration is complete**

```bash
grep -r '| jq ' skills/ | grep -v 'relay jq' | wc -l
```

Expected: 0

- [ ] **Step 6: Verify no relay.sh references remain in production code**

```bash
grep -r "relay\.sh" scripts/ install.sh skills/ --include='*.sh' --include='*.md' | grep -v 'lib/relay\.sh' | wc -l
```

Expected: 0 (note: `scripts/onboarding/lib/relay.sh` is the onboarding probe library and stays)

- [ ] **Step 7: Commit final state (if any uncommitted changes)**

```bash
git status
```
