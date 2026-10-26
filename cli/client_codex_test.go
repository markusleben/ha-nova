package main

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func withCodexAppInstalled(t *testing.T, installed bool) {
	t.Helper()
	original := codexAppInstalled
	codexAppInstalled = func(string) bool { return installed }
	t.Cleanup(func() { codexAppInstalled = original })
}

// codexAppOnlyInstall: an HA NOVA install where Codex is only the desktop
// app: no codex command on PATH, real runtime detection.
func codexAppOnlyInstall(t *testing.T, app bool) runtimePaths {
	t.Helper()
	paths := setupHealableInstall(t)
	t.Setenv("PATH", t.TempDir())
	clientRuntimeDetectedForStatus = clientRuntimeDetected
	withCodexAppInstalled(t, app)
	return paths
}

func TestCodexDesktopAppCountsAsCodex(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	withCodexAppInstalled(t, false)
	if clientRuntimeDetected("codex") {
		t.Fatal("no codex command and no Codex app, yet Codex counted as found")
	}
	withCodexAppInstalled(t, true)
	if !clientRuntimeDetected("codex") {
		t.Fatal("the Codex desktop app alone must count as Codex")
	}
}

func TestCodexAppInRecognizesOnlyTheCodexBundle(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("plutil and app bundles are macOS only")
	}
	dir := t.TempDir()
	plist := func(name, id string) {
		writeBundleTestFile(t, filepath.Join(dir, name, "Contents", "Info.plist"),
			`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>CFBundleIdentifier</key><string>`+id+`</string></dict></plist>
`, 0o644)
	}
	plist("ChatGPT.app", "com.openai.chat")
	if codexAppIn(dir, filepath.Join(dir, "missing")) {
		t.Fatal("the classic ChatGPT app counted as Codex")
	}
	plist("ChatGPT.app", "com.openai.codex")
	if !codexAppIn(filepath.Join(dir, "missing"), dir) {
		t.Fatal("ChatGPT.app with the Codex bundle id is the Codex app")
	}
}

func TestCodexMissingReasonNamesTheCLI(t *testing.T) {
	reason := clientStatusReason("codex", clientStatus{SupportedOnOS: true})
	if !strings.HasPrefix(reason, "install the Codex CLI") {
		t.Fatalf("reason = %q", reason)
	}
	if strings.Contains(reason, "ChatGPT desktop app") != (runtime.GOOS == "darwin") {
		t.Fatalf("only a Mac detects the desktop app, so only there may the reason offer it: %q", reason)
	}
}

func TestSetupCodexInstallsSkillsForTheDesktopAppAlone(t *testing.T) {
	paths := codexAppOnlyInstall(t, true)
	if _, err := installClient(paths, paths.InstallRoot, "codex"); err != nil {
		t.Fatalf("install for the Codex app: %v", err)
	}
	entry, _, err := findRegistryClient(paths, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if status := evaluateClientStatus(paths, installState{}, entry); !status.Ready {
		t.Fatalf("Codex app with HA NOVA skills must be ready: %+v", status)
	}
}

func TestSetupCodexWithoutCommandOrAppSaysWhatToInstall(t *testing.T) {
	paths := codexAppOnlyInstall(t, false)
	_, err := installClient(paths, paths.InstallRoot, "codex")
	if err == nil || !strings.Contains(err.Error(), "is not available yet: install the Codex CLI") {
		t.Fatalf("err = %v", err)
	}
}

func TestSetupCodexRemovesOnlyHANovasOwnOldFlatCopies(t *testing.T) {
	paths := codexAppOnlyInstall(t, true)
	skills := codexSkillsRoot(paths.Home)
	own := filepath.Join(skills, "ha-nova-read")
	writeBundleTestFile(t, filepath.Join(own, "SKILL.md"), "---\nname: ha-nova-read\n---\n", 0o644)
	writeBundleTestFile(t, filepath.Join(own, "notes.md"), "# Notes\n", 0o644)
	retired := filepath.Join(skills, "ha-nova-guide")
	writeBundleTestFile(t, filepath.Join(retired, "SKILL.md"), "---\nname: ha-nova-guide\n---\n", 0o644)
	withUserFile := filepath.Join(paths.Home, "elsewhere", "ha-nova-read")
	writeBundleTestFile(t, filepath.Join(withUserFile, "SKILL.md"), "---\nname: ha-nova-read\n---\n", 0o644)
	keep := []string{
		filepath.Join(skills, "my-skill", "SKILL.md"),
		filepath.Join(paths.Home, ".codex", "skills", "ha-nova", "ha-nova", "SKILL.md"),
	}
	for _, path := range keep {
		writeBundleTestFile(t, path, "---\nname: ha-nova\n---\n", 0o644)
	}

	if err := installFileClientsForRepair(paths, &installState{}, []string{"codex"}); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !fileExists(own) {
		t.Fatal("a repair (update, session start) removed an old copy; only setup does")
	}
	if _, err := installClient(paths, paths.InstallRoot, "codex"); err != nil {
		t.Fatalf("setup codex: %v", err)
	}
	for _, gone := range []string{own, retired} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Fatalf("HA NOVA's own old copy %s is still there: %v", gone, err)
		}
	}
	for _, path := range keep {
		if !fileExists(path) {
			t.Fatalf("setup removed %s, which isn't HA NOVA's old copy", path)
		}
	}

	// A copy that also holds a file of the user's, or is a link, stays.
	writeBundleTestFile(t, filepath.Join(own, "SKILL.md"), "---\nname: ha-nova-read\n---\n", 0o644)
	writeBundleTestFile(t, filepath.Join(own, "mine.txt"), "keep\n", 0o600)
	if err := os.Symlink(withUserFile, retired); err != nil {
		t.Fatal(err)
	}
	if _, err := installClient(paths, paths.InstallRoot, "codex"); err != nil {
		t.Fatalf("setup codex again: %v", err)
	}
	if !fileExists(filepath.Join(own, "mine.txt")) || !fileExists(filepath.Join(retired, "SKILL.md")) {
		t.Fatal("setup removed a folder that isn't provably HA NOVA's own")
	}
}

func TestCodexSkillProblemsFindsOldAndSecondCopies(t *testing.T) {
	paths := codexAppOnlyInstall(t, true)
	t.Setenv("CODEX_HOME", "")
	if _, err := installClient(paths, paths.InstallRoot, "codex"); err != nil {
		t.Fatalf("setup codex: %v", err)
	}
	if problems := codexSkillProblems(paths); len(problems) != 0 {
		t.Fatalf("a fresh install has no problems: %+v", problems)
	}

	tree := filepath.Join(codexSkillsRoot(paths.Home), "ha-nova")
	if err := installTreeCopy(tree, filepath.Join(paths.InstallRoot, "skills")); err != nil {
		t.Fatal(err)
	}
	if problems := codexSkillProblems(paths); len(problems) != 0 {
		t.Fatalf("an exact copy (Windows) is current: %+v", problems)
	}
	writeBundleTestFile(t, filepath.Join(tree, "read", "SKILL.md"), "---\nname: read\n---\n\nOld text.\n", 0o644)
	if got := codexSkillProblemKinds(paths); !slices.Equal(got, []string{"outdated"}) {
		t.Fatalf("changed copy: kinds = %v", got)
	}
	if err := installTreeCopy(tree, filepath.Join(paths.InstallRoot, "skills")); err != nil {
		t.Fatal(err)
	}
	writeBundleTestFile(t, filepath.Join(tree, "retired", "SKILL.md"), "---\nname: retired\n---\n", 0o644)
	if got := codexSkillProblemKinds(paths); !slices.Equal(got, []string{"outdated"}) {
		t.Fatalf("copy with a skill this release dropped: kinds = %v", got)
	}
	if err := installTreeCopy(tree, filepath.Join(paths.InstallRoot, "skills")); err != nil {
		t.Fatal(err)
	}

	writeBundleTestFile(t, filepath.Join(codexSkillsRoot(paths.Home), "ha-nova-read", "SKILL.md"), "---\nname: ha-nova-read\n---\n", 0o644)
	writeBundleTestFile(t, filepath.Join(paths.Home, ".codex", "skills", "ha-nova", "ha-nova", "SKILL.md"), "---\nname: ha-nova\n---\n", 0o644)
	writeBundleTestFile(t, filepath.Join(paths.Home, ".codex", "skills", "ha-nova-notes", "SKILL.md"), "---\nname: my-notes\n---\n", 0o644)
	codexHome := filepath.Join(paths.Home, "codex-work")
	t.Setenv("CODEX_HOME", codexHome)
	writeBundleTestFile(t, filepath.Join(codexHome, "skills", "ha-nova-read", "SKILL.md"), "---\nname: ha-nova-read\n---\n", 0o644)

	problems := codexSkillProblems(paths)
	want := []codexSkillProblem{
		{Path: filepath.Join(codexSkillsRoot(paths.Home), "ha-nova-read"), Kind: "legacy"},
		{Path: filepath.Join(paths.Home, ".codex", "skills", "ha-nova"), Kind: "extra"},
		{Path: filepath.Join(codexHome, "skills", "ha-nova-read"), Kind: "extra"},
	}
	if !slices.Equal(problems, want) {
		t.Fatalf("problems = %+v\nwant %+v", problems, want)
	}
	for _, problem := range problems {
		warning, fix := doctorCodexSkillProblem(problem)
		if !strings.Contains(warning, problem.Path) || !strings.HasPrefix(fix, "To fix: ") {
			t.Fatalf("%s: %q / %q", problem.Kind, warning, fix)
		}
		if (problem.Kind == "legacy") != strings.Contains(fix, "ha-nova setup codex") {
			t.Fatalf("%s: only setup's own cleanup may be offered as the fix: %q", problem.Kind, fix)
		}
	}
}

func installTreeCopy(target, source string) error {
	_, err := installTreeClient(filepath.Dir(target), source, false)
	return err
}

func codexSkillProblemKinds(paths runtimePaths) []string {
	kinds := []string{}
	for _, problem := range codexSkillProblems(paths) {
		kinds = append(kinds, problem.Kind)
	}
	return kinds
}
