package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPairFinishCloudV2ReplaysExactCommittedRequestAfterLostResponse(
	t *testing.T,
) {
	attempts := 0
	var committedRequest []byte
	client := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			if !strings.HasSuffix(request.URL.Path, CloudPathPairFinish) {
				t.Fatalf("finish request path = %s", request.URL.Path)
			}
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			if attempts == 1 {
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
					`{"ok":true,"data":{"response":"persisted-v2-response"}}`,
				)),
				true,
			), nil
		}),
	)

	var finish struct {
		Response string `json:"response"`
	}
	err := cloudPairingFinishCall(
		context.Background(),
		client,
		map[string]any{
			"handshake_id": "handshake",
			"ke3":          "ke3",
			"metadata":     "ciphertext",
		},
		&finish,
	)
	if err != nil {
		t.Fatalf("cloudPairingFinishCall: %v", err)
	}
	if attempts != 2 || finish.Response != "persisted-v2-response" {
		t.Fatalf("attempts=%d finish=%+v", attempts, finish)
	}
}

func TestPairFinishCloudV2RetriesOnlyAmbiguousOutcomes(t *testing.T) {
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
					true,
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
					true,
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
					true,
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
					true,
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
					true,
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
					true,
				), nil
			},
			wantAttempts: 1,
			wantSuccess:  false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			client := newProtocolTestCloudIngressClient(
				t,
				roundTripFunc(func(request *http.Request) (*http.Response, error) {
					attempts++
					if attempts == 1 {
						markPairingFinishRequestWritten(request)
						return test.first()
					}
					return pairingFinishTestResponse(
						http.StatusOK,
						io.NopCloser(strings.NewReader(
							`{"ok":true,"data":{"response":"replayed"}}`,
						)),
						true,
					), nil
				}),
			)
			var finish struct {
				Response string `json:"response"`
			}
			err := cloudPairingFinishCall(
				context.Background(),
				client,
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

func TestPairFinishCloudV2AmbiguousRetriesAreBounded(t *testing.T) {
	attempts := 0
	client := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			markPairingFinishRequestWritten(request)
			return nil, errors.New("response remains lost")
		}),
	)
	var finish map[string]any
	err := cloudPairingFinishCall(
		context.Background(),
		client,
		map[string]any{"handshake_id": "handshake"},
		&finish,
	)
	if !IsCloudErrorCode(err, CloudErrOutcomeUnknown) {
		t.Fatalf("persistent transport loss err=%v, want outcome unknown", err)
	}
	if attempts != pairingFinishAttempts {
		t.Fatalf("attempts=%d, want=%d", attempts, pairingFinishAttempts)
	}
	if problem := cloudProblemForError(err); problem.Remediation !=
		cloudRemediationVerifyState {
		t.Fatalf("persistent transport loss problem=%+v", problem)
	}
}

func TestPairFinishCloudV2PreDispatchFailureIsDefinitive(t *testing.T) {
	attempts := 0
	client := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return nil, errors.New("dial tcp: connection refused")
		}),
	)
	var finish map[string]any
	err := cloudPairingFinishCall(
		context.Background(),
		client,
		map[string]any{"handshake_id": "handshake"},
		&finish,
	)
	if err == nil || IsCloudErrorCode(err, CloudErrOutcomeUnknown) {
		t.Fatalf("pre-dispatch finish err=%v, want definitive failure", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d, want=1", attempts)
	}
}

func TestPairFinishCloudV2FailedWriteIsDefinitive(t *testing.T) {
	attempts := 0
	writeErr := errors.New("write tcp: broken pipe")
	client := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			markPairingFinishRequestWriteFailed(request, writeErr)
			return nil, writeErr
		}),
	)
	var finish map[string]any
	err := cloudPairingFinishCall(
		context.Background(),
		client,
		map[string]any{"handshake_id": "handshake"},
		&finish,
	)
	if err == nil || IsCloudErrorCode(err, CloudErrOutcomeUnknown) {
		t.Fatalf("failed-write finish err=%v, want definitive failure", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d, want=1", attempts)
	}
}

func TestPairFinishCloudV2IngressUnavailableIsDefinitive(t *testing.T) {
	for _, status := range []int{
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			attempts := 0
			client := newProtocolTestCloudIngressClient(
				t,
				roundTripFunc(func(request *http.Request) (*http.Response, error) {
					attempts++
					markPairingFinishRequestWritten(request)
					return pairingFinishTestResponse(
						status,
						io.NopCloser(strings.NewReader("")),
						false,
					), nil
				}),
			)
			var finish map[string]any
			err := cloudPairingFinishCall(
				context.Background(),
				client,
				map[string]any{"handshake_id": "handshake"},
				&finish,
			)
			if !IsCloudErrorCode(err, CloudErrIngressUnavailable) {
				t.Fatalf(
					"headerless %d finish err=%v, want ingress unavailable",
					status,
					err,
				)
			}
			if attempts != 1 {
				t.Fatalf("attempts=%d, want=1", attempts)
			}
			if problem := cloudProblemForError(err); problem.Remediation !=
				cloudRemediationRetry {
				t.Fatalf("headerless %d problem=%+v", status, problem)
			}
		})
	}
}

func TestPairFinishCloudV2ContextEndAfterSendIsOutcomeUnknown(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	client := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			markPairingFinishRequestWritten(request)
			cancel()
			return nil, request.Context().Err()
		}),
	)
	var finish map[string]any
	err := cloudPairingFinishCall(
		ctx,
		client,
		map[string]any{"handshake_id": "handshake"},
		&finish,
	)
	if !IsCloudErrorCode(err, CloudErrOutcomeUnknown) {
		t.Fatalf("cancelled post-send finish err=%v, want outcome unknown", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d, want=1", attempts)
	}
	if problem := cloudProblemForError(err); problem.Remediation !=
		cloudRemediationVerifyState {
		t.Fatalf("cancelled post-send finish problem=%+v", problem)
	}
}

func TestPairFinishCloudV2DoesNotDowngradeAmbiguousOutcome(t *testing.T) {
	attempts := 0
	client := newProtocolTestCloudIngressClient(
		t,
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				markPairingFinishRequestWritten(request)
				return nil, errors.New("response lost after server commit")
			}
			return pairingFinishTestResponse(
				http.StatusUnauthorized,
				io.NopCloser(strings.NewReader("")),
				true,
			), nil
		}),
	)
	var finish map[string]any
	err := cloudPairingFinishCall(
		context.Background(),
		client,
		map[string]any{"handshake_id": "handshake"},
		&finish,
	)
	if !IsCloudErrorCode(err, CloudErrOutcomeUnknown) ||
		IsCloudErrorCode(err, CloudErrPairingRejected) {
		t.Fatalf("mixed finish outcome err=%v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d, want=2", attempts)
	}
	if problem := cloudProblemForError(err); problem.Remediation !=
		cloudRemediationVerifyState {
		t.Fatalf("mixed finish outcome problem=%+v", problem)
	}
}
