package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// runPairCommand pairs this install with a NOVA Relay using a one-time code from
// the NOVA owner page. The passwordless secure flow: OPAQUE over the bootstrap
// port, then a device credential over SPKI-pinned TLS. Stores the credential and
// the secure endpoint; a re-pair replaces the old credential only after the new
// one activates.
func runPairCommand(paths runtimePaths, args []string) int {
	relayURL, code := "", ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--relay-url":
			if i+1 < len(args) {
				relayURL = args[i+1]
				i++
			}
		case "--code":
			if i+1 < len(args) {
				code = args[i+1]
				i++
			}
		case "-h", "--help":
			fmt.Println("Usage: ha-nova pair [--relay-url http://<ha-host>:8791] [--code NNNNNN]")
			fmt.Println("Open NOVA in the Home Assistant sidebar, click \"Connect a device\", then run this.")
			return 0
		default:
			printErr("unknown flag: %s", args[i])
			return 1
		}
	}

	// Pairing can run before a full setup as long as a relay URL is known: an
	// explicit --relay-url starts from a fresh config, otherwise the saved one.
	cfg, cfgErr := loadConfig(paths)
	bootstrapURL := strings.TrimSpace(relayURL)
	if bootstrapURL == "" {
		if cfgErr != nil {
			printErr("no relay URL known; pass --relay-url http://<ha-host>:8791 or run 'ha-nova setup' first")
			return 1
		}
		bootstrapURL = cfg.RelayBaseURL
	}
	if bootstrapURL == "" {
		printErr("no relay URL known; pass --relay-url http://<ha-host>:8791 or run 'ha-nova setup' first")
		return 1
	}
	if cfgErr != nil {
		cfg = runtimeConfig{RelayBaseURL: bootstrapURL}
	}

	if code == "" {
		entered, promptErr := promptWizardLineFromReader(bufio.NewReader(os.Stdin), os.Stdout, "Six-digit code from the NOVA page", "")
		if promptErr != nil {
			return 1
		}
		code = entered
	}
	normalized, err := normalizeRelayPairingCode(code)
	if err != nil {
		printErr("%s", err)
		return 1
	}

	save := func(c *runtimeConfig) error { return saveConfig(paths, *c) }
	deviceID, err := runSecurePairing(bootstrapURL, normalized, &cfg, save, defaultPairingClientInfo())
	if err != nil {
		switch {
		case errors.Is(err, errPairingCodeRejected):
			printErr("That code was not accepted. Generate a fresh one in NOVA and try again.")
		case errors.Is(err, errPairingInactive):
			printErr("No active code. Open NOVA in the sidebar and click \"Connect a device\" first.")
		case errors.Is(err, errRelayNotV1):
			printErr("This relay does not support secure pairing yet. Update the NOVA Relay App.")
		case errors.Is(err, errPinMismatch):
			printErr("The relay's secure identity did not match its fingerprint. Try pairing again; if it repeats, someone may be intercepting the connection.")
		default:
			printErr("Pairing failed: %s", err)
		}
		return 1
	}
	fmt.Printf("Paired securely. This device is connected (id %s).\n", deviceID)
	return 0
}
