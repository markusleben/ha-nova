package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// MAR-242: setup deletes a cleanup copy on this proof, so only Home
// Assistant's 400 invalid_grant counts, never a failing server's.
func TestRevocationProofNeedsHomeAssistantsDefinitiveAnswer(t *testing.T) {
	for status, proven := range map[int]bool{http.StatusBadRequest: true, http.StatusServiceUnavailable: false, http.StatusInternalServerError: false} {
		client, err := NewHAOAuthClient("https://unit.ui.nabu.casa", &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if strings.HasSuffix(request.URL.Path, "/revoke") {
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"invalid_grant"}`))}, nil
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		err = revokeAndVerifyCloudAuthorizationWithClient(context.Background(), client, "refresh-token", "http://127.0.0.1:43123/ha-nova")
		if (err == nil) != proven {
			t.Fatalf("HTTP %d invalid_grant: err %v, proven %v", status, err, proven)
		}
	}
}
