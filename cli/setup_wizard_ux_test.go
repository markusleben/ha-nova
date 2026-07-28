package main

import (
	"strings"
	"testing"
)

func TestOptionalCloudAddNextStepIsMinimalAndScoped(t *testing.T) {
	resetServerProfileSelection(t)
	coordinator := successfulCloudCoordinatorForTest()
	installCloudSetupTestSeams(t, coordinator, true, true)

	cfg := completedLocalCloudTestConfig()
	var output strings.Builder
	renderOptionalCloudAddNextStep(&output, cfg, false)
	for _, want := range []string{
		"Away-from-home access is optional and not configured.",
		"Add it later with: ha-nova cloud add --server default",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}

	for _, test := range []struct {
		name    string
		config  runtimeConfig
		service bool
	}{
		{name: "service", config: cfg, service: true},
		{name: "unpaired", config: runtimeConfig{}},
		{
			name: "already configured",
			config: func() runtimeConfig {
				configured := cfg
				configured.Cloud = &cloudLifecycleMetadata{
					State: cloudStateReady,
				}
				return configured
			}(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var hidden strings.Builder
			renderOptionalCloudAddNextStep(
				&hidden,
				test.config,
				test.service,
			)
			if hidden.Len() != 0 {
				t.Fatalf("unexpected Cloud next step:\n%s", hidden.String())
			}
		})
	}
}
