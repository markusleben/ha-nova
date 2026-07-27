package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCloudStatusJSONSurvivesPredispatchRecoveryFailure(
	t *testing.T,
) {
	for _, markerPath := range []struct {
		name string
		path func(string) string
	}{
		{
			name: "active transaction",
			path: conditionalJSONTransactionPath,
		},
		{
			name: "committed transaction",
			path: conditionalJSONCommittedTransactionPath,
		},
	} {
		t.Run(markerPath.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := writeTestConfigFile(
				t,
				`{"schema_version":1}`,
			)
			if err := os.WriteFile(
				markerPath.path(paths.ConfigFile),
				[]byte(`{"corrupt":`),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			exit, output := captureCommandOutput(t, func() int {
				return dispatch(
					paths,
					"ha-nova",
					[]string{
						"cloud",
						"status",
						"--server",
						"cabin",
						"--json",
					},
				)
			})
			var summary cloudStatusSummary
			if err := json.Unmarshal(
				[]byte(strings.TrimSpace(output)),
				&summary,
			); err != nil {
				t.Fatalf(
					"dispatch output=%q: %v",
					output,
					err,
				)
			}
			if exit != 1 ||
				summary.Status != "error" ||
				summary.Server != "cabin" ||
				summary.VerificationError == nil ||
				summary.VerificationError.Code !=
					cloudProblemConfigInvalid {
				t.Fatalf(
					"dispatch exit=%d summary=%+v",
					exit,
					summary,
				)
			}
		})
	}
}
