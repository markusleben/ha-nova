package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// codexAppBundleID: the Codex desktop app, also when it ships as the ChatGPT
// desktop app (ChatGPT.app); the classic ChatGPT app has another id.
const codexAppBundleID = "com.openai.codex"

// codexPlatformOS: the OS whose Codex surfaces setup names. Test seam.
var codexPlatformOS = runtime.GOOS

// codexAppInstalled: the Codex desktop app is on this Mac. It runs its own
// codex inside the app and reads the same ~/.agents/skills, but puts no codex
// command on PATH. macOS only: on Windows it is a Store package with no fixed
// path. Test seam.
var codexAppInstalled = func(home string) bool {
	if codexPlatformOS != "darwin" {
		return false
	}
	dirs := []string{"/Applications"}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return codexAppIn(dirs...)
}

// codexBundleChecks: plutil's answer per Info.plist and its mtime, so a
// process asks once per app version, and a recheck still sees a new app.
var codexBundleChecks sync.Map

func codexAppIn(dirs ...string) bool {
	for _, dir := range dirs {
		for _, name := range []string{"ChatGPT.app", "Codex.app"} {
			plist := filepath.Join(dir, name, "Contents", "Info.plist")
			info, err := os.Stat(plist)
			if err != nil {
				continue
			}
			key := plist + "@" + info.ModTime().String()
			if known, ok := codexBundleChecks.Load(key); ok {
				if known.(bool) {
					return true
				}
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			out, err := exec.CommandContext(ctx, "/usr/bin/plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", plist).Output()
			cancel()
			codex := err == nil && strings.TrimSpace(string(out)) == codexAppBundleID
			codexBundleChecks.Store(key, codex)
			if codex {
				return true
			}
		}
	}
	return false
}

// codexWhereKey: the catalog line saying where "Codex" works. The desktop
// app exists for macOS and Windows only.
func codexWhereKey() string {
	if codexPlatformOS == "linux" {
		return "setup.codex.where_linux"
	}
	return "setup.codex.where"
}

// codexWhereLine is codexWhereKey's line for the terminal.
func codexWhereLine() string {
	if codexPlatformOS == "linux" {
		return setupT("setup.codex.where_linux")
	}
	return setupT("setup.codex.where")
}

func codexSkillsRoot(home string) string {
	return filepath.Join(home, ".agents", "skills")
}

// legacyCodexFlatSkillCopies: HA NOVA skills from the old Gemini layout,
// flat in ~/.agents/skills, which Codex loads next to the ha-nova tree, with
// old texts. A folder whose SKILL.md frontmatter declares that exact name is
// one. It is HA NOVA's own (owned) only as a real folder of nothing but the
// Markdown files that layout wrote; anything else in it may be the user's
// (kept).
func legacyCodexFlatSkillCopies(home string, subSkills []string) (owned, kept []string) {
	for name := range legacyAntigravityFlatSkillNames(subSkills) {
		dir := filepath.Join(codexSkillsRoot(home), name)
		if skillFrontmatterName(filepath.Join(dir, "SKILL.md")) != name {
			continue
		}
		if onlyMarkdownFiles(dir) {
			owned = append(owned, dir)
		} else {
			kept = append(kept, dir)
		}
	}
	sort.Strings(owned)
	sort.Strings(kept)
	return owned, kept
}

// skillFrontmatterName: the name: line of SKILL.md's leading YAML
// frontmatter; "" without one.
func skillFrontmatterName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "---" {
			return ""
		}
		if name, ok := strings.CutPrefix(line, "name:"); ok {
			return strings.TrimSpace(name)
		}
	}
	return ""
}

func onlyMarkdownFiles(dir string) bool {
	info, err := os.Lstat(dir)
	if err != nil || info.Mode().Type() != fs.ModeDir {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".md") {
			return false
		}
	}
	return true
}

// moveLegacyCodexFlatSkills: setup moves HA NOVA's own old flat copies out of
// ~/.agents/skills into a backup folder under the config dir, and returns it
// ("" when there was nothing to move). Doctor names the rest.
func moveLegacyCodexFlatSkills(paths runtimePaths, sourceRoot string) (string, error) {
	subSkills, err := sourceSubSkills(sourceRoot)
	if err != nil {
		return "", err
	}
	owned, _ := legacyCodexFlatSkillCopies(paths.Home, subSkills)
	if len(owned) == 0 {
		return "", nil
	}
	backups := filepath.Join(paths.ConfigDir, "backups")
	if err := os.MkdirAll(backups, 0o700); err != nil {
		return "", err
	}
	backup := filepath.Join(backups, "codex-legacy-"+time.Now().UTC().Format("20060102T150405.000Z"))
	if err := os.Mkdir(backup, 0o700); err != nil {
		return "", err
	}
	for _, dir := range owned {
		if err := movePath(dir, filepath.Join(backup, filepath.Base(dir))); err != nil {
			return backup, err
		}
	}
	return backup, nil
}

// movePath renames; only across file systems does it copy, then remove.
func movePath(from, to string) error {
	err := os.Rename(from, to)
	if err == nil || !crossDevice(err) {
		return err
	}
	if err := copyDir(from, to); err != nil {
		return err
	}
	return os.RemoveAll(from)
}

func crossDevice(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	const windowsNotSameDevice = 17 // ERROR_NOT_SAME_DEVICE
	return errno == syscall.EXDEV || (runtime.GOOS == "windows" && errno == windowsNotSameDevice)
}

// cleanUpLegacyCodexSkills runs after an explicit setup saved its state: a
// failure here leaves the install as it is, and doctor flags what's left.
func cleanUpLegacyCodexSkills(paths runtimePaths, clients []string) {
	touchesAgentsSkills := false
	for _, client := range normalizeClients(clients) {
		client = canonicalClientID(client)
		touchesAgentsSkills = touchesAgentsSkills || client == "codex" || client == "antigravity"
	}
	if !touchesAgentsSkills {
		return
	}
	backup, err := moveLegacyCodexFlatSkills(paths, resolveSourceRoot(paths))
	if backup != "" {
		printHumanInfo("Moved old HA NOVA copies that Codex also loaded to %s", backup)
	}
	if err != nil {
		printHumanWarn("Couldn't move every old HA NOVA copy out of %s: %s. Run ha-nova doctor to see what's left.", codexSkillsRoot(paths.Home), err)
	}
}

type codexSkillProblem struct {
	Path string
	// Kind: "outdated" (the ha-nova tree differs from this release), "legacy"
	// (HA NOVA's own old flat copy, which setup moves away), "extra" (a copy
	// HA NOVA won't touch: it didn't install it, or it holds other files).
	Kind string
}

// codexSkillProblems: HA NOVA skill copies Codex loads besides the current
// one, or instead of it.
func codexSkillProblems(paths runtimePaths) []codexSkillProblem {
	problems := []codexSkillProblem{}
	sourceRoot := resolveSourceRoot(paths)
	root := filepath.Join(codexSkillsRoot(paths.Home), "ha-nova")
	if managed, _ := managedSkillFingerprint(root, filepath.Join(root, "ha-nova", "SKILL.md"), true); managed &&
		!skillTreeMatches(root, filepath.Join(sourceRoot, "skills")) {
		problems = append(problems, codexSkillProblem{Path: root, Kind: "outdated"})
	}
	subSkills, _ := sourceSubSkills(sourceRoot)
	owned, kept := legacyCodexFlatSkillCopies(paths.Home, subSkills)
	for _, dir := range owned {
		problems = append(problems, codexSkillProblem{Path: dir, Kind: "legacy"})
	}
	for _, dir := range kept {
		problems = append(problems, codexSkillProblem{Path: dir, Kind: "extra"})
	}
	for _, dir := range codexDeprecatedSkillRoots(paths.Home) {
		matches, _ := filepath.Glob(filepath.Join(dir, "ha-nova*"))
		for _, match := range matches {
			flat, _ := skillDeclaresName(filepath.Join(match, "SKILL.md"), filepath.Base(match))
			tree, _ := skillDeclaresHANova(filepath.Join(match, "ha-nova", "SKILL.md"))
			if flat || tree {
				problems = append(problems, codexSkillProblem{Path: match, Kind: "extra"})
			}
		}
	}
	return problems
}

// codexDeprecatedSkillRoots: $CODEX_HOME/skills (default ~/.codex/skills),
// which Codex still reads. HA NOVA never installs there.
func codexDeprecatedSkillRoots(home string) []string {
	roots := []string{filepath.Join(home, ".codex", "skills")}
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		if custom := filepath.Join(codexHome, "skills"); filepath.Clean(custom) != filepath.Clean(roots[0]) {
			roots = append(roots, custom)
		}
	}
	return roots
}

// skillTreeMatches: installed holds exactly the files of source, byte for
// byte. A symlink to source matches without reading.
func skillTreeMatches(installed, source string) bool {
	installedReal, err := filepath.EvalSymlinks(installed)
	if err != nil {
		return false
	}
	sourceReal, err := filepath.EvalSymlinks(source)
	if err != nil {
		return false
	}
	if installedReal == sourceReal {
		return true
	}
	want, err := readSkillTree(sourceReal)
	if err != nil {
		return false
	}
	got, err := readSkillTree(installedReal)
	if err != nil || len(got) != len(want) {
		return false
	}
	for rel, data := range want {
		if !bytes.Equal(got[rel], data) {
			return false
		}
	}
	return true
}

func readSkillTree(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = data
		return nil
	})
	return files, err
}

// doctorCodexSkillProblem: the warning and its fix for one stale copy.
func doctorCodexSkillProblem(problem codexSkillProblem) (string, string) {
	setupCommand := doctorClientSetupCommand("codex")
	switch problem.Kind {
	case "outdated":
		return fmt.Sprintf("Codex loads an older version of HA NOVA from %s", problem.Path),
			fmt.Sprintf("To fix: run \x60%s\x60, then start a new chat in Codex.", setupCommand)
	case "legacy":
		return fmt.Sprintf("Codex also loads an old copy of HA NOVA from %s", problem.Path),
			fmt.Sprintf("To fix: run \x60%s\x60. It moves the old copy into a backup folder.", setupCommand)
	default:
		fix := "To fix: delete that folder (move anything of yours out first), then start a new chat in Codex."
		if codexPlatformOS == "darwin" {
			fix += " In Finder, press Cmd+Shift+. to show hidden folders."
		}
		return fmt.Sprintf("Codex also loads a second copy of HA NOVA from %s, which HA NOVA leaves alone", problem.Path), fix
	}
}
