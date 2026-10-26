package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func withCodexAppInstalled(t *testing.T, installed func(home string) bool) {
	t.Helper()
	original := codexAppInstalled
	codexAppInstalled = installed
	t.Cleanup(func() { codexAppInstalled = original })
}

func withCodexPlatform(t *testing.T, osName string) {
	t.Helper()
	original := codexPlatformOS
	codexPlatformOS = osName
	t.Cleanup(func() { codexPlatformOS = original })
}

// codexAppOnlyInstall: an HA NOVA install where Codex is only the desktop
// app in this HOME: no codex command on PATH, real runtime detection.
func codexAppOnlyInstall(t *testing.T, app bool) runtimePaths {
	t.Helper()
	paths := setupHealableInstall(t)
	t.Setenv("PATH", t.TempDir())
	clientRuntimeDetectedForStatus = clientRuntimeDetected
	withCodexAppInstalled(t, func(home string) bool { return app && home == paths.Home })
	return paths
}

func codexStatus(t *testing.T, paths runtimePaths) clientStatus {
	t.Helper()
	entry, ok, err := findRegistryClient(paths, "codex")
	if err != nil || !ok {
		t.Fatalf("codex registry entry: ok=%v err=%v", ok, err)
	}
	return evaluateClientStatus(paths, installState{}, entry)
}

func writeSkill(t *testing.T, dir, skillMD string, extra ...string) {
	t.Helper()
	writeBundleTestFile(t, filepath.Join(dir, "SKILL.md"), skillMD, 0o644)
	for _, name := range extra {
		writeBundleTestFile(t, filepath.Join(dir, name), "extra\n", 0o644)
	}
}

func TestCodexDesktopAppCountsAsCodexInThisHome(t *testing.T) {
	if codexStatus(t, codexAppOnlyInstall(t, false)).RuntimeDetected {
		t.Fatal("no codex command and no Codex app, yet Codex counted as found")
	}
	paths := codexAppOnlyInstall(t, true)
	if !codexStatus(t, paths).RuntimeDetected {
		t.Fatal("the Codex desktop app alone must count as Codex")
	}
	other := paths
	other.Home = t.TempDir()
	if codexStatus(t, other).RuntimeDetected {
		t.Fatal("detection must look at the setup's HOME, not the process's")
	}
}

func TestCodexAppInRecognizesOnlyTheCodexBundle(t *testing.T) {
	if _, err := os.Stat("/usr/bin/plutil"); err != nil {
		t.Skip("plutil and app bundles are macOS only")
	}
	plist := func(dir, name, id string) {
		writeBundleTestFile(t, filepath.Join(dir, name, "Contents", "Info.plist"),
			"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<plist version=\"1.0\"><dict><key>CFBundleIdentifier</key><string>"+id+"</string></dict></plist>\n", 0o644)
	}
	classic, codex := t.TempDir(), t.TempDir()
	plist(classic, "ChatGPT.app", "com.openai.chat")
	plist(codex, "ChatGPT.app", "com.openai.codex")
	for range 2 { // the second round answers from the per-process cache
		if codexAppIn(classic, filepath.Join(classic, "missing")) {
			t.Fatal("the classic ChatGPT app counted as Codex")
		}
		if !codexAppIn(filepath.Join(codex, "missing"), codex) {
			t.Fatal("ChatGPT.app with the Codex bundle id is the Codex app")
		}
	}
}

func TestCodexCopyNamesTheDesktopAppOnlyWhereItExists(t *testing.T) {
	for _, tc := range []struct {
		os       string
		reason   string
		whereKey string
	}{
		{"darwin", "install the Codex CLI, or the ChatGPT desktop app that includes Codex, first", "setup.codex.where"},
		{"windows", "install the Codex CLI first", "setup.codex.where"},
		{"linux", "install the Codex CLI first", "setup.codex.where_linux"},
	} {
		withCodexPlatform(t, tc.os)
		if got := clientStatusReason("codex", clientStatus{SupportedOnOS: true}, t.TempDir()); got != tc.reason {
			t.Errorf("%s reason = %q", tc.os, got)
		}
		if got := codexWhereKey(); got != tc.whereKey {
			t.Errorf("%s where key = %q", tc.os, got)
		}
		where := setupT(tc.whereKey)
		if !strings.Contains(where, "codex command") || !strings.Contains(where, "IDE extension") {
			t.Errorf("%s where line: %q", tc.os, where)
		}
		if strings.Contains(where, "desktop app") == (tc.os == "linux") {
			t.Errorf("%s: the desktop app exists for macOS and Windows only: %q", tc.os, where)
		}
	}
}

func TestSetupCodexInstallsSkillsForTheDesktopAppAlone(t *testing.T) {
	paths := codexAppOnlyInstall(t, true)
	if _, err := installClient(paths, paths.InstallRoot, "codex"); err != nil {
		t.Fatalf("install for the Codex app: %v", err)
	}
	if status := codexStatus(t, paths); !status.Ready {
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

func setupClientsForTest(t *testing.T, paths runtimePaths, clients ...string) {
	t.Helper()
	state := installState{}
	if err := installClientsAndSaveState(paths, &state, clients, saveState); err != nil {
		t.Fatalf("setup %v: %v", clients, err)
	}
}

func legacyCodexFixture(t *testing.T, paths runtimePaths) (own, retired string) {
	t.Helper()
	skills := codexSkillsRoot(paths.Home)
	own = filepath.Join(skills, "ha-nova-read")
	writeSkill(t, own, "---\nname: ha-nova-read\n---\n", "notes.md")
	retired = filepath.Join(skills, "ha-nova-guide")
	writeSkill(t, retired, "---\nname: ha-nova-guide\n---\n")
	return own, retired
}

func TestOnlyExplicitSetupMovesHANovasOldCodexCopies(t *testing.T) {
	paths := codexAppOnlyInstall(t, true)
	own, retired := legacyCodexFixture(t, paths)

	if err := installFileClientsForRepair(paths, &installState{}, []string{"codex"}); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if _, err := installClient(paths, paths.InstallRoot, "codex"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if outcome := attemptClientAutoRepair(paths, clientStatus{ID: "codex", Label: "Codex", RuntimeDetected: true}); outcome.Err != nil {
		t.Fatalf("auto-repair: %+v", outcome)
	}
	if !fileExists(filepath.Join(own, "notes.md")) || !fileExists(filepath.Join(retired, "SKILL.md")) {
		t.Fatal("a repair, update, or session-start auto-repair touched an old copy; only setup may")
	}

	setupClientsForTest(t, paths, "codex")
	for _, gone := range []string{own, retired} {
		if _, err := os.Lstat(gone); !os.IsNotExist(err) {
			t.Fatalf("HA NOVA's own old copy %s is still there: %v", gone, err)
		}
	}
	backups, _ := filepath.Glob(filepath.Join(paths.ConfigDir, "backups", "codex-legacy-*"))
	if len(backups) != 1 || !fileExists(filepath.Join(backups[0], "ha-nova-read", "notes.md")) ||
		!fileExists(filepath.Join(backups[0], "ha-nova-guide", "SKILL.md")) {
		t.Fatalf("old copies must move into one backup folder, got %v", backups)
	}
	if !codexStatus(t, paths).Ready {
		t.Fatal("Codex must stay ready")
	}
}

func TestSetupKeepsWhatIsntProvablyHANovasOldCodexCopy(t *testing.T) {
	paths := codexAppOnlyInstall(t, true)
	skills := codexSkillsRoot(paths.Home)
	mine := filepath.Join(skills, "ha-nova-read")
	writeSkill(t, mine, "---\nname: ha-nova-read\n---\n", "mine.txt")
	bare := filepath.Join(skills, "ha-nova-guide")
	writeSkill(t, bare, "name: ha-nova-guide\n")
	linked := filepath.Join(paths.Home, "elsewhere", "ha-nova-guide")
	writeSkill(t, linked, "---\nname: ha-nova-guide\n---\n")
	other := filepath.Join(skills, "my-skill", "SKILL.md")
	writeBundleTestFile(t, other, "---\nname: ha-nova-read\n---\n", 0o644)
	deprecated := filepath.Join(paths.Home, ".codex", "skills", "ha-nova", "ha-nova", "SKILL.md")
	writeBundleTestFile(t, deprecated, "---\nname: ha-nova\n---\n", 0o644)

	setupClientsForTest(t, paths, "codex")
	if err := os.RemoveAll(bare); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linked, bare); err != nil {
		t.Fatal(err)
	}
	setupClientsForTest(t, paths, "codex")
	for _, kept := range []string{filepath.Join(mine, "mine.txt"), filepath.Join(bare, "SKILL.md"), other, deprecated} {
		if !fileExists(kept) {
			t.Fatalf("setup moved %s, which isn't provably HA NOVA's old copy", kept)
		}
	}
	if backups, _ := filepath.Glob(filepath.Join(paths.ConfigDir, "backups", "codex-legacy-*")); len(backups) != 0 {
		t.Fatalf("nothing to move, yet backups %v", backups)
	}
}

func TestSetupNameMatchIsFrontmatterOnly(t *testing.T) {
	paths := codexAppOnlyInstall(t, true)
	body := filepath.Join(codexSkillsRoot(paths.Home), "ha-nova-read")
	writeSkill(t, body, "---\nname: notes\n---\n\nname: ha-nova-read\n")
	setupClientsForTest(t, paths, "codex")
	if !fileExists(filepath.Join(body, "SKILL.md")) {
		t.Fatal("a name: line in the body made a folder HA NOVA's")
	}
}

func TestAntigravitySetupMovesOnlyHANovasOldCodexCopies(t *testing.T) {
	paths := setupHealableInstall(t)
	skills := codexSkillsRoot(paths.Home)
	mine := filepath.Join(skills, "ha-nova-read")
	writeSkill(t, mine, "---\nname: ha-nova-read\n---\n", "mine.txt")
	retired := filepath.Join(skills, "ha-nova-guide")
	writeSkill(t, retired, "---\nname: ha-nova-guide\n---\n")

	setupClientsForTest(t, paths, "antigravity")
	if !fileExists(filepath.Join(mine, "mine.txt")) {
		t.Fatal("Antigravity setup removed the user's ha-nova-read")
	}
	if fileExists(retired) {
		t.Fatal("Antigravity setup left HA NOVA's own old ha-nova-guide in ~/.agents/skills")
	}
}

func TestSetupCodexSavesStateWhenOldCopiesCantMove(t *testing.T) {
	paths := codexAppOnlyInstall(t, true)
	own, _ := legacyCodexFixture(t, paths)
	writeBundleTestFile(t, filepath.Join(paths.ConfigDir, "backups"), "not a folder\n", 0o644)

	setupClientsForTest(t, paths, "codex")
	state, err := loadState(paths)
	if err != nil || !slices.Contains(state.InstalledClients, "codex") {
		t.Fatalf("state must record Codex despite the cleanup failure: %+v err=%v", state, err)
	}
	if !fileExists(own) {
		t.Fatal("a failed move must leave the old copy in place")
	}
	if kinds := codexSkillProblemKinds(paths); !slices.Contains(kinds, "legacy") {
		t.Fatalf("doctor must still flag what's left: %v", kinds)
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
	source := filepath.Join(paths.InstallRoot, "skills")
	if _, err := installTreeClient(filepath.Dir(tree), source, false); err != nil {
		t.Fatal(err)
	}
	if problems := codexSkillProblems(paths); len(problems) != 0 {
		t.Fatalf("an exact copy (Windows) is current: %+v", problems)
	}
	writeBundleTestFile(t, filepath.Join(tree, "read", "SKILL.md"), "---\nname: read\n---\n\nOld text.\n", 0o644)
	if got := codexSkillProblemKinds(paths); !slices.Equal(got, []string{"outdated"}) {
		t.Fatalf("changed copy: kinds = %v", got)
	}
	if _, err := installTreeClient(filepath.Dir(tree), source, false); err != nil {
		t.Fatal(err)
	}
	writeBundleTestFile(t, filepath.Join(tree, "retired", "SKILL.md"), "---\nname: retired\n---\n", 0o644)
	if got := codexSkillProblemKinds(paths); !slices.Equal(got, []string{"outdated"}) {
		t.Fatalf("copy with a skill this release dropped: kinds = %v", got)
	}
	if _, err := installTreeClient(filepath.Dir(tree), source, false); err != nil {
		t.Fatal(err)
	}

	writeSkill(t, filepath.Join(codexSkillsRoot(paths.Home), "ha-nova-read"), "---\nname: ha-nova-read\n---\n")
	writeSkill(t, filepath.Join(codexSkillsRoot(paths.Home), "ha-nova-guide"), "---\nname: ha-nova-guide\n---\n", "mine.txt")
	writeBundleTestFile(t, filepath.Join(paths.Home, ".codex", "skills", "ha-nova", "ha-nova", "SKILL.md"), "---\nname: ha-nova\n---\n", 0o644)
	writeBundleTestFile(t, filepath.Join(paths.Home, ".codex", "skills", "ha-nova-notes", "SKILL.md"), "---\nname: my-notes\n---\n", 0o644)
	codexHome := filepath.Join(paths.Home, "codex-work")
	t.Setenv("CODEX_HOME", codexHome)
	writeBundleTestFile(t, filepath.Join(codexHome, "skills", "ha-nova-read", "SKILL.md"), "---\nname: ha-nova-read\n---\n", 0o644)

	want := []codexSkillProblem{
		{Path: filepath.Join(codexSkillsRoot(paths.Home), "ha-nova-read"), Kind: "legacy"},
		{Path: filepath.Join(codexSkillsRoot(paths.Home), "ha-nova-guide"), Kind: "extra"},
		{Path: filepath.Join(paths.Home, ".codex", "skills", "ha-nova"), Kind: "extra"},
		{Path: filepath.Join(codexHome, "skills", "ha-nova-read"), Kind: "extra"},
	}
	problems := codexSkillProblems(paths)
	if !slices.Equal(problems, want) {
		t.Fatalf("problems = %+v\nwant %+v", problems, want)
	}
	for _, osName := range []string{"darwin", "linux"} {
		withCodexPlatform(t, osName)
		for _, problem := range problems {
			warning, fix := doctorCodexSkillProblem(problem)
			if !strings.Contains(warning, problem.Path) || !strings.HasPrefix(fix, "To fix: ") {
				t.Fatalf("%s: %q / %q", problem.Kind, warning, fix)
			}
			if (problem.Kind == "legacy") != strings.Contains(fix, "ha-nova setup codex") {
				t.Fatalf("%s: only setup's own cleanup may be offered as the fix: %q", problem.Kind, fix)
			}
			finder := strings.Contains(fix, "Cmd+Shift+.")
			if finder != (problem.Kind == "extra" && osName == "darwin") {
				t.Fatalf("%s on %s: Finder hint = %v: %q", problem.Kind, osName, finder, fix)
			}
		}
	}
}

func codexSkillProblemKinds(paths runtimePaths) []string {
	kinds := []string{}
	for _, problem := range codexSkillProblems(paths) {
		kinds = append(kinds, problem.Kind)
	}
	return kinds
}
