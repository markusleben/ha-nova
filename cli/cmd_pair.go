package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Hook so the pair command's config seeding can be tested without a live relay.
var runSecurePairingForPairCmd = runSecurePairing

// runPairCommand pairs this install with a NOVA Relay using a one-time code from
// the NOVA owner page. The passwordless secure flow: OPAQUE over the bootstrap
// port, then a device credential over SPKI-pinned TLS. Stores the credential and
// the secure endpoint; a re-pair replaces the old credential only after the new
// one activates.
func runPairCommand(paths runtimePaths, args []string) int {
	relayURL, code, credentialStore := "", "", ""
	credentialStoreSet := false
	for i := 0; i < len(args); i++ {
		// Accept both `--flag value` and `--flag=value` for the string flags.
		name, inlineValue, hasInline := args[i], "", false
		if strings.HasPrefix(name, "--") {
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name, inlineValue, hasInline = name[:eq], name[eq+1:], true
			}
		}
		takeValue := func() string {
			if hasInline {
				return inlineValue
			}
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch name {
		case "--relay-url":
			relayURL = takeValue()
		case "--code":
			code = takeValue()
		case "--credential-store":
			credentialStore = takeValue()
			credentialStoreSet = true
		case "-h", "--help":
			fmt.Println("Usage: ha-nova pair [--relay-url http://<ha-host>:8791] [--code NNNNNN] [--credential-store=file]")
			fmt.Println("Open NOVA in the Home Assistant sidebar, click \"Connect a device\", then run this.")
			fmt.Println("--credential-store=file keeps the device credential in a private file — for headless systems and VMs whose desktop keyring is never unlocked.")
			return 0
		default:
			printErr("unknown flag: %s", args[i])
			return 1
		}
	}
	if credentialStoreSet && credentialStore != "file" {
		printErr("--credential-store supports only the value \"file\" (got %q)", credentialStore)
		return 1
	}
	if credentialStore == "file" {
		// A readable keyring credential moves along BEFORE the backend flips:
		// the flip must never mask a live desktop pairing. Locked or absent
		// keyrings make this a silent no-op and pairing continues file-backed.
		migrated, migrateErr := migrateKeyringDeviceCredentialToFile()
		if migrateErr != nil {
			printErr("cannot move the device credential into private file storage: %s", migrateErr)
			printErr("Nothing was paired — no code was used.")
			return 1
		}
		if migrated {
			fmt.Println("Moved this install's device credential into private file storage.")
		}
		forceDeviceCredentialFileMode()
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
	} else if strings.TrimSpace(relayURL) != "" {
		// An explicit --relay-url must persist for later functional calls, not
		// just drive this one pairing — the successful pairing saves cfg.
		cfg.RelayBaseURL = bootstrapURL
	}

	// Verify credential storage BEFORE asking for (or spending) a code: a broken
	// backend must not burn the owner's one-time code. On headless systems this
	// engages the private-file fallback and says so.
	probe, probeErr := probeDeviceCredentialStorage()
	if probeErr != nil {
		printErr("This system cannot store the device credential yet: %s", probeErr)
		printErr("Nothing was paired — no code was used.")
		return 1
	}
	if probe.note != "" {
		fmt.Println(probe.note)
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
	deviceID, err := runSecurePairingForPairCmd(bootstrapURL, normalized, &cfg, save, defaultPairingClientInfo())
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
