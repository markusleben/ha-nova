package main

import (
	"bufio"
	"context"
	"io"
)

func promptValidatedCloudRemoteOrigin(
	ctx context.Context,
	reader *bufio.Reader,
	out io.Writer,
	defaultValue string,
	resolver CloudCNAMEResolver,
) (CloudOrigin, error) {
	for {
		raw, err := promptWizardLineFromReader(
			reader,
			out,
			"Home Assistant Cloud URL (or type 'back'/'exit')",
			defaultValue,
		)
		if err != nil {
			return CloudOrigin{}, err
		}
		if _, err := parseStrictCloudOrigin(raw); err != nil {
			renderSetupErrorLine(
				out,
				"Enter the complete HTTPS remote URL shown by Home Assistant.",
			)
			continue
		}
		origin, err := ResolveCanonicalNabuOrigin(
			ctx,
			raw,
			resolver,
		)
		if err == nil {
			return origin, nil
		}
		if IsCloudErrorCode(err, CloudErrInvalidInput) {
			renderSetupErrorLine(
				out,
				"This HTTPS URL is not a verified Home Assistant Cloud remote URL. Check the URL shown by Home Assistant and try again.",
			)
			continue
		}
		// DNS transport failures and security-sensitive resolver outcomes are
		// not user-input validation. Stop without retrying or silently changing
		// the origin so the caller can render typed fail-closed recovery.
		return CloudOrigin{}, err
	}
}
