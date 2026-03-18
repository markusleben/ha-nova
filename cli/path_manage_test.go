package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathContainsEntryMatchesWholeSegmentOnly(t *testing.T) {
	pathValue := `C:\Tools;C:\Users\markus\.local\share\ha-nova-backup;C:\Other`
	target := `C:\Users\markus\.local\share\ha-nova`
	if pathContainsEntry(pathValue, target) {
		t.Fatalf("expected substring-only match to be rejected")
	}
}

func TestPathContainsEntryMatchesCaseInsensitiveSegment(t *testing.T) {
	pathValue := `C:\Tools;C:\Users\Markus\.LOCAL\share\HA-NOVA`
	target := `c:\users\markus\.local\share\ha-nova`
	if !pathContainsEntry(pathValue, target) {
		t.Fatalf("expected case-insensitive segment match")
	}
}

func TestRemoveManagedPathSkipsUnixCleanupWhenPathNotManaged(t *testing.T) {
	tmp := t.TempDir()
	rcFile := filepath.Join(tmp, ".zshrc")
	original := "# existing\nexport PATH=\"$HOME/.local/bin:$PATH\"\n"
	if err := os.WriteFile(rcFile, []byte(original), 0o644); err != nil {
		t.Fatalf("write rc file: %v", err)
	}

	removeManagedPath(runtimePaths{}, installState{
		PathManaged: false,
		PathTarget:  rcFile,
	})

	got, err := os.ReadFile(rcFile)
	if err != nil {
		t.Fatalf("read rc file: %v", err)
	}
	if string(got) != original {
		t.Fatalf("unexpected rc mutation:\n%s", string(got))
	}
}

func TestRemoveManagedPathWithReportFailsLoudWhenUnixWriteFails(t *testing.T) {
	tmp := t.TempDir()
	rcFile := filepath.Join(tmp, ".zshrc")
	if err := os.WriteFile(rcFile, []byte(pathBlockHeader+"\n"+pathExportLine+"\n"+pathBlockFooter+"\n"), 0o444); err != nil {
		t.Fatalf("write rc file: %v", err)
	}

	removed, err := removeManagedPathWithReport(runtimePaths{}, installState{
		PathManaged: true,
		PathTarget:  rcFile,
	})
	if err == nil {
		t.Fatal("expected write failure")
	}
	if removed != "" {
		t.Fatalf("did not expect removal to be reported on failure, got %q", removed)
	}
	if !strings.Contains(err.Error(), "permission") {
		t.Fatalf("expected permission failure, got %v", err)
	}
}

func TestRemoveManagedPathWithReportDoesNotStripGenericLocalBinExportWithoutHANOVABlock(t *testing.T) {
	tmp := t.TempDir()
	rcFile := filepath.Join(tmp, ".zshrc")
	original := "# existing\nexport PATH=\"$HOME/.local/bin:$PATH\"\nexport PATH=\"$HOME/bin:$PATH\"\n"
	if err := os.WriteFile(rcFile, []byte(original), 0o644); err != nil {
		t.Fatalf("write rc file: %v", err)
	}

	removed, err := removeManagedPathWithReport(runtimePaths{}, installState{
		PathManaged: true,
		PathTarget:  rcFile,
	})
	if err != nil {
		t.Fatalf("removeManagedPathWithReport() error: %v", err)
	}
	if removed != "" {
		t.Fatalf("did not expect PATH cleanup to be reported, got %q", removed)
	}

	got, err := os.ReadFile(rcFile)
	if err != nil {
		t.Fatalf("read rc file: %v", err)
	}
	if string(got) != original {
		t.Fatalf("expected generic PATH line to stay untouched:\n%s", string(got))
	}
}
