package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestConditionalCheckpointPreservesPriorWhenTargetDisappears(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"default_server": "default",
		"servers": {
			"default": {
				"profile_id": "profile-default",
				"route_policy": "local"
			}
		}
	}`)
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	previousHook := conditionalJSONAfterSwap
	conditionalJSONAfterSwap = func(string) error {
		return errors.New("simulated power loss")
	}
	t.Cleanup(func() {
		conditionalJSONAfterSwap = previousHook
	})
	servers, err := documentServersCopy(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeServersDocument(
		paths,
		doc,
		servers,
		defaultServerProfileName,
	); err == nil {
		t.Fatal("simulated power loss succeeded")
	}
	transactionData, err := os.ReadFile(
		conditionalJSONTransactionPath(paths.ConfigFile),
	)
	if err != nil {
		t.Fatal(err)
	}
	var transaction conditionalJSONTransaction
	if err := json.Unmarshal(
		transactionData,
		&transaction,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.ConfigFile); err != nil {
		t.Fatal(err)
	}
	err = recoverConditionalJSONTransaction(paths.ConfigFile)
	if err == nil ||
		!strings.Contains(err.Error(), "lost its target") {
		t.Fatalf("recovery error = %v", err)
	}
	prior, err := os.ReadFile(transaction.PriorPath)
	if err != nil {
		t.Fatalf("prior generation was not preserved: %v", err)
	}
	if jsonContentSHA256(prior) != transaction.ExpectedSHA256 {
		t.Fatal("preserved prior generation has the wrong hash")
	}
}

func TestConditionalCheckpointRejectsWriterAfterAtomicSwap(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{
		"schema_version": 3,
		"default_server": "default",
		"servers": {
			"default": {
				"profile_id": "profile-default",
				"route_policy": "local"
			}
		}
	}`)
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	external := []byte(`{"external":"wins"}`)
	previousHook := conditionalJSONAfterSwap
	conditionalJSONAfterSwap = func(path string) error {
		return os.WriteFile(path, external, 0o600)
	}
	t.Cleanup(func() {
		conditionalJSONAfterSwap = previousHook
	})
	servers, err := documentServersCopy(doc)
	if err != nil {
		t.Fatal(err)
	}
	err = writeServersDocument(
		paths,
		doc,
		servers,
		defaultServerProfileName,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "changed after") {
		t.Fatalf("post-swap writer error = %v", err)
	}
	got, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(external) {
		t.Fatalf("external generation was changed: %s", got)
	}
	if _, err := os.Lstat(
		conditionalJSONTransactionPath(paths.ConfigFile),
	); !os.IsNotExist(err) {
		t.Fatalf("transaction marker remains: %v", err)
	}
}

func TestConditionalCheckpointRejectsWriterBeforeMarkerRetirement(
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
	external := []byte(`{"generation":"external-tail"}`)
	previousHook := conditionalJSONBeforeMarkerRetirement
	conditionalJSONBeforeMarkerRetirement = func(path string) error {
		return os.WriteFile(path, external, 0o600)
	}
	t.Cleanup(func() {
		conditionalJSONBeforeMarkerRetirement = previousHook
	})

	err = writeJSONFileIfUnchanged(
		paths.ConfigFile,
		map[string]string{"generation": "replacement"},
		0o600,
		expected,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "before conditional") {
		t.Fatalf("tail writer error = %v", err)
	}
	got, err := os.ReadFile(paths.ConfigFile)
	if err != nil || string(got) != string(external) {
		t.Fatalf("external target=%s err=%v", got, err)
	}
	if _, err := os.Lstat(
		conditionalJSONTransactionPath(paths.ConfigFile),
	); err != nil {
		t.Fatalf("active transaction marker missing: %v", err)
	}
	conditionalJSONBeforeMarkerRetirement = func(string) error {
		return nil
	}
	err = recoverConditionalJSONTransaction(paths.ConfigFile)
	if err == nil ||
		!strings.Contains(err.Error(), "changed after") {
		t.Fatalf("tail writer recovery error = %v", err)
	}
	if err := recoverConditionalJSONTransaction(
		paths.ConfigFile,
	); err != nil {
		t.Fatal(err)
	}
}

func TestConditionalCheckpointRecoversTargetOnlyCommittedState(
	t *testing.T,
) {
	paths := writeTestConfigFile(t, `{"committed":true}`)
	target, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	transaction := conditionalJSONTransaction{
		Schema: conditionalJSONTransactionSchema,
		Phase:  conditionalJSONPhaseReplace,
		ReplacementPath: paths.ConfigFile +
			".tmp.missing",
		PriorPath: paths.ConfigFile +
			".prior.missing",
		ExpectedSHA256: jsonContentSHA256(
			[]byte(`{"committed":false}`),
		),
		ReplacementSHA: jsonContentSHA256(target),
	}
	if err := writeJSONFile(
		conditionalJSONTransactionPath(paths.ConfigFile),
		transaction,
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := recoverConditionalJSONTransaction(
		paths.ConfigFile,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(
		conditionalJSONTransactionPath(paths.ConfigFile),
	); !os.IsNotExist(err) {
		t.Fatalf("transaction marker remains: %v", err)
	}
}

func TestConditionalCheckpointRecoversDurableConflictRestore(
	t *testing.T,
) {
	for _, phase := range []string{
		"after conflict swap",
		"before conflict retirement",
	} {
		t.Run(phase, func(t *testing.T) {
			paths := writeTestConfigFile(
				t,
				`{"generation":"expected"}`,
			)
			expected, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			external := []byte(`{"generation":"external"}`)
			previousBeforeSwap := conditionalJSONBeforeSwap
			previousAfterConflict :=
				conditionalJSONAfterConflictSwap
			previousBeforeRetirement :=
				conditionalJSONBeforeConflictRetirement
			conditionalJSONBeforeSwap = func(path string) {
				conditionalJSONBeforeSwap = func(string) {}
				if err := os.WriteFile(
					path,
					external,
					0o600,
				); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "after conflict swap" {
				conditionalJSONAfterConflictSwap = func(
					string,
				) error {
					return errors.New("simulated restore crash")
				}
			} else {
				conditionalJSONBeforeConflictRetirement = func(
					string,
				) error {
					return errors.New("simulated retirement crash")
				}
			}
			t.Cleanup(func() {
				conditionalJSONBeforeSwap = previousBeforeSwap
				conditionalJSONAfterConflictSwap =
					previousAfterConflict
				conditionalJSONBeforeConflictRetirement =
					previousBeforeRetirement
			})

			err = writeJSONFileIfUnchanged(
				paths.ConfigFile,
				map[string]string{
					"generation": "replacement",
				},
				0o600,
				expected,
			)
			if err == nil ||
				!strings.Contains(err.Error(), "simulated") {
				t.Fatalf("interrupted restore error = %v", err)
			}
			marker, err := os.ReadFile(
				conditionalJSONTransactionPath(
					paths.ConfigFile,
				),
			)
			if err != nil {
				t.Fatal(err)
			}
			var transaction conditionalJSONTransaction
			if err := json.Unmarshal(
				marker,
				&transaction,
			); err != nil {
				t.Fatal(err)
			}
			if transaction.Phase !=
				conditionalJSONPhaseRestoreConflict ||
				transaction.ConflictPath == "" {
				t.Fatalf(
					"restore transaction = %+v",
					transaction,
				)
			}
			conditionalJSONAfterConflictSwap = func(
				string,
			) error {
				return nil
			}
			conditionalJSONBeforeConflictRetirement = func(
				string,
			) error {
				return nil
			}
			err = recoverConditionalJSONTransaction(
				paths.ConfigFile,
			)
			if !errors.Is(
				err,
				errConditionalJSONConflictRestored,
			) {
				t.Fatalf("restore recovery error = %v", err)
			}
			if err := recoverConditionalJSONTransaction(
				paths.ConfigFile,
			); err != nil {
				t.Fatalf("second recovery = %v", err)
			}
			got, err := os.ReadFile(paths.ConfigFile)
			if err != nil || string(got) != string(external) {
				t.Fatalf("target=%s err=%v", got, err)
			}
		})
	}
}
