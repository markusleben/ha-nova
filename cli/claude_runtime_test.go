package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func desktopClaudeTestHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake executable fixtures require a POSIX host")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	old := claudeDesktopPlatformOS
	claudeDesktopPlatformOS = "darwin"
	t.Cleanup(func() { claudeDesktopPlatformOS = old })
	bin := t.TempDir()
	for _, tool := range []string{"bash", "cat", "mkdir", "rm"} {
		if err := os.Symlink(filepath.Join("/bin", tool), filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return home
}

func writeDesktopClaude(t *testing.T, home, version, body string) string {
	t.Helper()
	dir := filepath.Join(home, "Library", "Application Support", "Claude", "claude-code", version, "a93ded4b1b47")
	path := filepath.Join(dir, "claude.app", "Contents", "MacOS", "claude")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".verified"), []byte("a93ded4b1b47"+strings.Repeat("0", 52)), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(path+".log") + "\n" +
		"case \"$*\" in\n" +
		"  --version) echo '" + version + " (Claude Code)'; exit 0 ;;\n" +
		"  'plugin --help') echo 'install update remove list validate'; exit 0 ;;\n" +
		"  'plugin marketplace --help') echo 'add remove update'; exit 0 ;;\n" +
		"esac\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDesktopClaudeNewestWorkingRuntime(t *testing.T) {
	home := desktopClaudeTestHome(t)
	writeDesktopClaude(t, home, "2.1.9", "exit 1")
	want := writeDesktopClaude(t, home, "2.1.100", "exit 1")
	broken := writeDesktopClaude(t, home, "2.1.101", "exit 1")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := claudeRuntimePath(); err != nil || got != want {
		t.Fatalf("runtime = %q, %v; want %q", got, err, want)
	}
	if !clientRuntimeDetected("claude") {
		t.Fatal("Desktop runtime wasn't detected")
	}
}

func TestDesktopClaudeRejectsIncompleteAndUnsupportedRuntimes(t *testing.T) {
	for _, kind := range []string{"marker", "checksum", "executable", "version", "plugins", "marketplace"} {
		t.Run(kind, func(t *testing.T) {
			home := desktopClaudeTestHome(t)
			path := writeDesktopClaude(t, home, "2.1.308", "exit 1")
			marker := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(path)))), ".verified")
			var err error
			switch kind {
			case "marker":
				err = os.Rename(marker, marker+".incomplete")
			case "checksum":
				err = os.WriteFile(marker, []byte(strings.Repeat("0", 64)), 0o644)
			case "executable":
				err = os.Chmod(path, 0o644)
			default:
				data, readErr := os.ReadFile(path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				old, replacement := "2.1.308 (Claude Code)", "2.1.299 (Claude Code)"
				if kind == "plugins" {
					old, replacement = "install update remove list validate", "not supported"
				} else if kind == "marketplace" {
					old, replacement = "add remove update", "not supported"
				}
				err = os.WriteFile(path, []byte(strings.ReplaceAll(string(data), old, replacement)), 0o755)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := claudeRuntimePath(); err == nil || clientRuntimeDetected("claude") {
				t.Fatal("unusable Desktop runtime counted as available")
			}
		})
	}
}

func TestDesktopClaudePATHAlwaysWinsIncludingCommandFailure(t *testing.T) {
	home := desktopClaudeTestHome(t)
	bundled := writeDesktopClaude(t, home, "2.1.308", "exit 0")
	path := filepath.Join(os.Getenv("PATH"), "claude")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s' \"$CLAUDE_CONFIG_DIR\"\nexit 17\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "terminal profile"))
	if got, err := claudeRuntimePath(); err != nil || got != path {
		t.Fatalf("runtime = %q, %v; want PATH %q", got, err, path)
	}
	output, err := claudeCommand("plugin", "install", "ha-nova@ha-nova").CombinedOutput()
	if err == nil || string(output) != os.Getenv("CLAUDE_CONFIG_DIR") {
		t.Fatalf("PATH command = %q, %v", output, err)
	}
	if log := mockInvocations(t, bundled+".log"); log != "" {
		t.Fatalf("bundled binary ran despite a PATH runtime: %s", log)
	}
}

func TestDesktopClaudeOtherPlatformsDoNotSearchMacBundles(t *testing.T) {
	home := desktopClaudeTestHome(t)
	path := writeDesktopClaude(t, home, "2.1.308", "exit 0")
	for _, platform := range []string{"linux", "windows"} {
		claudeDesktopPlatformOS = platform
		if _, err := claudeRuntimePath(); err == nil {
			t.Fatalf("Mac bundle detected on %s", platform)
		}
	}
	if log := mockInvocations(t, path+".log"); log != "" {
		t.Fatalf("Mac binary ran on another platform: %s", log)
	}
}

func TestDesktopClaudeUserLocalRuntimeStillWins(t *testing.T) {
	home := desktopClaudeTestHome(t)
	bundled := writeDesktopClaude(t, home, "2.1.308", "exit 0")
	local := filepath.Join(home, ".local", "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("#!/bin/sh\necho user-local\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := claudeRuntimePath(); err != nil || got != local || !clientRuntimeDetected("claude") {
		t.Fatalf("user-local runtime = %q, %v; want %q", got, err, local)
	}
	if output, err := claudeCommand("--version").Output(); err != nil || string(output) != "user-local\n" {
		t.Fatalf("user-local command = %q, %v", output, err)
	}
	if log := mockInvocations(t, bundled+".log"); log != "" {
		t.Fatalf("bundle probed despite user-local runtime: %s", log)
	}
}

func TestDesktopClaudeRuntimeProbeHasDeadline(t *testing.T) {
	home := desktopClaudeTestHome(t)
	path := writeDesktopClaude(t, home, "2.1.308", "exit 1")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec /bin/sleep 20\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := claudeRuntimePath(); err == nil {
		t.Fatal("hung runtime counted as available")
	}
	if time.Since(start) > 6*time.Second {
		t.Fatal("runtime probe didn't respect its deadline")
	}
}

func TestDesktopClaudeTerminalUsesQuotedBundledBinary(t *testing.T) {
	for _, profile := range []string{"", "custom", "relative"} {
		t.Run(profile, func(t *testing.T) {
			home := desktopClaudeTestHome(t)
			if runtime.GOOS != "darwin" {
				t.Skip("AppleScript handoff is macOS-only")
			}
			root := ""
			if profile == "custom" {
				root = filepath.Join(home, `profile with ' and " and $`)
			} else if profile == "relative" {
				root = "relative profile"
			}
			t.Setenv("CLAUDE_CONFIG_DIR", root)
			path := writeDesktopClaude(t, home, "2.1.308", "exit 1")
			log := filepath.Join(home, "terminal.log")
			stub := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + shellQuote(log) + "\n"
			if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "osascript"), []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
			paths, err := detectPaths()
			if err != nil {
				t.Fatal(err)
			}
			if err := openClaudeTerminal(paths); err != nil {
				t.Fatal(err)
			}
			got := mockInvocations(t, log)
			if !strings.Contains(got, `quoted form of "`+path+`"`) || strings.Contains(got, "&& claude") {
				t.Fatalf("terminal didn't quote bundled binary: %s", got)
			}
			if root == "" {
				if strings.Contains(got, "CLAUDE_CONFIG_DIR") {
					t.Fatalf("default profile gained an explicit override: %s", got)
				}
				return
			}
			abs, err := filepath.Abs(root)
			if err != nil {
				t.Fatal(err)
			}
			quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(abs)
			if !strings.Contains(got, `"env " & quoted form of "CLAUDE_CONFIG_DIR=`+quoted+`"`) {
				t.Fatalf("terminal lost the absolute profile override: %s", got)
			}
		})
	}
}

func TestDesktopClaudeLinuxTerminalUsesResolvedRuntime(t *testing.T) {
	home := desktopClaudeTestHome(t)
	if runtime.GOOS != "linux" {
		t.Skip("Linux terminal handoff")
	}
	claudeDesktopPlatformOS = "linux"
	local := filepath.Join(home, ".local", "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(home, "terminal.log")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$@\" \"$CLAUDE_CONFIG_DIR\" > " + shellQuote(log) + "\n"
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "x-terminal-emulator"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "relative profile")
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := openClaudeTerminal(paths); err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("relative profile")
	want := "-e\n" + local + "\n" + abs + "\n"
	for start := time.Now(); time.Since(start) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
		if got := mockInvocations(t, log); got == want {
			return
		}
	}
	t.Fatalf("terminal did not receive the runtime and profile: %s", mockInvocations(t, log))
}

func TestDesktopClaudeInstallsAndVerifiesSelectedProfile(t *testing.T) {
	for _, custom := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "custom"}[custom], func(t *testing.T) {
			home := desktopClaudeTestHome(t)
			root := filepath.Join(home, ".claude")
			if custom {
				root = filepath.Join(home, "Desktop Code profile")
				t.Setenv("CLAUDE_CONFIG_DIR", root)
			}
			paths, err := detectPaths()
			if err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(home, "mutations.log")
			mock := filepath.Join(installClaudeMarketplaceMock(t, logPath, ""), "claude")
			data, err := os.ReadFile(mock)
			if err != nil {
				t.Fatal(err)
			}
			data = []byte(strings.ReplaceAll(string(data), "${HOME}/.claude/plugins", "${CLAUDE_CONFIG_DIR:-${HOME}/.claude}/plugins"))
			if err := os.WriteFile(mock, data, 0o755); err != nil {
				t.Fatal(err)
			}
			writeDesktopClaude(t, home, "2.1.308", "exec "+shellQuote(mock)+" \"$@\"")
			writeClaudeMarketplaceFixture(t, paths.InstallRoot)
			if err := installClaudePlugin(paths, paths.InstallRoot); err != nil {
				t.Fatal(err)
			}
			if !inspectClaudeInstallSnapshot(paths, loadStateOrDefault(paths)).Attached {
				t.Fatal("installed bundle wasn't verified in the selected profile")
			}
			if !fileExists(filepath.Join(root, "plugins", "installed_plugins.json")) {
				t.Fatal("plugin didn't land in the selected config root")
			}
			if custom && fileExists(filepath.Join(home, ".claude")) {
				t.Fatal("custom-profile install wrote to the default profile")
			}
			for _, want := range []string{"plugin validate ", "plugin marketplace add ", "plugin install ha-nova@ha-nova"} {
				if log := mockInvocations(t, logPath); !strings.Contains(log, want) {
					t.Fatalf("missing bundled operation %q in %s", want, log)
				}
			}
		})
	}
}

func TestDesktopClaudePluginListUsesBundleAndInheritedProfile(t *testing.T) {
	home := desktopClaudeTestHome(t)
	root := filepath.Join(home, "custom profile")
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	path := writeDesktopClaude(t, home, "2.1.308", "[ \"$CLAUDE_CONFIG_DIR\" = "+shellQuote(root)+" ] || exit 1\nprintf '[{\"id\":\"ha-nova@ha-nova\",\"enabled\":true}]'\n")
	if found, usable, ok := readClaudePluginInstallFromCLI(home); !found || !usable || !ok {
		t.Fatalf("bundled plugin list = %v, %v, %v", found, usable, ok)
	}
	if !strings.Contains(mockInvocations(t, path+".log"), "plugin list --json") {
		t.Fatal("verification didn't use bundled runtime")
	}
}

func TestDesktopClaudePrivateConfigWarningDoesNotDecryptOrRedirect(t *testing.T) {
	home := desktopClaudeTestHome(t)
	path := filepath.Join(home, "Library", "Application Support", "Claude", "ccd-environment-config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{"", "{}", `{"envVars":""}`, `{"envVars":"private-encrypted-value"}`, "{torn"} {
		if data != "" {
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		got := claudeDesktopConfigHint(home)
		wantHint := strings.Contains(data, "private-") || data == "{torn"
		if (got != "") != wantHint {
			t.Fatalf("hint for %q = %q", data, got)
		}
		if wantHint && (!strings.Contains(got, "CLAUDE_CONFIG_DIR") || !strings.Contains(got, "ha-nova setup claude") || strings.Contains(got, "private-encrypted-value")) {
			t.Fatalf("unsafe or unactionable hint: %s", got)
		}
		if got := claudeConfigRoot(home); got != filepath.Join(home, ".claude") {
			t.Fatalf("Desktop settings redirected the terminal profile to %q", got)
		}
	}
	claudeDesktopPlatformOS = "windows"
	if got := claudeDesktopConfigHint(home); got != "" {
		t.Fatalf("Mac-only hint on Windows: %s", got)
	}
}
