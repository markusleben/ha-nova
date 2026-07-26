package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

func recoverCommittedConditionalJSONTransaction(
	path string,
) error {
	data, err := os.ReadFile(
		conditionalJSONCommittedTransactionPath(path),
	)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf(
			"read committed conditional replacement: %w",
			err,
		)
	}
	transaction, err := decodeConditionalJSONTransaction(
		data,
		"committed conditional replacement",
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
	return finishCommittedConditionalJSONTransaction(
		path,
		transaction,
	)
}

func finishCommittedConditionalJSONTransaction(
	path string,
	transaction conditionalJSONTransaction,
) error {
	if err := clearConditionalJSONAuxiliaryFiles(
		path,
		transaction,
	); err != nil {
		return err
	}
	return removeCommittedTransactionMarkerDurably(
		conditionalJSONCommittedTransactionPath(path),
	)
}

func decodeConditionalJSONTransaction(
	data []byte,
	label string,
) (conditionalJSONTransaction, error) {
	var transaction conditionalJSONTransaction
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&transaction); err != nil {
		return conditionalJSONTransaction{}, fmt.Errorf(
			"%s metadata is corrupt: %w",
			label,
			err,
		)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return conditionalJSONTransaction{}, fmt.Errorf(
			"%s metadata has trailing data",
			label,
		)
	}
	return transaction, nil
}
