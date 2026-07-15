package main

type setupWizardSteps struct {
	Total        int
	RelayInstall int
	RelayToken   int
	LLAT         int
	Pairing      int
	Verify       int
	Skills       int
}

func buildSetupPairingWizardSteps() setupWizardSteps {
	return setupWizardSteps{
		Total:        5,
		RelayInstall: 1,
		LLAT:         2,
		Pairing:      3,
		Verify:       4,
		Skills:       5,
	}
}

func buildSetupWizardSteps(includeLLAT bool) setupWizardSteps {
	steps := setupWizardSteps{
		Total:        4,
		RelayInstall: 1,
		RelayToken:   2,
		Verify:       3,
		Skills:       4,
	}
	if includeLLAT {
		steps.Total = 5
		steps.LLAT = 3
		steps.Verify = 4
		steps.Skills = 5
	}
	return steps
}
