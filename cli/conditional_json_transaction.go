package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type conditionalJSONTransaction struct {
	Schema          int    `json:"schema"`
	ReplacementPath string `json:"replacement_path"`
	PriorPath       string `json:"prior_path"`
	ExpectedSHA256  string `json:"expected_sha256"`
	ReplacementSHA  string `json:"replacement_sha256"`
}

const conditionalJSONTransactionSchema = 1

var conditionalJSONBeforeSwap = func(string) {}
var conditionalJSONAfterSwap = func(string) error { return nil }

func conditionalJSONTransactionPath(path string) string {
	return path + ".ha-nova-transaction.json"
}

func replaceFileConditionally(
	path string,
	replacementPath string,
	expected []byte,
) error {
	if err := recoverConditionalJSONTransaction(path); err != nil {
		return err
	}
	replacement, err := os.ReadFile(replacementPath)
	if err != nil {
		return err
	}
	priorFile, err := os.CreateTemp(
		filepath.Dir(path),
		filepath.Base(path)+".prior.*",
	)
	if err != nil {
		return err
	}
	priorPath := priorFile.Name()
	if err := priorFile.Close(); err != nil {
		return err
	}
	if err := os.Remove(priorPath); err != nil {
		return err
	}
	transaction := conditionalJSONTransaction{
		Schema:          conditionalJSONTransactionSchema,
		ReplacementPath: replacementPath,
		PriorPath:       priorPath,
		ExpectedSHA256:  jsonContentSHA256(expected),
		ReplacementSHA:  jsonContentSHA256(replacement),
	}
	if err := writeJSONFile(
		conditionalJSONTransactionPath(path),
		transaction,
		0o600,
	); err != nil {
		return fmt.Errorf(
			"persist conditional replacement transaction: %w",
			err,
		)
	}
	conditionalJSONBeforeSwap(path)
	if err := replaceFileKeepingPrior(
		path,
		replacementPath,
		priorPath,
	); err != nil {
		_ = recoverConditionalJSONTransaction(path)
		return fmt.Errorf("conditionally replace file: %w", err)
	}
	if err := conditionalJSONAfterSwap(path); err != nil {
		return err
	}
	return finishConditionalJSONTransaction(path, transaction)
}

func recoverConditionalJSONTransaction(path string) error {
	transactionPath := conditionalJSONTransactionPath(path)
	data, err := os.ReadFile(transactionPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf(
			"read interrupted conditional replacement: %w",
			err,
		)
	}
	var transaction conditionalJSONTransaction
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&transaction); err != nil {
		return fmt.Errorf(
			"interrupted conditional replacement metadata is corrupt: %w",
			err,
		)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf(
			"interrupted conditional replacement metadata has trailing data",
		)
	}
	if err := validateConditionalJSONTransaction(
		path,
		transaction,
	); err != nil {
		return err
	}
	return finishConditionalJSONTransaction(path, transaction)
}

func finishConditionalJSONTransaction(
	path string,
	transaction conditionalJSONTransaction,
) error {
	targetSHA, targetExists, err := optionalFileSHA256(path)
	if err != nil {
		return err
	}
	priorSHA, priorExists, err :=
		optionalFileSHA256(transaction.PriorPath)
	if err != nil {
		return err
	}
	replacementSHA, replacementExists, err :=
		optionalFileSHA256(transaction.ReplacementPath)
	if err != nil {
		return err
	}

	switch {
	case priorExists:
		if priorSHA != transaction.ExpectedSHA256 {
			return restoreConditionalJSONConflict(
				path,
				transaction,
				targetSHA,
				targetExists,
			)
		}
		if !targetExists {
			return errors.New(
				"interrupted conditional replacement lost its target; preserved the prior generation for manual recovery",
			)
		}
		if targetSHA != transaction.ReplacementSHA {
			if err := clearConditionalJSONTransaction(
				path,
				transaction,
			); err != nil {
				return err
			}
			return errors.New(
				"file changed after conditional replacement",
			)
		}
		// The atomic replacement observed the expected source generation and
		// the checkpoint remains the current generation.
		return clearConditionalJSONTransaction(path, transaction)
	case replacementExists &&
		replacementSHA == transaction.ExpectedSHA256 &&
		targetExists &&
		targetSHA == transaction.ReplacementSHA:
		// Unix crash after atomic exchange but before moving the prior
		// generation to its durable transaction path.
		if err := os.Rename(
			transaction.ReplacementPath,
			transaction.PriorPath,
		); err != nil {
			return err
		}
		if err := syncParentDirectory(path); err != nil {
			return err
		}
		return clearConditionalJSONTransaction(path, transaction)
	case replacementExists &&
		replacementSHA == transaction.ReplacementSHA:
		// The transaction was persisted but the atomic replacement did not
		// happen. Keep the current target, whatever generation it now contains.
		return clearConditionalJSONTransaction(path, transaction)
	case !priorExists &&
		!replacementExists &&
		targetExists &&
		(targetSHA == transaction.ReplacementSHA ||
			targetSHA == transaction.ExpectedSHA256):
		// Recovery from an older cleanup that removed both auxiliary
		// generations before durably retiring the transaction marker.
		return clearConditionalJSONTransaction(path, transaction)
	default:
		return errors.New(
			"interrupted conditional replacement has an unknown state; preserved every generation for manual recovery",
		)
	}
}

func restoreConditionalJSONConflict(
	path string,
	transaction conditionalJSONTransaction,
	targetSHA string,
	targetExists bool,
) error {
	if !targetExists || targetSHA != transaction.ReplacementSHA {
		return errors.New(
			"conditional replacement conflict: both the source and destination changed; preserved every generation for manual recovery",
		)
	}
	conflictPath, err := unusedConditionalConflictPath(path)
	if err != nil {
		return err
	}
	if err := replaceFileKeepingPrior(
		path,
		transaction.PriorPath,
		conflictPath,
	); err != nil {
		return fmt.Errorf(
			"restore file after conditional replacement conflict: %w",
			err,
		)
	}
	if conflictSHA, exists, readErr :=
		optionalFileSHA256(conflictPath); readErr != nil {
		return readErr
	} else if exists &&
		conflictSHA == transaction.ReplacementSHA {
		_ = os.Remove(conflictPath)
	}
	if err := clearConditionalJSONTransaction(
		path,
		transaction,
	); err != nil {
		return err
	}
	return errors.New("file changed before conditional replacement")
}

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
	dir := filepath.Clean(filepath.Dir(path))
	for _, item := range []struct {
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
	} {
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
	for _, candidate := range []string{
		transaction.ReplacementPath,
		transaction.PriorPath,
	} {
		if err := os.Remove(candidate); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return syncParentDirectory(path)
}

func unusedConditionalConflictPath(path string) (string, error) {
	file, err := os.CreateTemp(
		filepath.Dir(path),
		filepath.Base(path)+".conflict.*",
	)
	if err != nil {
		return "", err
	}
	conflictPath := file.Name()
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(conflictPath); err != nil {
		return "", err
	}
	return conflictPath, nil
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
