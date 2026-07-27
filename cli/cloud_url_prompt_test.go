package main

import (
	"bufio"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCloudOriginPromptRepromptsAfterUntrustedHTTPSOrigin(t *testing.T) {
	resolver := &fakeCloudResolver{canonical: "www.example.com."}
	var output strings.Builder
	origin, err := promptValidatedCloudRemoteOrigin(
		context.Background(),
		bufio.NewReader(strings.NewReader(
			"https://www.example.com\n"+
				productionCloudTestOrigin+"\n",
		)),
		&output,
		"",
		resolver,
	)
	if err != nil {
		t.Fatal(err)
	}
	if origin.CanonicalOrigin != productionCloudTestOrigin {
		t.Fatalf("origin=%+v", origin)
	}
	if !strings.Contains(
		output.String(),
		"not a verified Home Assistant Cloud remote URL",
	) {
		t.Fatalf("missing untrusted-origin guidance:\n%s", output.String())
	}
}

func TestCloudOriginPromptFailsClosedOnResolverTransportError(t *testing.T) {
	resolver := &fakeCloudResolver{err: errors.New("DNS offline")}
	reader := bufio.NewReader(strings.NewReader(
		"https://custom.example.com\n" +
			productionCloudTestOrigin + "\n",
	))
	var output strings.Builder
	_, err := promptValidatedCloudRemoteOrigin(
		context.Background(),
		reader,
		&output,
		"",
		resolver,
	)
	if !IsCloudErrorCode(err, CloudErrNetwork) {
		t.Fatalf("resolver error=%v", err)
	}
	remaining, readErr := reader.ReadString('\n')
	if readErr != nil || strings.TrimSpace(remaining) != productionCloudTestOrigin {
		t.Fatalf(
			"transport failure consumed retry input=%q err=%v",
			remaining,
			readErr,
		)
	}
	if strings.Contains(
		output.String(),
		"not a verified Home Assistant Cloud remote URL",
	) {
		t.Fatalf("transport failure was rendered as input retry:\n%s", output.String())
	}
}
