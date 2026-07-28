package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"testing"
)

type pairingFinishReadErrorBody struct{}

func (pairingFinishReadErrorBody) Read([]byte) (int, error) {
	return 0, errors.New("response lost after server commit")
}

func (pairingFinishReadErrorBody) Close() error {
	return nil
}

func pairingFinishTestResponse(
	status int,
	body io.ReadCloser,
	relayProof bool,
) *http.Response {
	header := make(http.Header)
	if relayProof {
		header.Set(relayVersionHeader, "1.2.3")
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       body,
	}
}

func markPairingFinishRequestWritten(request *http.Request) {
	trace := httptrace.ContextClientTrace(request.Context())
	if trace != nil && trace.WroteRequest != nil {
		trace.WroteRequest(httptrace.WroteRequestInfo{})
	}
}

func markPairingFinishRequestWriteFailed(
	request *http.Request,
	err error,
) {
	trace := httptrace.ContextClientTrace(request.Context())
	if trace != nil && trace.WroteRequest != nil {
		trace.WroteRequest(httptrace.WroteRequestInfo{Err: err})
	}
}

func TestPairFinishV1ReplaysExactCommittedRequestAfterLostResponse(t *testing.T) {
	attempts := 0
	var committedRequest []byte
	client := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			if attempts == 1 {
				// Model the relay's atomic commit: it stores both the pending device
				// and encrypted finish response, but the transport loses the response.
				committedRequest = append([]byte(nil), body...)
				markPairingFinishRequestWritten(request)
				return nil, errors.New("response lost after server commit")
			}
			if !bytes.Equal(body, committedRequest) {
				t.Fatalf(
					"finish retry changed committed payload:\nfirst:  %s\nsecond: %s",
					committedRequest,
					body,
				)
			}
			return pairingFinishTestResponse(
				http.StatusOK,
				io.NopCloser(strings.NewReader(
					`{"ok":true,"data":{"response":"persisted-v1-response"}}`,
				)),
				false,
			), nil
		}),
	}

	var finish struct {
		Response string `json:"response"`
	}
	err := pairFinishPostJSON(
		client,
		"http://relay.test/pair/v1/finish",
		map[string]any{
			"handshake_id": "handshake",
			"ke3":          "ke3",
			"metadata":     "ciphertext",
		},
		&finish,
	)
	if err != nil {
		t.Fatalf("pairFinishPostJSON: %v", err)
	}
	if attempts != 2 || finish.Response != "persisted-v1-response" {
		t.Fatalf("attempts=%d finish=%+v", attempts, finish)
	}
}

func TestPairFinishV1RetriesOnlyAmbiguousOutcomes(t *testing.T) {
	tests := []struct {
		name         string
		first        func() (*http.Response, error)
		wantAttempts int
		wantSuccess  bool
	}{
		{
			name: "response body read failure",
			first: func() (*http.Response, error) {
				return pairingFinishTestResponse(
					http.StatusOK,
					pairingFinishReadErrorBody{},
					false,
				), nil
			},
			wantAttempts: 2,
			wantSuccess:  true,
		},
		{
			name: "server 503",
			first: func() (*http.Response, error) {
				return pairingFinishTestResponse(
					http.StatusServiceUnavailable,
					io.NopCloser(strings.NewReader("")),
					false,
				), nil
			},
			wantAttempts: 2,
			wantSuccess:  true,
		},
		{
			name: "definitive 400",
			first: func() (*http.Response, error) {
				return pairingFinishTestResponse(
					http.StatusBadRequest,
					io.NopCloser(strings.NewReader(
						`{"ok":false,"error":{"code":"PAIRING_INACTIVE"}}`,
					)),
					false,
				), nil
			},
			wantAttempts: 1,
			wantSuccess:  false,
		},
		{
			name: "definitive 400 with unreadable body",
			first: func() (*http.Response, error) {
				return pairingFinishTestResponse(
					http.StatusBadRequest,
					pairingFinishReadErrorBody{},
					false,
				), nil
			},
			wantAttempts: 1,
			wantSuccess:  false,
		},
		{
			name: "definitive 429",
			first: func() (*http.Response, error) {
				return pairingFinishTestResponse(
					http.StatusTooManyRequests,
					io.NopCloser(strings.NewReader("")),
					false,
				), nil
			},
			wantAttempts: 1,
			wantSuccess:  false,
		},
		{
			name: "malformed success envelope",
			first: func() (*http.Response, error) {
				return pairingFinishTestResponse(
					http.StatusOK,
					io.NopCloser(strings.NewReader(`{"ok":true,"data":`)),
					false,
				), nil
			},
			wantAttempts: 1,
			wantSuccess:  false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			client := &http.Client{
				Transport: roundTripFunc(func(
					*http.Request,
				) (*http.Response, error) {
					attempts++
					if attempts == 1 {
						return test.first()
					}
					return pairingFinishTestResponse(
						http.StatusOK,
						io.NopCloser(strings.NewReader(
							`{"ok":true,"data":{"response":"replayed"}}`,
						)),
						false,
					), nil
				}),
			}
			var finish struct {
				Response string `json:"response"`
			}
			err := pairFinishPostJSON(
				client,
				"http://relay.test/pair/v1/finish",
				map[string]any{
					"handshake_id": "handshake",
					"ke3":          "ke3",
					"metadata":     "ciphertext",
				},
				&finish,
			)
			if (err == nil) != test.wantSuccess {
				t.Fatalf("error=%v, wantSuccess=%v", err, test.wantSuccess)
			}
			if attempts != test.wantAttempts {
				t.Fatalf("attempts=%d, want=%d", attempts, test.wantAttempts)
			}
		})
	}
}

func TestPairFinishV1AmbiguousRetriesAreBounded(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		attempts++
		markPairingFinishRequestWritten(request)
		return nil, errors.New("response remains lost")
	})}
	var finish map[string]any
	err := pairFinishPostJSON(
		client,
		"http://relay.test/pair/v1/finish",
		map[string]any{"handshake_id": "handshake"},
		&finish,
	)
	if !errors.Is(err, errPairingOutcomeUnknown) {
		t.Fatalf("persistent transport loss err=%v, want outcome unknown", err)
	}
	if attempts != pairingFinishAttempts {
		t.Fatalf("attempts=%d, want=%d", attempts, pairingFinishAttempts)
	}
}

func TestPairFinishV1PreDispatchFailureIsDefinitive(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(
		*http.Request,
	) (*http.Response, error) {
		attempts++
		return nil, errors.New("dial tcp: connection refused")
	})}
	var finish map[string]any
	err := pairFinishPostJSON(
		client,
		"http://relay.test/pair/v1/finish",
		map[string]any{"handshake_id": "handshake"},
		&finish,
	)
	if err == nil || errors.Is(err, errPairingOutcomeUnknown) {
		t.Fatalf("pre-dispatch finish err=%v, want definitive failure", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d, want=1", attempts)
	}
}

func TestPairFinishV1FailedWriteIsDefinitive(t *testing.T) {
	attempts := 0
	writeErr := errors.New("write tcp: broken pipe")
	client := &http.Client{Transport: roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		attempts++
		markPairingFinishRequestWriteFailed(request, writeErr)
		return nil, writeErr
	})}
	var finish map[string]any
	err := pairFinishPostJSON(
		client,
		"http://relay.test/pair/v1/finish",
		map[string]any{"handshake_id": "handshake"},
		&finish,
	)
	if err == nil || errors.Is(err, errPairingOutcomeUnknown) {
		t.Fatalf("failed-write finish err=%v, want definitive failure", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d, want=1", attempts)
	}
}

func TestPairFinishV1DoesNotDowngradeAmbiguousOutcome(t *testing.T) {
	for _, definitive := range []error{
		errPairingInactive,
		errPairingCodeRejected,
		errRelayNotV1,
		&relayPairingRateLimitError{retryAfterSeconds: 1},
	} {
		attempts := 0
		err := retryPairingFinish(func() (bool, error) {
			attempts++
			if attempts == 1 {
				return true, errors.New("response lost after server commit")
			}
			return false, definitive
		})
		if !errors.Is(err, errPairingOutcomeUnknown) ||
			errors.Is(err, definitive) {
			t.Fatalf("mixed finish outcome err=%v", err)
		}
		if attempts != 2 {
			t.Fatalf("attempts=%d, want=2", attempts)
		}
	}
}
