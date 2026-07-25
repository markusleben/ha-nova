package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type cloudHTTPFailingReader struct {
	err error
}

func (r cloudHTTPFailingReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestReadCloudResponsePreservesStableCloudError(t *testing.T) {
	for _, code := range []CloudErrorCode{
		CloudErrOutcomeUnknown,
		CloudErrResponseTooLarge,
	} {
		t.Run(string(code), func(t *testing.T) {
			_, err := readCloudResponse(
				cloudHTTPFailingReader{
					err: newCloudError(code, "read Cloud Relay response", nil),
				},
				64,
				"read Cloud health response",
			)
			if !IsCloudErrorCode(err, code) {
				t.Fatalf("error = %v, want %s", err, code)
			}
		})
	}
}

func TestReadCloudResponseKeepsFunctionalBodyFailureOutcomeUnknown(t *testing.T) {
	body := &cloudIngressLimitedBody{
		ReadCloser: io.NopCloser(cloudHTTPFailingReader{
			err: errors.New("ingress_session=must-not-leak"),
		}),
		remaining:        64,
		outcomeSensitive: true,
	}
	_, err := readCloudResponse(body, 64, "read Cloud health response")
	if !IsCloudErrorCode(err, CloudErrOutcomeUnknown) {
		t.Fatalf("error = %v, want %s", err, CloudErrOutcomeUnknown)
	}
	if strings.Contains(err.Error(), "must-not-leak") {
		t.Fatalf("error leaked transport detail: %v", err)
	}
}

func TestReadCloudResponseMapsUnclassifiedReadFailureToNetworkError(t *testing.T) {
	_, err := readCloudResponse(
		cloudHTTPFailingReader{err: errors.New("read failed")},
		64,
		"read response",
	)
	if !IsCloudErrorCode(err, CloudErrNetwork) {
		t.Fatalf("error = %v, want %s", err, CloudErrNetwork)
	}
}
