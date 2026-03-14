package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func runSetup(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	host := fs.String("host", "", "Home Assistant host")
	haURL := fs.String("ha-url", "", "Home Assistant base URL")
	relayToken := fs.String("relay-token", "", "Relay auth token")
	nonInteractive := fs.Bool("non-interactive", false, "Disable prompts")
	if err := fs.Parse(normalizeSetupArgs(args)); err != nil {
		printErr("%s", err)
		return 1
	}

	target := ""
	if remaining := fs.Args(); len(remaining) > 0 {
		target = remaining[0]
	}

	cfg, _ := loadConfig(paths)
	state := loadStateOrDefault(paths)

	if target == "" && !*nonInteractive {
		likely := detectLikelyClients(paths)
		defaultClient := likely[0]
		if len(likely) > 1 {
			defaultClient = "all"
		}
		answer, err := promptLine("Client (claude/codex/opencode/gemini/all)", defaultClient)
		if err != nil {
			printErr("%s", err)
			return 1
		}
		target = strings.ToLower(strings.TrimSpace(answer))
	}
	if target == "" {
		target = "all"
	}

	if *host != "" {
		cfg.HAHost = *host
	}
	if *haURL != "" {
		cfg.HAURL = *haURL
	}
	if cfg.HAHost == "" && cfg.HAURL != "" {
		cfg.HAHost = strings.TrimPrefix(strings.TrimPrefix(cfg.HAURL, "http://"), "https://")
		cfg.HAHost = strings.TrimSuffix(cfg.HAHost, ":8123")
	}
	if cfg.HAHost == "" && !*nonInteractive {
		answer, err := promptLine("Home Assistant host", cfg.HAHost)
		if err != nil {
			printErr("%s", err)
			return 1
		}
		cfg.HAHost = strings.TrimSpace(answer)
	}
	if cfg.HAHost == "" {
		printErr("missing Home Assistant host; use --host or run interactively")
		return 1
	}
	if cfg.HAURL == "" {
		cfg.HAURL = "http://" + cfg.HAHost + ":8123"
	}
	if cfg.RelayBaseURL == "" {
		cfg.RelayBaseURL = "http://" + cfg.HAHost + ":8791"
	}

	token := strings.TrimSpace(*relayToken)
	if token == "" {
		if existing, err := readRelayAuthToken(); err == nil {
			token = existing
		}
	}
	if token == "" && !*nonInteractive {
		answer, err := promptLine("Relay auth token", "")
		if err != nil {
			printErr("%s", err)
			return 1
		}
		token = strings.TrimSpace(answer)
	}
	if token == "" {
		printErr("missing relay auth token; use --relay-token or run interactively")
		return 1
	}

	if err := saveConfig(paths, cfg); err != nil {
		printErr("cannot save config: %s", err)
		return 1
	}
	if err := writeRelayAuthToken(token); err != nil {
		printErr("cannot save relay token: %s", err)
		return 1
	}
	state.Version = localVersion(paths)
	if resolveSourceRoot(paths) == paths.InstallRoot {
		state.InstallSource = "bundle"
	} else {
		state.InstallSource = "dev"
	}

	if err := installClients(paths, &state, expandTargetClients(target)); err != nil {
		printErr("client installation failed: %s", err)
		return 1
	}
	if err := saveState(paths, state); err != nil {
		printErr("cannot save state: %s", err)
		return 1
	}

	printInfo("Saved HA NOVA configuration")
	if err := copyToClipboard(token); err == nil {
		printInfo("Copied relay token to clipboard")
	}
	if err := openBrowser(cfg.HAURL + "/hassio/addon/2368fcfa_ha_nova_relay/config"); err != nil {
		printWarn("Browser launch skipped; open this URL manually if needed: %s/hassio/addon/2368fcfa_ha_nova_relay/config", cfg.HAURL)
	}
	return runDoctor(paths, nil)
}

func normalizeSetupArgs(args []string) []string {
	if len(args) < 2 {
		return args
	}
	if strings.HasPrefix(args[0], "-") || !isSetupTarget(args[0]) {
		return args
	}
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			return append(append([]string{}, args[1:]...), args[0])
		}
	}
	return args
}

func isSetupTarget(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "all", "claude", "codex", "opencode", "gemini":
		return true
	default:
		return false
	}
}

func runDoctor(paths runtimePaths, _ []string) int {
	cfg, cfgErr := loadConfig(paths)
	token, tokenErr := readRelayAuthToken()
	state := loadStateOrDefault(paths)
	status := 0

	if cfgErr == nil {
		printInfo("Config present: %s", paths.ConfigFile)
	} else {
		printErr("%s", cfgErr)
		return 1
	}

	if tokenErr == nil && token != "" {
		printInfo("Relay auth token present in secure storage")
	} else {
		printErr("relay auth token missing; run: ha-nova setup")
		return 1
	}

	if err := probeHTTP(cfg.HAURL); err != nil {
		printErr("Home Assistant unreachable: %s", err)
		status = 1
	} else {
		printInfo("Home Assistant reachable: %s", cfg.HAURL)
	}

	body, err := fetchRelayHealth(cfg.RelayBaseURL, token)
	if err != nil {
		printErr("Relay health failed: %s", err)
		status = 1
	} else {
		printInfo("Relay health reachable: %s/health", cfg.RelayBaseURL)
		if warning := checkRelayVersion(paths, body); warning != "" {
			printWarn("%s", warning)
			status = 1
		}
	}

	if len(state.InstalledClients) > 0 {
		printInfo("Installed clients: %s", strings.Join(state.InstalledClients, ", "))
	}
	if msg := checkForUpdate(paths, false); strings.Contains(msg, "UPDATE AVAILABLE") {
		fmt.Fprintln(os.Stdout, msg)
	}
	if status == 0 {
		printInfo("Doctor checks passed")
	}
	return status
}

func runCheckUpdate(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("check-update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	quiet := fs.Bool("quiet", false, "quiet")
	if err := fs.Parse(args); err != nil {
		printErr("%s", err)
		return 1
	}
	msg := checkForUpdate(paths, *quiet)
	if msg == "" {
		return 0
	}
	fmt.Fprintln(os.Stdout, msg)
	if strings.Contains(msg, "UPDATE AVAILABLE") {
		return 0
	}
	if strings.Contains(msg, "WARNING") {
		return 1
	}
	return 0
}

func runUpdate(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	versionFlag := fs.String("version", "", "explicit version")
	if err := fs.Parse(args); err != nil {
		printErr("%s", err)
		return 1
	}

	targetVersion := strings.TrimPrefix(strings.TrimSpace(*versionFlag), "v")
	if targetVersion == "" {
		release, err := fetchLatestRelease(paths, true)
		if err != nil {
			printErr("%s", err)
			return 1
		}
		targetVersion = release.Version
	}
	currentVersion := localVersion(paths)
	if currentVersion != "dev" && compareSemver(currentVersion, targetVersion) >= 0 {
		printInfo("Already up to date: v%s", currentVersion)
		return 0
	}

	stageRoot, err := stageBundle(paths, targetVersion)
	if err != nil {
		printErr("update failed: %s", err)
		return 1
	}

	if runtime.GOOS == "windows" {
		if err := launchWindowsReplace(paths, stageRoot); err != nil {
			printErr("cannot start Windows updater: %s", err)
			return 1
		}
		printInfo("Update staged. Restart your shell/client after the updater finishes.")
		return 0
	}

	if err := applyStagedBundle(paths, stageRoot); err != nil {
		printErr("cannot apply update: %s", err)
		return 1
	}
	if err := postUpdateSync(paths); err != nil {
		printWarn("post-update sync failed: %s", err)
	}
	printInfo("Updated to v%s", localVersion(paths))
	return 0
}

func runInternalReplace(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("internal-replace", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	stageRoot := fs.String("stage-root", "", "stage root")
	parentPID := fs.Int("parent-pid", 0, "parent pid")
	if err := fs.Parse(args); err != nil {
		printErr("%s", err)
		return 1
	}
	if *stageRoot == "" {
		printErr("missing --stage-root")
		return 1
	}
	waitForParentRelease(*parentPID)
	if err := applyStagedBundle(paths, *stageRoot); err != nil {
		printErr("%s", err)
		return 1
	}
	if err := postUpdateSync(paths); err != nil {
		printWarn("post-update sync failed: %s", err)
	}
	return 0
}

func runInternalSyncClients(paths runtimePaths, _ []string) int {
	if err := postUpdateSync(paths); err != nil {
		printErr("%s", err)
		return 1
	}
	return 0
}

func runUninstall(paths runtimePaths, args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	yes := fs.Bool("yes", false, "skip confirmation")
	if err := fs.Parse(args); err != nil {
		printErr("%s", err)
		return 1
	}
	if !*yes && isInteractiveTTY() {
		answer, err := promptLine("Remove HA NOVA completely? (yes/no)", "no")
		if err != nil {
			printErr("%s", err)
			return 1
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "yes" {
			printInfo("Uninstall cancelled")
			return 0
		}
	}

	state := loadStateOrDefault(paths)
	if err := removeInstalledClients(paths, state); err != nil {
		printErr("failed to remove client integrations: %s", err)
		return 1
	}
	_ = deleteRelayAuthToken()
	removeManagedPath(paths, state)
	for _, path := range []string{
		paths.InstallRoot,
		paths.ConfigDir,
		paths.PublicBinary,
		paths.UpdateCacheFile,
	} {
		if path == "" {
			continue
		}
		_ = os.RemoveAll(path)
	}
	printInfo("HA NOVA removed")
	return 0
}

func fetchRelayHealth(relayBaseURL, token string) ([]byte, error) {
	url := strings.TrimRight(relayBaseURL, "/") + "/health"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func probeHTTP(url string) error {
	req, err := http.NewRequest("GET", strings.TrimRight(url, "/"), nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func stageBundle(paths runtimePaths, version string) (string, error) {
	stageDir, err := os.MkdirTemp("", "ha-nova-stage-*")
	if err != nil {
		return "", err
	}

	archivePath := filepath.Join(stageDir, bundleAssetName())
	checksumURL := fmt.Sprintf("https://github.com/markusleben/ha-nova/releases/download/v%s/%s.sha256", version, bundleAssetName())
	downloadURL := fmt.Sprintf("https://github.com/markusleben/ha-nova/releases/download/v%s/%s", version, bundleAssetName())
	resp, err := httpClient.Get(downloadURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(archivePath)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	checksumResp, err := httpClient.Get(checksumURL)
	if err != nil {
		return "", err
	}
	defer checksumResp.Body.Close()
	if checksumResp.StatusCode >= 400 {
		return "", fmt.Errorf("checksum download failed: HTTP %d", checksumResp.StatusCode)
	}
	manifestBytes, err := io.ReadAll(checksumResp.Body)
	if err != nil {
		return "", err
	}
	if err := verifyFileChecksum(archivePath, string(manifestBytes)); err != nil {
		return "", err
	}

	if err := extractArchive(archivePath, stageDir); err != nil {
		return "", err
	}
	stageRoot := filepath.Join(stageDir, "ha-nova")
	if err := validateBundleRoot(stageRoot); err != nil {
		return "", err
	}
	return stageRoot, nil
}

func extractArchive(archivePath, destDir string) error {
	if strings.HasSuffix(archivePath, ".zip") {
		return unzipArchive(archivePath, destDir)
	}
	return untarArchive(archivePath, destDir)
}

func untarArchive(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		target, err := secureArchivePath(destDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func unzipArchive(archivePath, destDir string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		target, err := secureArchivePath(destDir, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return err
		}
		rc.Close()
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}

func applyStagedBundle(paths runtimePaths, stageRoot string) error {
	if err := validateBundleRoot(stageRoot); err != nil {
		return err
	}
	if err := replaceInstallRoot(paths.InstallRoot, stageRoot); err != nil {
		return err
	}
	return ensurePublicBinary(paths, filepath.Join(paths.InstallRoot, publicBinaryName()))
}

func postUpdateSync(paths runtimePaths) error {
	state := loadStateOrDefault(paths)
	if err := installClients(paths, &state, state.InstalledClients); err != nil {
		return err
	}
	state.Version = localVersion(paths)
	return saveState(paths, state)
}

func launchWindowsReplace(paths runtimePaths, stageRoot string) error {
	tempHelper := filepath.Join(os.TempDir(), "ha-nova-updater-"+strconv.Itoa(os.Getpid())+".exe")
	if err := copyFile(filepath.Join(paths.InstallRoot, publicBinaryName()), tempHelper); err != nil {
		return err
	}
	cmd := exec.Command(tempHelper, "internal-replace", "--parent-pid", strconv.Itoa(os.Getpid()), "--stage-root", stageRoot)
	return cmd.Start()
}

func waitForParentRelease(parentPID int) {
	if parentPID <= 0 || runtime.GOOS != "windows" {
		time.Sleep(2 * time.Second)
		return
	}
	for i := 0; i < 60; i++ {
		cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", fmt.Sprintf(`if (Get-Process -Id %d -ErrorAction SilentlyContinue) { exit 1 }`, parentPID))
		if err := cmd.Run(); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func checkFile(path string) error {
	_, err := os.Stat(path)
	return err
}

func expandTargetClients(target string) []string {
	if target == "" || target == "all" {
		return []string{"claude", "codex", "opencode", "gemini"}
	}
	return []string{target}
}

func validateBundleRoot(stageRoot string) error {
	if stageRoot == "" {
		return fmt.Errorf("bundle root missing")
	}
	info, err := os.Stat(stageRoot)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("bundle root is not a directory: %s", stageRoot)
	}
	metaPath := filepath.Join(stageRoot, "bundle.json")
	if _, err := os.Stat(metaPath); err != nil {
		return fmt.Errorf("bundle metadata missing from staged update")
	}
	runtimePath := filepath.Join(stageRoot, publicBinaryName())
	if _, err := os.Stat(runtimePath); err != nil {
		return fmt.Errorf("bundle runtime missing from staged update")
	}
	meta, err := loadBundleMetadataFile(metaPath)
	if err != nil {
		return err
	}
	if meta.OS != bundlePlatformOS() {
		return fmt.Errorf("bundle OS mismatch: %s", meta.OS)
	}
	if meta.Arch != bundlePlatformArch() {
		return fmt.Errorf("bundle arch mismatch: %s", meta.Arch)
	}
	if meta.BinaryName != publicBinaryName() {
		return fmt.Errorf("bundle binary mismatch: %s", meta.BinaryName)
	}
	return nil
}

func secureArchivePath(destDir, entryName string) (string, error) {
	cleanName := filepath.Clean(entryName)
	if cleanName == "." || cleanName == "" {
		return "", fmt.Errorf("invalid archive entry: %q", entryName)
	}
	if filepath.IsAbs(cleanName) {
		return "", fmt.Errorf("absolute archive entry path rejected: %q", entryName)
	}
	target := filepath.Join(destDir, cleanName)
	rel, err := filepath.Rel(destDir, target)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry escapes destination: %q", entryName)
	}
	return target, nil
}

func replaceInstallRoot(installRoot, stageRoot string) error {
	parent := filepath.Dir(installRoot)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}

	nextRoot := filepath.Join(parent, ".ha-nova-next-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	backupRoot := filepath.Join(parent, ".ha-nova-old-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := copyDir(stageRoot, nextRoot); err != nil {
		_ = os.RemoveAll(nextRoot)
		return err
	}

	oldExists := false
	if _, err := os.Stat(installRoot); err == nil {
		oldExists = true
		if err := os.Rename(installRoot, backupRoot); err != nil {
			_ = os.RemoveAll(nextRoot)
			return err
		}
	}

	if err := os.Rename(nextRoot, installRoot); err != nil {
		if oldExists {
			_ = os.Rename(backupRoot, installRoot)
		}
		_ = os.RemoveAll(nextRoot)
		return err
	}

	if oldExists {
		_ = os.RemoveAll(backupRoot)
	}
	return nil
}

func loadBundleMetadataFile(path string) (bundleMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return bundleMetadata{}, err
	}
	var meta bundleMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return bundleMetadata{}, err
	}
	return meta, nil
}

func verifyFileChecksum(path, manifest string) error {
	fields := strings.Fields(strings.TrimSpace(manifest))
	if len(fields) == 0 {
		return fmt.Errorf("checksum manifest missing digest")
	}
	expected := strings.ToLower(fields[0])
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s", filepath.Base(path))
	}
	return nil
}
