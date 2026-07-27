package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	cloudReleaseStressRequestCount = 10_000
	cloudReleaseStressRequestLimit = 20 * time.Second
	cloudReleaseStressOverallLimit = time.Hour
)

type cloudReleaseStressOptions struct {
	server    string
	serverSet bool
}

type cloudReleaseStressFailure struct {
	request int
	cause   error
}

func (failure *cloudReleaseStressFailure) Error() string {
	return fmt.Sprintf(
		"request %d/%d failed: %v",
		failure.request,
		cloudReleaseStressRequestCount,
		failure.cause,
	)
}

func (failure *cloudReleaseStressFailure) Unwrap() error {
	return failure.cause
}

func parseCloudReleaseStressFlags(
	args []string,
) (cloudReleaseStressOptions, error) {
	var options cloudReleaseStressOptions
	fs := flag.NewFlagSet("internal-cloud-stress", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&options.server, "server", "", "server profile")
	if err := fs.Parse(args); err != nil {
		if helpRequested(
			err,
			fs,
			"ha-nova internal-cloud-stress [--server <name>]",
		) {
			return options, errHelpShown
		}
		return options, err
	}
	fs.Visit(func(current *flag.Flag) {
		if current.Name == "server" {
			options.serverSet = true
		}
	})
	if fs.NArg() != 0 {
		return options, errors.New(
			"internal-cloud-stress does not accept positional arguments",
		)
	}
	if options.serverSet {
		if strings.TrimSpace(options.server) != options.server ||
			options.server == "" {
			return options, errors.New(
				"--server requires a non-empty profile name without surrounding whitespace",
			)
		}
		if err := validateServerProfileName(options.server); err != nil {
			return options, err
		}
	}
	return options, nil
}

func runInternalCloudReleaseStress(
	paths runtimePaths,
	args []string,
) int {
	options, err := parseCloudReleaseStressFlags(args)
	if err != nil {
		if errors.Is(err, errHelpShown) {
			return 0
		}
		printErr("%s", err)
		return 1
	}
	if options.serverSet {
		setServerSelectionOverride(options.server)
	}
	cfg, err := loadConfig(paths)
	if err != nil {
		printErr("%s", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		cloudReleaseStressOverallLimit,
	)
	defer cancel()
	selection, err := selectRelayTransport(
		ctx,
		cfg,
		relayViaCloud,
		true,
	)
	if err != nil {
		printErr("%s", relayTransportErrorMessage(err))
		return 1
	}
	if err := validateCloudReleaseStressSelection(selection); err != nil {
		printErr("%s", relayTransportErrorMessage(err))
		return 1
	}
	if err := runCloudReleaseStress(ctx, cfg, selection); err != nil {
		var failure *cloudReleaseStressFailure
		if errors.As(err, &failure) {
			printErr(
				"Cloud stress failed at request %d/%d: %s",
				failure.request,
				cloudReleaseStressRequestCount,
				relayTransportErrorMessage(failure.cause),
			)
		} else {
			printErr("Cloud stress failed: %s", relayTransportErrorMessage(err))
		}
		return 1
	}
	fmt.Fprintf(
		os.Stdout,
		"Cloud stress passed: %d/%d read-only requests through one process-local Ingress session.\n",
		cloudReleaseStressRequestCount,
		cloudReleaseStressRequestCount,
	)
	return 0
}

func validateCloudReleaseStressSelection(
	selection relayTransportSelection,
) error {
	transport, ok := selection.Client.Transport.(*cloudRelayRoundTripper)
	if selection.Via != relayViaCloud ||
		!ok ||
		transport.ingress == nil ||
		transport.credential != selection.Credential {
		return newCloudError(
			CloudErrInvalidInput,
			"validate single-session Cloud stress transport",
			nil,
		)
	}
	return nil
}

func runCloudReleaseStress(
	ctx context.Context,
	cfg runtimeConfig,
	selection relayTransportSelection,
) error {
	client := cloudNoRedirectHTTPClient(
		selection.Client,
		cloudReleaseStressRequestLimit,
	)
	client.Timeout = cloudReleaseStressRequestLimit
	endpoint := strings.TrimRight(selection.BaseURL, "/") + "/health"

	for requestNumber := 1; requestNumber <= cloudReleaseStressRequestCount; requestNumber++ {
		if err := runCloudReleaseStressRequest(
			ctx,
			client,
			endpoint,
			selection.Credential,
			cfg.RelayInstanceID,
		); err != nil {
			return &cloudReleaseStressFailure{
				request: requestNumber,
				cause:   err,
			}
		}
	}
	return nil
}

func runCloudReleaseStressRequest(
	parent context.Context,
	client *http.Client,
	endpoint string,
	credential string,
	expectedRelayInstanceID string,
) error {
	ctx, cancel := context.WithTimeout(
		parent,
		cloudReleaseStressRequestLimit,
	)
	defer cancel()
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint,
		nil,
	)
	if err != nil {
		return newCloudError(
			CloudErrInvalidInput,
			"build Cloud stress request",
			err,
		)
	}
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return cloudRequestError("send Cloud stress request", err)
	}
	if response == nil || response.Body == nil {
		return newCloudError(
			CloudErrHAProtocol,
			"validate Cloud stress response",
			nil,
		)
	}
	body, readErr := readCloudResponse(
		response.Body,
		cloudLocalDiscoveryMaxBytes,
		"read Cloud stress response",
	)
	closeErr := response.Body.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return cloudRequestError("close Cloud stress response", closeErr)
	}
	if isHTTPRedirect(response.StatusCode) {
		return newCloudHTTPError(
			CloudErrRedirectRejected,
			"validate Cloud stress response",
			response.StatusCode,
			false,
		)
	}
	if response.StatusCode != http.StatusOK {
		return newCloudHTTPError(
			CloudErrHAProtocol,
			"validate Cloud stress response",
			response.StatusCode,
			false,
		)
	}
	body, err = normalizeUTF8Bytes(body, "Cloud stress response")
	if err != nil {
		return newCloudError(
			CloudErrHAProtocol,
			"validate Cloud stress response encoding",
			err,
		)
	}
	return validateCloudRelayHealthIdentity(
		body,
		expectedRelayInstanceID,
	)
}
