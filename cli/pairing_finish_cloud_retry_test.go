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
				roundTripFunc(func(*http.Request) (*http.Response, error) {
					attempts++
					if attempts == 1 {
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
		roundTripFunc(func(*http.Request) (*http.Response, error) {
			attempts++
			return nil, errors.New("response remains lost")
		}),
	)
	var finish map[string]any
	if err := cloudPairingFinishCall(
		context.Background(),
		client,
		map[string]any{"handshake_id": "handshake"},
		&finish,
	); err == nil {
		t.Fatal("persistent transport loss unexpectedly succeeded")
	}
	if attempts != pairingFinishAttempts {
		t.Fatalf("attempts=%d, want=%d", attempts, pairingFinishAttempts)
	}
}
