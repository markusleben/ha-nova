package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// isolateTestHome points HOME (and the Windows profile dirs) of this test
// run at an empty dir under testRoot, so no test reaches the developer's
// real home: a test fake once overwrote a real Claude Code plugin registry.
// Tests that write under HOME set their own (t.Setenv("HOME",
// t.TempDir())); TestMain fails the run when anything lands in this shared
// one. Go's own caches and settings stay where they are.
func isolateTestHome(testRoot string) string {
	if output, err := exec.Command("go", "env", "GOCACHE", "GOMODCACHE", "GOPATH", "GOENV").Output(); err == nil {
		for i, value := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if key := []string{"GOCACHE", "GOMODCACHE", "GOPATH", "GOENV"}[i]; os.Getenv(key) == "" && strings.TrimSpace(value) != "" {
				os.Setenv(key, strings.TrimSpace(value))
			}
		}
	}
	home := filepath.Join(testRoot, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		os.Setenv("USERPROFILE", home)
		os.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		os.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "CLAUDE_CONFIG_DIR", "CODEX_HOME"} {
		os.Unsetenv(key)
	}
	return home
}

// sharedHomeWrites: what tests left in the shared test home.
func sharedHomeWrites(home string) []string {
	var written []string
	_ = filepath.WalkDir(home, func(path string, entry os.DirEntry, err error) error {
		if path != home && (err != nil || !entry.IsDir()) {
			written = append(written, path)
		}
		return nil
	})
	return written
}

// setTestHome makes home this test's home on every OS: HOME, and on
// Windows the profile dirs HA NOVA reads there instead.
func setTestHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}
}

// requireTestHome fails the test unless home is a temp dir. Every fake that
// writes Claude Code or HA NOVA state checks its home with it first.
func requireTestHome(t *testing.T, home string) {
	t.Helper()
	if err := testHomeError(home); err != nil {
		t.Fatal(err)
	}
}

func testHomeError(home string) error {
	tmp, tmpErr := filepath.EvalSymlinks(os.TempDir())
	resolved, err := filepath.EvalSymlinks(home)
	if tmpErr != nil || err != nil || !strings.HasPrefix(resolved, tmp+string(filepath.Separator)) {
		return fmt.Errorf("test home %q is not a temp dir: test fakes never write outside one", home)
	}
	return nil
}

func TestTestFakesRefuseHomesOutsideTemp(t *testing.T) {
	if err := testHomeError(t.TempDir()); err != nil {
		t.Fatalf("temp home refused: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, home := range []string{wd, filepath.Dir(wd), string(filepath.Separator)} {
		if testHomeError(home) == nil {
			t.Fatalf("home %q outside the temp dir accepted", home)
		}
	}
	if code := runFakeClaude(t.TempDir(), []string{"plugin", "install", "ha-nova@ha-nova"}); code == 0 || os.Getenv(fakeClaudeHomeEnv) != "" {
		t.Fatalf("fake claude without a test home exit = %d, want a refusal", code)
	}
}
