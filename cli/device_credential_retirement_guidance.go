package main

import "fmt"

func deviceRetirementSetupCommand(profile string) string {
	if profile == "" || profile == defaultServerProfileName {
		return "ha-nova setup"
	}
	return fmt.Sprintf("ha-nova setup --server %s", profile)
}
