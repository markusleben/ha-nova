package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Transient sibling directories the in-place updater creates next to the install
// root while swapping bundles. They exist only for the duration of an update and
// are renamed/removed by commit()/rollback(). resolveSourceRoot keys off these
// prefixes to never resolve the client source from a moved-aside backup (see
// isTransientInstallBackup) — on Linux the running binary is renamed INTO
// `.ha-nova-old-*`, so os.Executable() would otherwise point client sync at the
// stale, about-to-be-deleted tree.
const (
	installBackupPrefixNext   = ".ha-nova-next-"
	installBackupPrefixOld    = ".ha-nova-old-"
	installBackupPrefixFailed = ".ha-nova-failed-"
)

func stageBundle(paths runtimePaths, version string) (string, error) {
	stageDir, err := os.MkdirTemp("", "ha-nova-stage-*")
	if err != nil {
		return "", err
	}
	keepStageDir := false
	defer func() {
		if !keepStageDir {
			_ = os.RemoveAll(stageDir)
		}
	}()

	archivePath := filepath.Join(stageDir, bundleAssetName())
	checksumURL := strings.TrimSpace(os.Getenv("HA_NOVA_BUNDLE_SHA256_URL"))
	downloadURL := strings.TrimSpace(os.Getenv("HA_NOVA_BUNDLE_URL"))
	if downloadURL == "" {
		downloadURL = fmt.Sprintf("https://github.com/markusleben/ha-nova/releases/download/v%s/%s", version, bundleAssetName())
	}
	if checksumURL == "" {
		checksumURL = downloadURL + ".sha256"
	}
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
	keepStageDir = true
	return stageRoot, nil
}

func cleanupStagedBundle(stageRoot string) {
	stageDir := filepath.Dir(stageRoot)
	if strings.HasPrefix(filepath.Base(stageDir), "ha-nova-stage-") {
		_ = os.RemoveAll(stageDir)
	}
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

func applyStagedBundleWithRollback(paths runtimePaths, stageRoot string) (func() error, func() error, error) {
	if err := validateBundleRoot(stageRoot); err != nil {
		return nil, nil, err
	}
	replacement, err := replaceInstallRootWithBackup(paths.InstallRoot, stageRoot)
	if err != nil {
		return nil, nil, err
	}
	if err := ensurePublicBinary(paths, filepath.Join(paths.InstallRoot, publicBinaryName())); err != nil {
		_ = replacement.rollback(paths)
		return nil, nil, err
	}
	return func() error {
			return replacement.rollback(paths)
		},
		func() error {
			return replacement.commit()
		},
		nil
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
	registryPath := filepath.Join(stageRoot, "clients", "registry.json")
	if _, err := os.Stat(registryPath); err != nil {
		return fmt.Errorf("client registry missing from staged update")
	}
	if err := validateClientRegistryFile(registryPath); err != nil {
		return err
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

func replaceInstallRootWithBackup(installRoot, stageRoot string) (installRootReplacement, error) {
	parent := filepath.Dir(installRoot)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return installRootReplacement{}, err
	}

	nextRoot := filepath.Join(parent, installBackupPrefixNext+strconv.FormatInt(time.Now().UnixNano(), 10))
	backupRoot := filepath.Join(parent, installBackupPrefixOld+strconv.FormatInt(time.Now().UnixNano(), 10))
	if err := copyDir(stageRoot, nextRoot); err != nil {
		_ = os.RemoveAll(nextRoot)
		return installRootReplacement{}, err
	}

	oldExists := false
	if _, err := os.Stat(installRoot); err == nil {
		oldExists = true
		if err := os.Rename(installRoot, backupRoot); err != nil {
			_ = os.RemoveAll(nextRoot)
			return installRootReplacement{}, err
		}
	}

	if err := os.Rename(nextRoot, installRoot); err != nil {
		if oldExists {
			_ = os.Rename(backupRoot, installRoot)
		}
		_ = os.RemoveAll(nextRoot)
		return installRootReplacement{}, err
	}

	return installRootReplacement{
		backupRoot: backupRoot,
		hadOld:     oldExists,
	}, nil
}

func (r installRootReplacement) commit() error {
	if !r.hadOld || strings.TrimSpace(r.backupRoot) == "" {
		return nil
	}
	return os.RemoveAll(r.backupRoot)
}

func (r installRootReplacement) rollback(paths runtimePaths) error {
	failedRoot := ""
	if _, err := os.Stat(paths.InstallRoot); err == nil {
		failedRoot = filepath.Join(filepath.Dir(paths.InstallRoot), installBackupPrefixFailed+strconv.FormatInt(time.Now().UnixNano(), 10))
		if err := os.Rename(paths.InstallRoot, failedRoot); err != nil {
			return err
		}
	}

	if r.hadOld && strings.TrimSpace(r.backupRoot) != "" {
		if err := os.Rename(r.backupRoot, paths.InstallRoot); err != nil {
			if failedRoot != "" {
				_ = os.Rename(failedRoot, paths.InstallRoot)
			}
			return err
		}
	} else if failedRoot == "" {
		return nil
	}

	if failedRoot != "" {
		_ = os.RemoveAll(failedRoot)
	}
	return ensurePublicBinary(paths, filepath.Join(paths.InstallRoot, publicBinaryName()))
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
