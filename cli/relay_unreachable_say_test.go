package main

import (
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
	pin := &url.Error{Op: "Post", URL: "https://relay.test:8792/core", Err: errPinMismatch}
	if err := sendTransportError(s, "/core", pin); errors.As(err, &unreachable) {
		t.Fatalf("pin mismatch became relayUnreachableError: %v", err)
	}
}
