package main

import (
	"bufio"
	"fmt"
	"os"
)

// confirmCloudRemove asks `cloud remove` without --yes; code is the exit
// when it doesn't go on.
func confirmCloudRemove(remoteOnly bool) (proceed bool, code int) {
	if !uiInputSupportsTTY() || !stdoutIsInteractiveTTY() {
		printHumanErr("cloud remove requires confirmation; rerun with --yes")
		return false, 1
	}
	fmt.Fprintln(os.Stdout, "This revokes HA NOVA's Home Assistant Link authorization.")
	if remoteOnly {
		fmt.Fprintln(
			os.Stdout,
			"The Cloud-only NOVA device pairing is revoked too; the Home Assistant Link subscription stays unchanged.",
		)
	} else {
		fmt.Fprintln(
			os.Stdout,
			"The local NOVA device pairing and Home Assistant Link subscription stay unchanged.",
		)
	}
	confirmed, promptErr := promptYesNoMenu(
		bufio.NewReader(os.Stdin),
		os.Stdout,
		"Remove Home Assistant Link access?",
		"Yes, remove it",
		"No, keep it",
		false,
	)
	if promptErr != nil || !confirmed {
		printHumanInfo("Home Assistant Link stays. Nothing was changed.")
		return false, 0
	}
	return true, 0
}
