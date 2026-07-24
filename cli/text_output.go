package main

import (
	"os"
	"path/filepath"
)

// writeRelayTextOutput writes bytes that the raw response boundary already
// normalized. Go performs no platform transcoding, so the bytes stay BOM-less
// UTF-8 on every operating system.
func writeRelayTextOutput(path string, utf8Data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, utf8Data, 0o644)
}
