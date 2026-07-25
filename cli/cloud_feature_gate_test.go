package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCloudRemoteFeatureGateFailsClosedForReleaseBuilds(t *testing.T) {
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()

	paths := writeCloudFeatureVersion(
		t,
		`{"skill_version":"1.0.0","min_relay_version":"1.0.0","cloud_remote_enabled":false,"cloud_remote_platforms":[]}`,
	)
	configureCloudRemoteFeature(paths)
	if cloudRemoteFeatureAvailable() {
		t.Fatal("disabled release metadata enabled Cloud Remote")
	}

	paths = writeCloudFeatureVersion(
		t,
		`{"skill_version":"1.0.0","min_relay_version":"1.0.0","cloud_remote_enabled":true,"cloud_remote_platforms":[]}`,
	)
	configureCloudRemoteFeature(paths)
	if cloudRemoteFeatureAvailable() {
		t.Fatal("enabled release metadata without validated platforms enabled Cloud Remote")
	}

	paths = writeCloudFeatureVersion(
		t,
		`{"skill_version":"1.0.0","min_relay_version":"1.0.0","cloud_remote_enabled":true,"cloud_remote_platforms":["darwin","darwin"]}`,
	)
	configureCloudRemoteFeature(paths)
	if cloudRemoteFeatureAvailable() {
		t.Fatal("duplicate release platforms enabled Cloud Remote")
	}
}

func TestCloudRemoteFeatureGateSelectsOnlyValidatedReleasePlatform(t *testing.T) {
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()

	metadata := fmt.Sprintf(
		`{"skill_version":"1.0.0","min_relay_version":"1.0.0","cloud_remote_enabled":true,"cloud_remote_platforms":[%q]}`,
		runtime.GOOS,
	)
	paths := writeCloudFeatureReleaseBundle(t, "1.0.0", "1.0.0", metadata)
	configureCloudRemoteFeature(paths)

	if cloudRemotePlatformSupported(runtime.GOOS) != cloudRemoteFeatureAvailable() {
		t.Fatalf(
			"release availability for %s = %v",
			runtime.GOOS,
			cloudRemoteFeatureAvailable(),
		)
	}
}

func TestCloudRemoteFeatureGateAllowsExplicitDeveloperBuild(t *testing.T) {
	restore := setCloudFeatureTestBuild(t, true)
	defer restore()

	configureCloudRemoteFeature(runtimePaths{
		VersionFile: filepath.Join(t.TempDir(), "missing-version.json"),
	})
	if cloudRemotePlatformSupported(runtime.GOOS) != cloudRemoteFeatureAvailable() {
		t.Fatalf(
			"developer availability for %s = %v",
			runtime.GOOS,
			cloudRemoteFeatureAvailable(),
		)
	}
}

func TestCloudRemoteDevAppSlugIsDeveloperBuildOnly(t *testing.T) {
	identity, restore := setCloudFeatureTestIdentity(t, cloudRemoteBuildIdentity{
		Development: true,
		AppSlug:     "local_ha_nova_cloud_beta",
	})
	defer restore()

	slug, err := selectedCloudNOVAAppSlug()
	if err != nil || slug != identity.AppSlug {
		t.Fatalf("developer App slug = %q, %v", slug, err)
	}

	for _, invalid := range []string{
		"",
		"../invalid",
		"ha_nova_cloud_beta",
		HAOfficialNOVAAppSlug,
		" local_ha_nova_cloud_beta",
	} {
		identity.AppSlug = invalid
		if _, err := selectedCloudNOVAAppSlug(); err == nil {
			t.Fatalf("invalid developer App slug %q was accepted", invalid)
		}
		if cloudRemoteFeatureAvailable() {
			t.Fatalf("invalid developer App slug %q enabled Cloud Remote", invalid)
		}
	}

	identity.Development = false
	identity.AppSlug = "local_ha_nova_cloud_beta"
	slug, err = selectedCloudNOVAAppSlug()
	if err != nil || slug != HAOfficialNOVAAppSlug {
		t.Fatalf("release App slug = %q, %v", slug, err)
	}
}

func TestCloudRemoteDevAppSlugCoversIngressValidationAndBrowserRoute(t *testing.T) {
	const devAppSlug = "local_ha_nova_cloud_beta"
	_, restore := setCloudFeatureTestIdentity(t, cloudRemoteBuildIdentity{
		Development: true,
		AppSlug:     devAppSlug,
	})
	defer restore()

	const ingressRoot = "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789"
	app := HAAddonInfo{
		Slug:         devAppSlug,
		State:        "started",
		Version:      "0.7.1",
		Ingress:      true,
		IngressEntry: ingressRoot,
		IngressURL:   ingressRoot + haNOVAIngressUIEntry,
	}
	if root, err := app.MachineIngressRoot(); err != nil || root != ingressRoot {
		t.Fatalf("developer App ingress root = %q, %v", root, err)
	}
	origin, err := ParseCanonicalNabuOrigin("https://unit.ui.nabu.casa")
	if err != nil {
		t.Fatal(err)
	}
	target, err := canonicalCloudAppURL(CloudOrigin{
		InputOrigin:     origin.String(),
		CanonicalOrigin: origin.String(),
		InputHost:       origin.Host,
		CanonicalHost:   origin.Host,
	}, app)
	if err != nil || target != "https://unit.ui.nabu.casa/app/"+devAppSlug {
		t.Fatalf("developer browser target = %q, %v", target, err)
	}
}

func TestCloudRemoteReleaseBuildIgnoresDevelopmentLDFlags(t *testing.T) {
	const childEnvironment = "HA_NOVA_CLOUD_LDFLAG_TEST_CHILD"
	if os.Getenv(childEnvironment) == "1" {
		previousIdentity := cloudRemoteBuildIdentityForRuntime
		cloudRemoteBuildIdentityForRuntime = compiledCloudRemoteBuildIdentity
		t.Cleanup(func() {
			cloudRemoteBuildIdentityForRuntime = previousIdentity
		})

		if cloudRemoteDevelopmentBuild() {
			t.Fatal("release build identity was enabled through linker flags")
		}
		if !cloudRemoteBuildIdentityForRuntime().Disabled {
			t.Fatal("ordinary go build was not fail-closed")
		}
		slug, err := selectedCloudNOVAAppSlug()
		if err != nil || slug != HAOfficialNOVAAppSlug {
			t.Fatalf("release App slug = %q, %v", slug, err)
		}
		return
	}

	command := exec.Command(
		"go",
		"test",
		"-run",
		"^TestCloudRemoteReleaseBuildIgnoresDevelopmentLDFlags$",
		"-count=1",
		"-ldflags",
		"-X github.com/markusleben/ha-nova/cli.CloudRemoteDevBuild=true -X github.com/markusleben/ha-nova/cli.CloudRemoteDevAppSlug=local_ldflag_attempt -X github.com/markusleben/ha-nova/cli.cloudRemoteDevAppSlug=local_ldflag_attempt",
		".",
	)
	command.Env = append(os.Environ(), childEnvironment+"=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("release ldflag isolation test failed: %v\n%s", err, output)
	}
}

func TestCloudRemoteBuildTagsSelectIsolatedIdentities(t *testing.T) {
	const childEnvironment = "HA_NOVA_CLOUD_BUILD_TAG_TEST_CHILD"
	if mode := os.Getenv(childEnvironment); mode != "" {
		previousIdentity := cloudRemoteBuildIdentityForRuntime
		cloudRemoteBuildIdentityForRuntime = compiledCloudRemoteBuildIdentity
		t.Cleanup(func() {
			cloudRemoteBuildIdentityForRuntime = previousIdentity
		})

		switch mode {
		case "disabled":
			identity := cloudRemoteBuildIdentityForRuntime()
			if !identity.Disabled || identity.Development {
				t.Fatalf("disabled build identity = %+v", identity)
			}
			metadata := fmt.Sprintf(
				`{"skill_version":"1.0.0","min_relay_version":"1.0.0","cloud_remote_enabled":true,"cloud_remote_platforms":[%q]}`,
				runtime.GOOS,
			)
			configureCloudRemoteFeature(writeCloudFeatureVersion(t, metadata))
			if cloudRemoteFeatureAvailable() {
				t.Fatal("cloudremote_disabled build accepted enabled metadata")
			}
		case "development":
			identity := cloudRemoteBuildIdentityForRuntime()
			if identity.Disabled || !identity.Development {
				t.Fatalf("development build identity = %+v", identity)
			}
			slug, err := selectedCloudNOVAAppSlug()
			if err != nil || slug != "local_ha_nova_build_tag" {
				t.Fatalf("development build App slug = %q, %v", slug, err)
			}
		case "official":
			identity := cloudRemoteBuildIdentityForRuntime()
			if identity.Disabled || identity.Development || !identity.Official {
				t.Fatalf("official build identity = %+v", identity)
			}
		default:
			t.Fatalf("unknown build-tag child mode %q", mode)
		}
		return
	}

	for _, testCase := range []struct {
		mode    string
		tag     string
		ldflags string
	}{
		{mode: "disabled", tag: "cloudremote_disabled"},
		{mode: "official", tag: "cloudremote_official"},
		{
			mode:    "development",
			tag:     "cloudremote_dev",
			ldflags: "-X github.com/markusleben/ha-nova/cli.cloudRemoteDevAppSlug=local_ha_nova_build_tag",
		},
	} {
		t.Run(testCase.mode, func(t *testing.T) {
			arguments := []string{
				"test",
				"-run",
				"^TestCloudRemoteBuildTagsSelectIsolatedIdentities$",
				"-count=1",
				"-tags",
				testCase.tag,
			}
			if testCase.ldflags != "" {
				arguments = append(arguments, "-ldflags", testCase.ldflags)
			}
			arguments = append(arguments, ".")
			command := exec.Command("go", arguments...)
			command.Env = append(
				os.Environ(),
				childEnvironment+"="+testCase.mode,
			)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("%s build-tag isolation failed: %v\n%s", testCase.mode, err, output)
			}
		})
	}
}

func writeCloudFeatureVersion(t *testing.T, content string) runtimePaths {
	t.Helper()
	path := filepath.Join(t.TempDir(), "version.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return runtimePaths{VersionFile: path}
}

func setCloudFeatureTestBuild(t *testing.T, development bool) func() {
	t.Helper()
	identity := cloudRemoteBuildIdentity{Official: true}
	if development {
		identity = cloudRemoteBuildIdentity{
			Development: true,
			AppSlug:     "local_ha_nova_test",
		}
	}
	_, restore := setCloudFeatureTestIdentity(t, identity)
	return restore
}

func setCloudFeatureTestIdentity(
	t *testing.T,
	identity cloudRemoteBuildIdentity,
) (*cloudRemoteBuildIdentity, func()) {
	t.Helper()
	previousIdentity := cloudRemoteBuildIdentityForRuntime
	previousEnabled := cloudRemoteReleaseEnabled
	previousPlatforms := cloudRemoteReleasePlatforms
	previousVersion := Version
	previousExecutable := executablePathForInstallSource
	current := identity
	cloudRemoteBuildIdentityForRuntime = func() cloudRemoteBuildIdentity {
		return current
	}
	return &current, func() {
		cloudRemoteBuildIdentityForRuntime = previousIdentity
		cloudRemoteReleaseEnabled = previousEnabled
		cloudRemoteReleasePlatforms = previousPlatforms
		Version = previousVersion
		executablePathForInstallSource = previousExecutable
	}
}
