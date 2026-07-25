package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInvalidProfileSelectionNeverFallsBackToMutationGuidance(
	t *testing.T,
) {
	for _, test := range []struct {
		name          string
		selectProfile func(*testing.T)
	}{
		{
			name: "environment",
			selectProfile: func(t *testing.T) {
				t.Setenv(serverSelectionEnvVar, "BAD PROFILE")
			},
		},
		{
			name: "flag override",
			selectProfile: func(*testing.T) {
				setServerSelectionOverride("BAD PROFILE")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(
				t,
				`{"schema_version":1,"relay_base_url":"http://ha:8791"}`,
			)
			test.selectProfile(t)
			var output strings.Builder
			renderDurableCloudRecoveryGuidance(
				&output,
				paths,
				&cloudProblem{Remediation: cloudRemediationRetry},
			)
			rendered := output.String()
			if !strings.Contains(rendered, "cannot be resolved safely") ||
				!strings.Contains(rendered, "Repair default_server") ||
				strings.Contains(rendered, "cloud add") ||
				strings.Contains(rendered, "cloud reconnect") ||
				strings.Contains(rendered, "cloud remove") {
				t.Fatalf("invalid-selection recovery=%s", rendered)
			}
		})
	}
}

func TestUnsafeConfiguredDefaultNeverFallsBackToMutationGuidance(
	t *testing.T,
) {
	for _, test := range []struct {
		name   string
		config string
	}{
		{
			name: "missing configured default",
			config: `{
				"schema_version":3,
				"default_server":"missing",
				"servers":{
					"default":{
						"profile_id":"profile-default",
						"route_policy":"local",
						"relay_base_url":"http://ha:8791"
					}
				}
			}`,
		},
		{
			name: "invalid configured default",
			config: `{
				"schema_version":3,
				"default_server":"BAD PROFILE",
				"servers":{
					"BAD PROFILE":{
						"profile_id":"profile-invalid-default",
						"route_policy":"local",
						"relay_base_url":"http://ha:8791"
					}
				}
			}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(t, test.config)
			var output strings.Builder
			renderDurableCloudRecoveryGuidance(
				&output,
				paths,
				&cloudProblem{
					Code:        cloudProblemSecureStorage,
					Remediation: cloudRemediationUnlockStorage,
				},
			)
			rendered := output.String()
			if !strings.Contains(rendered, "cannot be resolved safely") ||
				!strings.Contains(rendered, "Repair default_server") ||
				strings.Contains(rendered, "cloud add") ||
				strings.Contains(rendered, "cloud unlock") ||
				strings.Contains(rendered, "cloud reconnect") ||
				strings.Contains(rendered, "cloud remove") ||
				strings.Contains(rendered, "setup --server") {
				t.Fatalf("unsafe-default recovery=%s", rendered)
			}
		})
	}
}

func TestDirectHeldGuidanceRejectsInvalidActiveProfile(t *testing.T) {
	resetServerProfileSelection(t)
	setActiveServerProfile("BAD PROFILE")
	cfg := runtimeConfig{
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
			RecoveryHold: &cloudRecoveryHold{
				Code:        cloudProblemAuthorization,
				Remediation: cloudRemediationSecurityStop,
			},
		},
	}
	var output strings.Builder
	renderCloudRecoveryGuidance(
		&output,
		cfg,
		cloudRecoveryHoldProblem(cfg),
	)
	rendered := output.String()
	if !strings.Contains(rendered, "selected server profile name is invalid") ||
		!strings.Contains(rendered, "Repair default_server") ||
		strings.Contains(rendered, "cloud add") ||
		strings.Contains(rendered, "cloud unlock") ||
		strings.Contains(rendered, "cloud reconnect") ||
		strings.Contains(rendered, "cloud remove") {
		t.Fatalf("direct invalid-profile guidance=%s", rendered)
	}
}

func TestCloudStatusJSONBestEffortProfileAttribution(t *testing.T) {
	for _, test := range []struct {
		name       string
		config     string
		args       []string
		env        string
		wantServer string
	}{
		{
			name: "configured default on parse error",
			config: strings.Replace(
				testV2TwoProfileConfig,
				`"default_server": "default"`,
				`"default_server": "cabin"`,
				1,
			),
			args:       []string{"--json", "--bogus"},
			wantServer: "cabin",
		},
		{
			name:       "explicit profile on load error",
			config:     testV2TwoProfileConfig,
			args:       []string{"--server", "missing", "--json"},
			wantServer: "missing",
		},
		{
			name: "configured default on document load error",
			config: `{
				"schema_version":3,
				"default_server":"cabin",
				"relay_base_url":false,
				"servers":{"cabin":{}}
			}`,
			args:       []string{"--json"},
			wantServer: "cabin",
		},
		{
			name:       "environment on parse error",
			config:     testV2TwoProfileConfig,
			args:       []string{"--json", "--bogus"},
			env:        "cabin",
			wantServer: "cabin",
		},
		{
			name:       "invalid explicit value",
			config:     testV2TwoProfileConfig,
			args:       []string{"--server", "BAD PROFILE", "--json"},
			wantServer: "BAD PROFILE",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := writeTestConfigFile(t, test.config)
			t.Setenv(serverSelectionEnvVar, test.env)
			exit, output := captureCommandOutput(t, func() int {
				return runCloudStatusCommand(paths, test.args)
			})
			var summary cloudStatusSummary
			if err := json.Unmarshal(
				[]byte(strings.TrimSpace(output)),
				&summary,
			); err != nil {
				t.Fatalf("status JSON=%q: %v", output, err)
			}
			if exit != 1 || summary.Server != test.wantServer {
				t.Fatalf(
					"status exit=%d server=%q want=%q summary=%+v",
					exit,
					summary.Server,
					test.wantServer,
					summary,
				)
			}
		})
	}
}
