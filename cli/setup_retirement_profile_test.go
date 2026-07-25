package main

import (
	"os"
	"strings"
	"testing"
)

func TestResolveSetupRetirementProfileSelectionPrecedence(
	t *testing.T,
) {
	tests := []struct {
		name       string
		config     string
		env        string
		explicit   string
		want       string
		wantErr    bool
		removeFile bool
	}{
		{
			name:   "configured named default",
			config: strings.Replace(testV2ThreeProfileConfig, `"default_server": "default"`, `"default_server": "cabin"`, 1),
			want:   "cabin",
		},
		{
			name:   "environment beats configured default",
			config: testV2ThreeProfileConfig,
			env:    "cabin",
			want:   "cabin",
		},
		{
			name:     "explicit beats environment",
			config:   testV2ThreeProfileConfig,
			env:      "cabin",
			explicit: "lake",
			want:     "lake",
		},
		{
			name:       "missing config is fresh default",
			config:     `{"schema_version":1}`,
			want:       defaultServerProfileName,
			removeFile: true,
		},
		{
			name:    "unreadable config has no fallback",
			config:  `{`,
			wantErr: true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			paths := setupServerCommandTest(t, testCase.config)
			t.Setenv(serverSelectionEnvVar, testCase.env)
			if testCase.explicit != "" {
				setServerSelectionOverride(testCase.explicit)
			}
			if testCase.removeFile {
				if err := os.Remove(paths.ConfigFile); err != nil {
					t.Fatal(err)
				}
			}

			got, err := resolveSetupRetirementProfile(paths)
			if testCase.wantErr {
				if err == nil || got != "" {
					t.Fatalf("profile=%q err=%v", got, err)
				}
				return
			}
			if err != nil || got != testCase.want {
				t.Fatalf(
					"profile=%q want=%q err=%v",
					got,
					testCase.want,
					err,
				)
			}
		})
	}
}

func TestConfiguredNamedDefaultResumesRetirementNonInteractively(
	t *testing.T,
) {
	config := strings.Replace(
		testV2ThreeProfileConfig,
		`"default_server": "default"`,
		`"default_server": "cabin"`,
		1,
	)
	paths := setupServerCommandTest(t, config)
	seedDeviceRetirementCheckpointForProfile(
		t,
		paths,
		"cabin",
		deviceCredentialRetirementPrepared,
	)
	withClientRuntimeAvailability(t, map[string]bool{})

	exit, output := captureCommandOutput(t, func() int {
		return runSetup(paths, []string{"--non-interactive"})
	})
	if exit != 0 ||
		strings.Contains(output, "only for Cloud-only resume") ||
		strings.Contains(output, "no supported AI clients") {
		t.Fatalf(
			"configured named retirement exit=%d: %s",
			exit,
			output,
		)
	}
	pending, err :=
		deviceCredentialRetirementCheckpointExistsForProfile(
			paths,
			"cabin",
		)
	if err != nil || pending {
		t.Fatalf(
			"configured named checkpoint pending=%v err=%v",
			pending,
			err,
		)
	}
}

func TestUnresolvedRetirementCheckpointDetectionCoversEveryProfile(
	t *testing.T,
) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		t.Run(profile, func(t *testing.T) {
			paths := setupServerCommandTest(t, `{"schema_version":1}`)
			path, err :=
				deviceCredentialRetirementCheckpointPathForProfile(
					paths,
					profile,
				)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			exists, err :=
				setupRetirementCheckpointExistsWithUnresolvedProfile(
					paths,
				)
			if err != nil || !exists {
				t.Fatalf(
					"profile=%s exists=%v err=%v",
					profile,
					exists,
					err,
				)
			}
		})
	}
}
