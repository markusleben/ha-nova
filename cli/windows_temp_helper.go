package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Windows UAC installer detection treats an executable without a manifest as
// an installer when its file name contains install, setup, update, patch or
// upgrade: CreateProcess then fails with ERROR_ELEVATION_REQUIRED and
// ShellExecute raises a UAC prompt a standard user cannot pass. The CLI has no
// manifest, so its %TEMP% helper copies must avoid those words.
// ponytail: name-only fix; an embedded asInvoker manifest (.syso) would make
// any name safe, add it when the build grows a Windows resource step.
const (
	windowsUpdateHelperPrefix    = "ha-nova-helper-"
	windowsUninstallHelperPrefix = "ha-nova-remover-"
	staleWindowsHelperAge        = time.Hour
)

// Names used through 0.26.0-rc2; still swept so their leftovers go away.
var legacyWindowsHelperPrefixes = []string{"ha-nova-updater-", "ha-nova-uninstall-"}

// stageWindowsHelperCopy copies the installed CLI to %TEMP%\<prefix><pid>.exe
// after sweeping what earlier runs left there.
func stageWindowsHelperCopy(paths runtimePaths, prefix string) (string, error) {
	sweepStaleWindowsHelpers()
	helper := filepath.Join(os.TempDir(), prefix+strconv.Itoa(os.Getpid())+".exe")
	return helper, copyFile(filepath.Join(paths.InstallRoot, publicBinaryName()), helper)
}

// sweepStaleWindowsHelpers removes helper copies and staged bundles that an
// earlier update or uninstall left in %TEMP% (best effort). Windows keeps a
// running helper's image locked, and the age floor spares a copy another
// process has just made but not started yet.
func sweepStaleWindowsHelpers() {
	dir := os.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !isWindowsHelperLeftover(entry) {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < staleWindowsHelperAge {
			continue
		}
		_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
	}
}

func isWindowsHelperLeftover(entry os.DirEntry) bool {
	name := strings.ToLower(entry.Name())
	if entry.IsDir() {
		return strings.HasPrefix(name, bundleStageDirPrefix)
	}
	if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".exe") {
		return false
	}
	for _, prefix := range append([]string{windowsUpdateHelperPrefix, windowsUninstallHelperPrefix}, legacyWindowsHelperPrefixes...) {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
