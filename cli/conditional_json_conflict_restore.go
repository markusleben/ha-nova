package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func beginConditionalJSONConflictRestore(
	path string,
	transaction conditionalJSONTransaction,
	restoreSHA string,
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
	transaction.Phase = conditionalJSONPhaseRestoreConflict
	transaction.RestoreSHA = restoreSHA
	transaction.ConflictPath = conflictPath
	if err := writeJSONFile(
		conditionalJSONTransactionPath(path),
		transaction,
		0o600,
	); err != nil {
		return fmt.Errorf(
			"persist conditional conflict restore: %w",
			err,
		)
	}
	return finishConditionalJSONConflictRestore(
		path,
		transaction,
		true,
	)
}

func finishConditionalJSONConflictRestore(
	path string,
	transaction conditionalJSONTransaction,
	allowSwap bool,
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
	conflictSHA, conflictExists, err :=
		optionalFileSHA256(transaction.ConflictPath)
	if err != nil {
		return err
	}
	switch {
	case allowSwap &&
		targetExists &&
		targetSHA == transaction.ReplacementSHA &&
		priorExists &&
		priorSHA == transaction.RestoreSHA &&
		!conflictExists:
		replaceErr := replaceFileKeepingPrior(
			path,
			transaction.PriorPath,
			transaction.ConflictPath,
		)
		if replaceErr == nil {
			if err := conditionalJSONAfterConflictSwap(
				path,
			); err != nil {
				return err
			}
		}
		recoveryErr := finishConditionalJSONConflictRestore(
			path,
			transaction,
			false,
		)
		if replaceErr != nil &&
			recoveryErr != nil &&
			!errors.Is(
				recoveryErr,
				errConditionalJSONConflictRestored,
			) {
			return errors.Join(replaceErr, recoveryErr)
		}
		return recoveryErr
	case targetExists &&
		targetSHA == transaction.RestoreSHA &&
		priorExists &&
		priorSHA == transaction.ReplacementSHA &&
		!conflictExists:
		if err := os.Rename(
			transaction.PriorPath,
			transaction.ConflictPath,
		); err != nil {
			return err
		}
		if err := syncParentDirectory(path); err != nil {
			return err
		}
		return finishConditionalJSONConflictRestore(
			path,
			transaction,
			false,
		)
	case targetExists &&
		targetSHA == transaction.RestoreSHA &&
		!priorExists &&
		conflictExists &&
		conflictSHA == transaction.ReplacementSHA:
		if err := conditionalJSONBeforeConflictRetirement(
			path,
		); err != nil {
			return err
		}
		if err := clearConditionalJSONTransaction(
			path,
			transaction,
		); err != nil {
			return err
		}
		return errConditionalJSONConflictRestored
	default:
		return errors.New(
			"interrupted conditional conflict restore has an unknown state; preserved every generation for manual recovery",
		)
	}
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
