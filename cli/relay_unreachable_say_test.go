package main

import (
	"context"
	"crypto/tls"
	"errors"
	"net/url"
	"testing"
)

// F2: only a Relay that did not answer gets the plain "is the App running"
// say; a TLS identity failure keeps its own wording (Codex review).
func TestSendTransportErrorSeparatesIdentityFromReachability(t *testing.T) {
	s := &relaySession{transport: relayTransportSelection{BaseURL: "https://relay.test:8792"}}
	refused := &url.Error{Op: "Post", URL: "https://relay.test:8792/core", Err: errors.New("connection refused")}
	var unreachable relayUnreachableError
	if err := sendTransportError(s, "/core", refused); !errors.As(err, &unreachable) {
		t.Fatalf("refused: %T %v, want relayUnreachableError", err, err)
	}
	timeout := &url.Error{Op: "Post", URL: "https://relay.test:8792/core", Err: context.DeadlineExceeded}
	if err := sendTransportError(s, "/core", timeout); !errors.As(err, &unreachable) {
		t.Fatalf("timeout: %T %v, want relayUnreachableError", err, err)
	}
	for name, cause := range map[string]error{"pin mismatch": errPinMismatch, "plain HTTP": tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"}} {
		err := sendTransportError(s, "/core", &url.Error{Op: "Post", URL: "https://relay.test:8792/core", Err: cause})
		if errors.As(err, &unreachable) {
			t.Fatalf("%s became relayUnreachableError: %v", name, err)
		}
	}
}
