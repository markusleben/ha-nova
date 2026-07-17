package main

type setupWizardSteps struct {
	Total        int
	RelayInstall int
	RelayToken   int
	Pairing      int
	Verify       int
	Skills       int
}

func buildSetupPairingWizardSteps() setupWizardSteps {
	return setupWizardSteps{
		Total:        4,
		RelayInstall: 1,
		Pairing:      2,
		Verify:       3,
		Skills:       4,
	}
}

func buildSetupWizardSteps() setupWizardSteps {
	return setupWizardSteps{
		Total:        4,
		RelayInstall: 1,
		RelayToken:   2,
		Verify:       3,
		Skills:       4,
	}
}
