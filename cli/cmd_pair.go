package main

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
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
	relayURL, code, credentialStore, serverName := "", "", "", ""
	relayURLSet, codeSet, credentialStoreSet, serverNameSet := false, false, false, false
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
			relayURLSet = true
		case "--code":
			code = takeValue()
			codeSet = true
		case "--credential-store":
			credentialStore = takeValue()
			credentialStoreSet = true
		case "--server":
			serverName = takeValue()
			serverNameSet = true
		case "-h", "--help":
			fmt.Println("Usage: ha-nova pair [--relay-url http://<ha-host>:8791] [--code NNNNNN] [--credential-store=file] [--server <name>]")
			fmt.Println("Open NOVA in the Home Assistant sidebar, click \"Connect a device\", then run this.")
			fmt.Println("--credential-store=file keeps the device credential in a private file — for headless systems and VMs whose desktop keyring is never unlocked.")
			fmt.Println("--server <name> pairs into the named server profile (multi-server installs); a NEW profile also needs --relay-url.")
			return 0
		default:
			printErr("unknown flag: %s", args[i])
			return 1
		}
	}
	if relayURLSet && strings.TrimSpace(relayURL) == "" {
		printErr("--relay-url requires a non-empty URL; nothing was paired")
		return 1
	}
	if codeSet && strings.TrimSpace(code) == "" {
		printErr("--code requires a non-empty pairing code; nothing was paired")
		return 1
	}
	if serverNameSet && strings.TrimSpace(serverName) == "" {
		printErr("--server requires a non-empty profile name; nothing was paired")
		return 1
	}
	if credentialStoreSet && credentialStore != "file" {
		printErr("--credential-store supports only the value \"file\" (got %q)", credentialStore)
		return 1
	}
	if serverNameSet {
		if err := validateUTF8String(serverName, "server profile name"); err != nil {
			printErr("%s; nothing was paired", err)
			return 1
		}
		if err := validateServerProfileName(serverName); err != nil {
			printErr("%s", err)
			return 1
		}
	}
	bootstrapURL := strings.TrimSpace(relayURL)
	if relayURLSet {
		if err := validatePairRelayURL(bootstrapURL); err != nil {
			printErr("%s; nothing was paired", err)
			return 1
		}
	}
	normalizedCode := ""
	if codeSet {
		if err := validateUTF8String(code, "pairing code"); err != nil {
			printErr("%s; nothing was paired", err)
			return 1
		}
		var err error
		normalizedCode, err = normalizeRelayPairingCode(code)
		if err != nil {
			printErr("%s; nothing was paired", err)
			return 1
		}
	}
	if serverNameSet {
		// Route this run — config saves AND credential slots — to the named
		// profile before anything touches storage. The seam is set here too so
		// the storage probe/migration below already use the profile's slots.
		setServerSelectionOverride(serverName)
		setActiveServerProfile(serverName)
	}
	configSnapshot, hadConfigSnapshot, snapshotErr := readOptionalFile(paths.ConfigFile)
	if snapshotErr != nil {
		printErr("cannot inspect server configuration: %s", snapshotErr)
		return 1
	}
	pairLifecycleGeneration, lifecycleErr := readInstallLifecycleGeneration(paths)
	if lifecycleErr != nil {
		printErr("cannot inspect install lifecycle: %s", lifecycleErr)
		return 1
	}
	if censusLifecycleStopped(paths) {
		printErr("HA NOVA was uninstalled; run `ha-nova setup` before pairing.")
		return 1
	}

	// Pairing can run before a full setup as long as a relay URL is known: an
	// explicit --relay-url starts from a fresh config, otherwise the saved one.
	// A NEW profile name has no saved relay URL by definition — falling back to
	// another profile's URL would bootstrap against the wrong server, so it is a
	// hard error without --relay-url.
	cfg, cfgErr := loadConfig(paths)
	newProfile := serverNameSet && errors.Is(cfgErr, errUnknownServerProfile)
	if cfgErr != nil && errors.Is(cfgErr, errUnknownServerProfile) && !newProfile {
		// Selection came from HA_NOVA_SERVER: a typo must fail loud, never
		// silently pair a fresh profile.
		printErr("%s", cfgErr)
		return 1
	}
	if !serverNameSet && cfgErr != nil {
		// loadConfig failed before profile resolution (missing/incomplete
		// config), so an env-only selection never reached the credential seam —
		// saves would target the env profile while credentials land in the
		// default slots. Creation always requires the explicit flag.
		if name, source := requestedServerSelection(); name != "" && name != defaultServerProfileName {
			printErr("profile %q (from %s) is not set up; create it explicitly: ha-nova pair --server %s --relay-url http://<ha-host>:8791", name, source, name)
			return 1
		}
	}
	if !relayURLSet {
		if newProfile {
			printErr("server profile %q does not exist yet; pass --relay-url http://<ha-host>:8791 to create it", serverName)
			return 1
		}
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
	if err := validatePairRelayURL(bootstrapURL); err != nil {
		printErr("saved %s; nothing was paired", err)
		return 1
	}
	resumedActivation := false
	if cfgErr != nil {
		cfg, cfgErr = recoverPairConfigForExplicitRelayURL(
			paths,
			bootstrapURL,
			cfgErr,
			newProfile,
			hadConfigSnapshot,
		)
		if cfgErr != nil {
			printErr(
				"cannot safely pair with the saved server configuration: %s",
				cfgErr,
			)
			printErr("Nothing was paired — no code was used.")
			return 1
		}
	}
	if err := validateRuntimeConfigSave(paths, cfg); err != nil {
		printErr(
			"cannot safely pair with the saved server configuration: %s",
			err,
		)
		printErr("Nothing was paired — no code was used.")
		return 1
	}
	// Resume a durable local activation before an explicit relay override,
	// storage migration/probing, or another one-time code can mutate or
	// overwrite it. Valid incomplete profiles recovered above retain their
	// pending endpoints, so they use the same recovery path.
	var resumeErr error
	resumedActivation, resumeErr = resumeInterruptedPairingForDoctor(
		paths,
		&cfg,
		pairLifecycleGeneration,
		configSnapshot,
		hadConfigSnapshot,
	)
	if resumeErr != nil {
		printPendingActivationResumeError(resumeErr)
		return 1
	}
	if resumedActivation && credentialStore != "file" {
		fmt.Println("Resumed the interrupted secure device activation.")
		return 0
	}
	if resumedActivation {
		configSnapshot, hadConfigSnapshot, resumeErr =
			readOptionalFile(paths.ConfigFile)
		if resumeErr != nil {
			printErr(
				"cannot verify the resumed server configuration: %s",
				resumeErr,
			)
			return 1
		}
	}
	if !resumedActivation && relayURLSet {
		// An explicit --relay-url must persist for later functional calls, not
		// just drive this one pairing — the successful pairing saves cfg.
		cfg.RelayBaseURL = bootstrapURL
	}
	// Reject Cloud/local replacement conflicts before migration, storage probes,
	// or a six-digit-code prompt. The inner pairing guard remains as a
	// concurrency-safe defense at the point of use.
	guardPairMutation := func() error {
		if err := ensureUpdateLifecycleCurrent(paths, pairLifecycleGeneration); err != nil {
			return err
		}
		if err := ensureOptionalFileSnapshotCurrent(
			paths.ConfigFile,
			configSnapshot,
			hadConfigSnapshot,
		); err != nil {
			return err
		}
		return requireSettledDeviceCredentialRetirement(
			paths,
			activeServerProfile(),
		)
	}
	guardErr := withClientMutationLock(paths, func() error {
		if err := guardPairMutation(); err != nil {
			return err
		}
		return validateLocalDeviceReplacementAllowed(cfg)
	})
	if guardErr != nil {
		printErr("Pairing cannot start: %s", guardErr)
		return 1
	}
	// Storage mutation only AFTER every selection/bootstrap guard above: a pair
	// that exits before pairing must never have flipped the backend or moved
	// credentials.
	if credentialStore == "file" {
		// A readable keyring credential moves along BEFORE the backend flips:
		// the flip must never mask a live desktop pairing. Locked or absent
		// keyrings make this a silent no-op and pairing continues file-backed.
		migrated := false
		migrateErr := withClientMutationLock(paths, func() error {
			if err := guardPairMutation(); err != nil {
				return err
			}
			var err error
			migrated, err = migrateKeyringDeviceCredentialToFile()
			return err
		})
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
	if resumedActivation {
		fmt.Println("Resumed the interrupted secure device activation.")
		return 0
	}
	// Verify credential storage BEFORE asking for (or spending) a code: a broken
	// backend must not burn the owner's one-time code. On headless systems this
	// engages the private-file fallback and says so.
	var probe deviceStorageProbe
	probeErr := withClientMutationLock(paths, func() error {
		if err := guardPairMutation(); err != nil {
			return err
		}
		var err error
		probe, err = probeDeviceCredentialStorage()
		return err
	})
	if probeErr != nil {
		printErr("This system cannot store the device credential yet: %s", probeErr)
		printErr("Nothing was paired — no code was used.")
		return 1
	}
	if probe.note != "" {
		fmt.Println(probe.note)
	}

	if !codeSet {
		entered, promptErr := promptWizardLineFromReader(bufio.NewReader(os.Stdin), os.Stdout, "Six-digit code from the NOVA page", "")
		if promptErr != nil {
			return 1
		}
		code = entered
		normalized, normalizeErr := normalizeRelayPairingCode(code)
		if normalizeErr != nil {
			printErr("%s", normalizeErr)
			return 1
		}
		normalizedCode = normalized
	}

	deviceID := ""
	err := withClientMutationLock(paths, func() error {
		if err := guardPairMutation(); err != nil {
			return err
		}
		save := func(c *runtimeConfig) error { return saveConfig(paths, *c) }
		var pairErr error
		deviceID, pairErr = runSecurePairingForPairCmd(bootstrapURL, normalizedCode, &cfg, save, defaultPairingClientInfo())
		return pairErr
	})
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

func validatePairRelayURL(value string) error {
	if err := validateUTF8String(value, "relay URL"); err != nil {
		return err
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return fmt.Errorf("relay URL is invalid: %w", err)
	}
	hostname := parsed.Hostname()
	if (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) || hostname == "" {
		return errors.New("relay URL must be an absolute HTTP(S) URL with a host")
	}
	if err := validateUTF8String(hostname, "relay URL host"); err != nil {
		return err
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return errors.New("relay URL port must not be empty")
	}
	if port := parsed.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return errors.New("relay URL port must be between 1 and 65535")
		}
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.ForceQuery || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("relay URL must be a base URL without a path, query, or fragment")
	}
	return nil
}
