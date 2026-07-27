package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateConditionalJSONTransaction(
	path string,
	transaction conditionalJSONTransaction,
) error {
	if transaction.Schema != conditionalJSONTransactionSchema {
		return fmt.Errorf(
			"unsupported conditional replacement transaction schema %d",
			transaction.Schema,
		)
	}
	switch transaction.Phase {
	case conditionalJSONPhaseReplace:
		if transaction.RestoreSHA != "" ||
			transaction.ConflictPath != "" {
			return errors.New(
				"conditional replacement transaction has unexpected restore state",
			)
		}
	case conditionalJSONPhaseRestoreConflict:
		if !validSHA256Hex(transaction.RestoreSHA) ||
			transaction.ConflictPath == "" {
			return errors.New(
				"conditional replacement transaction has invalid restore state",
			)
		}
	default:
		return errors.New(
			"conditional replacement transaction has an invalid phase",
		)
	}
	dir := filepath.Clean(filepath.Dir(path))
	items := []struct {
		path   string
		prefix string
	}{
		{
			path:   transaction.ReplacementPath,
			prefix: filepath.Base(path) + ".tmp.",
		},
		{
			path:   transaction.PriorPath,
			prefix: filepath.Base(path) + ".prior.",
		},
	}
	if transaction.Phase ==
		conditionalJSONPhaseRestoreConflict {
		items = append(items, struct {
			path   string
			prefix string
		}{
			path: transaction.ConflictPath,
			prefix: filepath.Base(path) +
				".conflict.",
		})
	}
	for _, item := range items {
		candidate := item.path
		if filepath.Clean(filepath.Dir(candidate)) != dir {
			return errors.New(
				"conditional replacement transaction escapes its target directory",
			)
		}
		if !strings.HasPrefix(
			filepath.Base(candidate),
			item.prefix,
		) {
			return errors.New(
				"conditional replacement transaction has an invalid generation path",
			)
		}
	}
	if !validSHA256Hex(transaction.ExpectedSHA256) ||
		!validSHA256Hex(transaction.ReplacementSHA) {
		return errors.New(
			"conditional replacement transaction has an invalid generation hash",
		)
	}
	return nil
}

func validSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func clearConditionalJSONTransaction(
	path string,
	transaction conditionalJSONTransaction,
) error {
	if err := removeTransactionMarkerDurably(
		conditionalJSONTransactionPath(path),
	); err != nil {
		return err
	}
	if err := conditionalJSONAfterMarkerRetirement(
		path,
	); err != nil {
		return err
	}
	return finishCommittedConditionalJSONTransaction(
		path,
		transaction,
	)
}

func clearConditionalJSONAuxiliaryFiles(
	path string,
	transaction conditionalJSONTransaction,
) error {
	for _, candidate := range []string{
		transaction.ReplacementPath,
		transaction.PriorPath,
		transaction.ConflictPath,
	} {
		if candidate == "" {
			continue
		}
		if err := os.Remove(candidate); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return syncParentDirectory(path)
}

func optionalFileSHA256(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return jsonContentSHA256(data), true, nil
}

func jsonContentSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}
