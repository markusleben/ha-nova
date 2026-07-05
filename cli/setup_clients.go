package main

import "fmt"

type setupClientChoice struct {
	Number         string
	Value          string
	Label          string
	Disabled       bool
	DisabledReason string
	Resolved       []string
}

func buildSetupClientChoices(paths runtimePaths, state installState) ([]setupClientChoice, error) {
	availableChoices := []setupClientChoice{}
	disabledChoices := []setupClientChoice{}
	available := []string{}
	clients, err := loadClientRegistry(paths)
	if err != nil {
		return nil, err
	}
	for _, client := range clients {
		status := evaluateClientStatus(paths, state, client)
		choice := setupClientChoice{
			Value: client.ID,
			Label: client.Label,
		}
		if !status.SupportedOnOS || !status.RuntimeDetected {
			choice.Disabled = true
			choice.DisabledReason = status.Reason
			disabledChoices = append(disabledChoices, choice)
		} else {
			choice.Resolved = []string{client.ID}
			available = append(available, client.ID)
			availableChoices = append(availableChoices, choice)
		}
	}

	choices := append(append([]setupClientChoice{}, availableChoices...), disabledChoices...)
	for index := range choices {
		choices[index].Number = indexNumber(index + 1)
	}

	allChoice := setupClientChoice{
		Number:   indexNumber(len(choices) + 1),
		Value:    "all",
		Label:    "All available clients",
		Resolved: append([]string{}, available...),
	}
	if len(available) == 0 {
		allChoice.Disabled = true
		allChoice.DisabledReason = "install a supported AI client first"
	}
	return append(choices, allChoice), nil
}

func indexNumber(value int) string {
	return fmt.Sprintf("%d", value)
}

func resolveSetupClients(paths runtimePaths, target string) ([]string, []string, error) {
	state, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		return nil, nil, err
	}
	return resolveSetupClientsWithState(paths, target, state)
}

func resolveSetupClientsWithState(paths runtimePaths, target string, state installState) ([]string, []string, error) {
	choices, err := buildSetupClientChoices(paths, state)
	if err != nil {
		return nil, nil, err
	}
	return resolveSetupClientsWithChoices(choices, target)
}

func resolveSetupClientsWithChoices(choices []setupClientChoice, target string) ([]string, []string, error) {
	target = canonicalClientID(target)
	for _, choice := range choices {
		if choice.Value != target {
			continue
		}
		if choice.Disabled {
			return nil, nil, fmt.Errorf("%s is not available yet: %s", choice.Label, choice.DisabledReason)
		}
		if choice.Value == "all" {
			skipped := []string{}
			for _, other := range choices {
				if other.Value == "all" || !other.Disabled {
					continue
				}
				skipped = append(skipped, other.Label)
			}
			return append([]string{}, choice.Resolved...), skipped, nil
		}
		return append([]string{}, choice.Resolved...), nil, nil
	}
	return nil, nil, fmt.Errorf("unsupported client: %s", target)
}
