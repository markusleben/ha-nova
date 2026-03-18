package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunJQSupportsFilterFile(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	inputPath := filepath.Join(tempDir, "input.json")
	filterPath := filepath.Join(tempDir, "filter.jq")

	if err := os.WriteFile(inputPath, []byte(`{"items":[1,2,3]}`), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := os.WriteFile(filterPath, []byte(`.items | length`), 0o644); err != nil {
		t.Fatalf("write filter: %v", err)
	}

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writer
	defer func() {
		_ = writer.Close()
		os.Stdout = originalStdout
	}()

	exitCode := runJQ([]string{"--file", inputPath, "--jq-file", filterPath})
	_ = writer.Close()

	var output bytes.Buffer
	if _, err := output.ReadFrom(reader); err != nil {
		t.Fatalf("read output: %v", err)
	}

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if got := output.String(); got != "3\n" {
		t.Fatalf("expected jq output %q, got %q", "3\n", got)
	}
}

func TestRunJQRejectsInlineFilterAndFilterFileTogether(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	filterPath := filepath.Join(tempDir, "filter.jq")
	if err := os.WriteFile(filterPath, []byte(`.`), 0o644); err != nil {
		t.Fatalf("write filter: %v", err)
	}

	if exitCode := runJQ([]string{"--jq-file", filterPath, "."}); exitCode == 0 {
		t.Fatal("expected failure when inline filter and --jq-file are both provided")
	}
}
