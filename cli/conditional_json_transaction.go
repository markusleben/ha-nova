package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type conditionalJSONTransaction struct {
	Schema          int    `json:"schema"`
	Phase           string `json:"phase"`
	ReplacementPath string `json:"replacement_path"`
	PriorPath       string `json:"prior_path"`
	ExpectedSHA256  string `json:"expected_sha256"`
	ReplacementSHA  string `json:"replacement_sha256"`
	RestoreSHA      string `json:"restore_sha256,omitempty"`
	ConflictPath    string `json:"conflict_path,omitempty"`
}

const (
	conditionalJSONTransactionSchema    = 2
	conditionalJSONPhaseReplace         = "replace"
	conditionalJSONPhaseRestoreConflict = "restore_conflict"
)

var errConditionalJSONConflictRestored = errors.New("file changed before conditional replacement")

var conditionalJSONBeforeSwap = func(string) {}
var conditionalJSONAfterSwap = func(string) error { return nil }
var conditionalJSONAfterConflictSwap = func(string) error { return nil }
var conditionalJSONBeforeConflictRetirement = func(string) error {
	return nil
}
var conditionalJSONBeforeMarkerRetirement = func(string) error {
	return nil
}
var conditionalJSONAfterMarkerRetirement = func(string) error {
	return nil
}

func conditionalJSONTransactionPath(path string) string {
	return path + ".ha-nova-transaction.json"
}

func conditionalJSONCommittedTransactionPath(path string) string {
	return conditionalJSONTransactionPath(path) + ".committed"
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
		Phase:           conditionalJSONPhaseReplace,
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
	if err := recoverCommittedConditionalJSONTransaction(
		path,
	); err != nil {
		return err
	}
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
	transaction, err := decodeConditionalJSONTransaction(
		data,
		"interrupted conditional replacement",
	)
	if err != nil {
		return err
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
	if transaction.Phase == conditionalJSONPhaseRestoreConflict {
		return finishConditionalJSONConflictRestore(
			path,
			transaction,
			true,
		)
	}
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
			return beginConditionalJSONConflictRestore(
				path,
				transaction,
				priorSHA,
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
		return retireCommittedConditionalJSONTransaction(
			path,
			transaction,
		)
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
		return retireCommittedConditionalJSONTransaction(
			path,
			transaction,
		)
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
		if targetSHA == transaction.ReplacementSHA {
			return retireCommittedConditionalJSONTransaction(
				path,
				transaction,
			)
		}
		return clearConditionalJSONTransaction(path, transaction)
	default:
		return errors.New(
			"interrupted conditional replacement has an unknown state; preserved every generation for manual recovery",
		)
	}
}

func retireCommittedConditionalJSONTransaction(
	path string,
	transaction conditionalJSONTransaction,
) error {
	if err := conditionalJSONBeforeMarkerRetirement(
		path,
	); err != nil {
		return err
	}
	targetSHA, exists, err := optionalFileSHA256(path)
	if err != nil {
		return err
	}
	if !exists || targetSHA != transaction.ReplacementSHA {
		return errors.New(
			"file changed before conditional transaction retirement",
		)
	}
	return clearConditionalJSONTransaction(path, transaction)
}
