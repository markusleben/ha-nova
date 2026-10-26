package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// codexAppBundleID: the Codex desktop app, also when it ships as the ChatGPT
// desktop app (ChatGPT.app); the classic ChatGPT app has another id.
const codexAppBundleID = "com.openai.codex"

// codexAppInstalled: the Codex desktop app is on this Mac. It runs its own
// codex inside the app and reads the same ~/.agents/skills, but puts no codex
// command on PATH. macOS only: on Windows it is a Store package with no fixed
// path. Test seam.
var codexAppInstalled = func(home string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	dirs := []string{"/Applications"}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return codexAppIn(dirs...)
}

func codexAppIn(dirs ...string) bool {
	for _, dir := range dirs {
		for _, name := range []string{"ChatGPT.app", "Codex.app"} {
			plist := filepath.Join(dir, name, "Contents", "Info.plist")
			if !fileExists(plist) {
				continue
			}
			out, err := exec.Command("/usr/bin/plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", plist).Output()
			if err == nil && strings.TrimSpace(string(out)) == codexAppBundleID {
				return true
			}
		}
	}
	return false
}

func codexSkillsRoot(home string) string {
	return filepath.Join(home, ".agents", "skills")
}

// legacyCodexFlatSkillCopies: HA NOVA skills from the old Gemini layout,
// flat in ~/.agents/skills, which Codex loads next to the ha-nova tree, with
// old texts. A folder whose SKILL.md declares that exact name is one. It is
// HA NOVA's own (owned) only as a real folder of nothing but the Markdown
// files that layout wrote; anything else in it may be the user's (kept).
func legacyCodexFlatSkillCopies(home string, subSkills []string) (owned, kept []string) {
	for name := range legacyAntigravityFlatSkillNames(subSkills) {
		dir := filepath.Join(codexSkillsRoot(home), name)
		if declared, _ := skillDeclaresName(filepath.Join(dir, "SKILL.md"), name); !declared {
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

func onlyMarkdownFiles(dir string) bool {
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
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

// removeLegacyCodexFlatSkills: setup removes HA NOVA's own old flat copies;
// doctor names the rest.
func removeLegacyCodexFlatSkills(home, sourceRoot string) error {
	subSkills, err := sourceSubSkills(sourceRoot)
	if err != nil {
		return err
	}
	owned, _ := legacyCodexFlatSkillCopies(home, subSkills)
	for _, dir := range owned {
		if err := removePathAtomic(dir); err != nil {
			return err
		}
	}
	return nil
}

type codexSkillProblem struct {
	Path string
	// Kind: "outdated" (the ha-nova tree differs from this release), "legacy"
	// (HA NOVA's own old flat copy, which setup removes), "extra" (a copy
	// HA NOVA won't remove: it didn't install it, or it holds other files).
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
			fmt.Sprintf("To fix: run `%s`, then start a new chat in Codex.", setupCommand)
	case "legacy":
		return fmt.Sprintf("Codex also loads an old copy of HA NOVA from %s", problem.Path),
			fmt.Sprintf("To fix: run `%s`. It removes the old copy.", setupCommand)
	default:
		return fmt.Sprintf("Codex also loads a second copy of HA NOVA from %s, which HA NOVA leaves alone", problem.Path),
			"To fix: delete that folder (move anything of yours out first), then start a new chat in Codex."
	}
}
