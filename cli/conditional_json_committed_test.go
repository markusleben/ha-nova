package main

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
)

func TestConditionalCheckpointRecoversCommittedAuxiliaryCleanup(
	t *testing.T,
) {
	paths := writeTestConfigFile(
		t,
		`{"generation":"expected"}`,
	)
	expected, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	previousHook := conditionalJSONAfterMarkerRetirement
	conditionalJSONAfterMarkerRetirement = func(string) error {
		return errors.New("simulated committed cleanup crash")
	}
	t.Cleanup(func() {
		conditionalJSONAfterMarkerRetirement = previousHook
	})

	err = writeJSONFileIfUnchanged(
		paths.ConfigFile,
		map[string]string{"generation": "replacement"},
		0o600,
		expected,
	)
	if err == nil ||
		err.Error() != "simulated committed cleanup crash" {
		t.Fatalf("write error = %v", err)
	}
	if _, err := os.Lstat(
		conditionalJSONTransactionPath(paths.ConfigFile),
	); !os.IsNotExist(err) {
		t.Fatalf("active marker remains: %v", err)
	}
	committedPath := conditionalJSONCommittedTransactionPath(
		paths.ConfigFile,
	)
	data, err := os.ReadFile(committedPath)
	if err != nil {
		t.Fatal(err)
	}
	var transaction conditionalJSONTransaction
	if err := json.Unmarshal(data, &transaction); err != nil {
		t.Fatal(err)
	}
	auxiliaryPaths := []string{
		transaction.ReplacementPath,
		transaction.PriorPath,
	}
	existingAuxiliaries := 0
	for _, candidate := range auxiliaryPaths {
		if _, err := os.Lstat(candidate); err == nil {
			existingAuxiliaries++
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if existingAuxiliaries == 0 {
		t.Fatal("committed transaction retained no auxiliary generation")
	}

	conditionalJSONAfterMarkerRetirement = func(string) error {
		return nil
	}
	if err := recoverConditionalJSONTransaction(
		paths.ConfigFile,
	); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range auxiliaryPaths {
		if _, err := os.Lstat(candidate); !os.IsNotExist(err) {
			t.Fatalf(
				"committed auxiliary %q remains: %v",
				candidate,
				err,
			)
		}
	}
	if _, err := os.Lstat(committedPath); !os.IsNotExist(err) {
		t.Fatalf("committed cleanup marker remains: %v", err)
	}
}
