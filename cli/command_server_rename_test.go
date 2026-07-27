package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerRenameRejectsCredentialBearingProfile(t *testing.T) {
	config := strings.Replace(
		testV2TwoProfileConfig,
		`"default_server": "default"`,
		`"default_server": "cabin"`,
		1,
	)
	paths := setupServerCommandTest(t, config)
	if err := secretSet(
		deviceCredentialServiceForProfile("cabin"),
		testProfileCredentialB,
	); err != nil {
		t.Fatal(err)
	}
	if err := secretSet(
		deviceCredentialPendingServiceForProfile("cabin"),
		testProfileCredentialB,
	); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(
			paths,
			[]string{"rename", "cabin", "seaside"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, "stored device credentials") ||
		!strings.Contains(output, "cannot be stranded") {
		t.Fatalf("rename exit=%d output=%q", exit, output)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("credential-bearing rename changed config")
	}
	for _, service := range []string{
		deviceCredentialServiceForProfile("cabin"),
		deviceCredentialPendingServiceForProfile("cabin"),
	} {
		if got, ok, err := readCredentialSlot(service); err != nil ||
			!ok ||
			got != testProfileCredentialB {
			t.Fatalf(
				"source slot %s changed: %q ok=%v err=%v",
				service,
				got,
				ok,
				err,
			)
		}
	}
}

func TestServerRenameRefusals(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{"literal default", []string{"rename", "default", "home"}, "cannot be renamed"},
		{"to default", []string{"rename", "cabin", "default"}, "reserved for the legacy-token profile"},
		{"unknown old", []string{"rename", "nope", "home"}, "unknown server profile"},
		{"to existing", []string{"rename", "cabin", "lake"}, "already exists"},
		{"invalid new", []string{"rename", "cabin", "Bad_Name"}, "invalid server profile name"},
		{"reserved new", []string{"rename", "cabin", "pending"}, "reserved"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			paths := setupServerCommandTest(
				t,
				testV2ThreeProfileConfig,
			)
			if err := secretSet(
				deviceCredentialServiceForProfile("cabin"),
				testProfileCredentialB,
			); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(paths.ConfigFile)
			exit, output := captureCommandOutput(t, func() int {
				return runServerCommand(paths, testCase.args)
			})
			if exit != 1 ||
				!strings.Contains(output, testCase.wantMsg) {
				t.Fatalf(
					"exit=%d missing %q: %s",
					exit,
					testCase.wantMsg,
					output,
				)
			}
			after, _ := os.ReadFile(paths.ConfigFile)
			if string(before) != string(after) {
				t.Fatal("refused rename changed config")
			}
		})
	}
}

func TestServerRenameRejectsRawPendingFileWithoutMarker(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	oldPath, err := deviceSecretFilePath(
		deviceCredentialPendingServiceForProfile("cabin"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		oldPath,
		[]byte(testProfileCredentialB),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(
			paths,
			[]string{"rename", "cabin", "seaside"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, "raw device credential file") {
		t.Fatalf("rename exit=%d output=%q", exit, output)
	}
	if data, err := os.ReadFile(oldPath); err != nil ||
		string(data) != testProfileCredentialB {
		t.Fatalf("source file changed: data=%q err=%v", data, err)
	}
	newPath, err := deviceSecretFilePath(
		deviceCredentialPendingServiceForProfile("seaside"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(newPath); !os.IsNotExist(err) {
		t.Fatalf("destination exists after refusal: %v", err)
	}
}

func TestServerRenameRejectsOccupiedRawDestinationCredential(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	destinationCredential := validCredential(91)
	oldPath, err := deviceSecretFilePath(
		deviceCredentialPendingServiceForProfile("cabin"),
	)
	if err != nil {
		t.Fatal(err)
	}
	newPath, err := deviceSecretFilePath(
		deviceCredentialPendingServiceForProfile("seaside"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		oldPath,
		[]byte(testProfileCredentialB),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		newPath,
		[]byte(destinationCredential),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(
			paths,
			[]string{"rename", "cabin", "seaside"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, "device credential") {
		t.Fatalf("rename exit=%d output=%s", exit, output)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("occupied destination rename changed config")
	}
	for path, want := range map[string]string{
		oldPath: testProfileCredentialB,
		newPath: destinationCredential,
	} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf(
				"credential %s changed: data=%q err=%v",
				path,
				data,
				err,
			)
		}
	}
}

func TestServerRenameHeadlessRejectsMarkerlessPendingFile(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", "")
	previousPreflight := deviceCredentialPreflight
	deviceCredentialPreflight = func() error {
		return errDesktopKeyringSessionUnavailable
	}
	t.Cleanup(func() {
		deviceCredentialPreflight = previousPreflight
	})
	oldPath, err := deviceSecretFilePath(
		deviceCredentialPendingServiceForProfile("cabin"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		oldPath,
		[]byte(testProfileCredentialB),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(
			paths,
			[]string{"rename", "cabin", "seaside"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, "cannot prove credential slot") {
		t.Fatalf("headless rename exit=%d output=%s", exit, output)
	}
	newPath, err := deviceSecretFilePath(
		deviceCredentialPendingServiceForProfile("seaside"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(newPath); !os.IsNotExist(err) {
		t.Fatalf("destination exists after refusal: %v", err)
	}
	if data, err := os.ReadFile(oldPath); err != nil ||
		string(data) != testProfileCredentialB {
		t.Fatalf("source file changed: data=%q err=%v", data, err)
	}
}
