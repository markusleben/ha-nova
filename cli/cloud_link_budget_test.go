package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Home Assistant Link route budgets and its one bounded retry
// (docs/work/2026-11-03-link-route-timeouts-spec.md).

func shortLinkRetryPause(t *testing.T) {
	t.Helper()
	old := linkRetryPause
	linkRetryPause = time.Millisecond
	t.Cleanup(func() { linkRetryPause = old })
}

func relayAnswer(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{relayVersionHeader: []string{"1.2.3"}, "Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// A Link connection that never opens fails at its connect budget, not after
// the transport's default (10 s TLS), and is recorded as never connected.
func TestLinkConnectionFailsAtItsConnectBudget(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { // accepts and never answers the TLS handshake: a stalled Link front
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			defer connection.Close()
		}
	}()
	client := &http.Client{Transport: newLinkHTTPTransport()}
	ctx, cancel := context.WithTimeout(withRelayConnectBudget(context.Background(), 200*time.Millisecond), 3*time.Second)
	defer cancel()
	ctx, trace := traceLinkConnection(ctx)
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+listener.Addr().String()+"/manifest.json", nil)
	started := time.Now()
	_, err = client.Do(request)
	if err == nil {
		t.Fatal("a stalled TLS handshake succeeded")
	}
	if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
		t.Fatalf("stalled Link connection gave up after %s, want about the 200ms connect budget", elapsed)
	}
	if !trace.neverConnected() {
		t.Fatal("a connection that never opened was not recorded as never connected")
	}
}

// The R2 proof on a real socket: a refused connection is never connected; a
// request that reached the server is not, even when its answer is lost.
func TestLinkConnectionTraceProvesNothingWasWritten(t *testing.T) {
	refused, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refusedAddress := refused.Addr().String()
	refused.Close()

	answered, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer answered.Close()
	var received atomic.Int32
	go func() { // reads the request, then drops the connection: the answer is lost
		for {
			connection, err := answered.Accept()
			if err != nil {
				return
			}
			buffer := make([]byte, 4096)
			if count, _ := connection.Read(buffer); count > 0 {
				received.Add(1)
			}
			connection.Close()
		}
	}()

	client := &http.Client{Transport: newLinkHTTPTransport()}
	for name, testCase := range map[string]struct {
		address        string
		neverConnected bool
	}{
		"refused":     {address: refusedAddress, neverConnected: true},
		"answer lost": {address: answered.Addr().String(), neverConnected: false},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, trace := traceLinkConnection(context.Background())
			request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+testCase.address+"/core",
				strings.NewReader(`{"method":"POST","path":"/api/services/lock/unlock"}`))
			if _, err := client.Do(request); err == nil {
				t.Fatal("request succeeded")
			}
			if trace.neverConnected() != testCase.neverConnected {
				t.Fatalf("neverConnected = %v, want %v", trace.neverConnected(), testCase.neverConnected)
			}
		})
	}
	if received.Load() != 1 {
		t.Fatalf("server received %d requests, want exactly 1", received.Load())
	}
}

// R2 and R3 in the Ingress client; a write that may have reached the Relay is
// never sent again.
func TestCloudIngressRetriesOnlyWhatCannotHaveReachedTheRelay(t *testing.T) {
	shortLinkRetryPause(t)
	deviceCredential := validCredential(23)
	write := []byte(`{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":"lock.front"}}`)
	connected := func(request *http.Request) {
		if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.GotConn != nil {
			trace.GotConn(httptrace.GotConnInfo{})
		}
	}
	for name, testCase := range map[string]struct {
		endpoint  CloudIngressEndpoint
		body      []byte
		first     func(*http.Request) (*http.Response, error)
		wantCalls int32
		wantErr   bool
	}{
		"write whose connection never opened is sent once": {
			endpoint: CloudEndpointCore, body: write,
			first: func(request *http.Request) (*http.Response, error) {
				err := errors.New("dial tcp: i/o timeout")
				noteLinkSetupFailure(request.Context(), err)
				return nil, err
			},
			wantCalls: 2,
		},
		"write sent, answer lost: never again": {
			endpoint: CloudEndpointCore, body: write,
			first: func(request *http.Request) (*http.Response, error) {
				connected(request)
				return nil, errors.New("read: connection reset by peer")
			},
			wantCalls: 1, wantErr: true,
		},
		"write sent, gateway failed: never again": {
			endpoint: CloudEndpointCore, body: write,
			first: func(request *http.Request) (*http.Response, error) {
				connected(request)
				return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			},
			wantCalls: 1, wantErr: true,
		},
		"health GET without an answer goes once more": {
			endpoint: CloudEndpointHealth,
			first: func(request *http.Request) (*http.Response, error) {
				connected(request)
				return nil, errors.New("read: connection reset by peer")
			},
			wantCalls: 2,
		},
		"health GET that Link could not pass on goes once more": {
			endpoint: CloudEndpointHealth,
			first: func(request *http.Request) (*http.Response, error) {
				connected(request)
				return &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			},
			wantCalls: 2,
		},
		"health GET answered by the Relay is final": {
			endpoint: CloudEndpointHealth,
			first: func(request *http.Request) (*http.Response, error) {
				connected(request)
				return relayAnswer(http.StatusServiceUnavailable, `{"ok":false}`), nil
			},
			wantCalls: 1, wantErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					return testCase.first(request)
				}
				connected(request)
				return relayAnswer(http.StatusOK, `{"ok":true,"data":{}}`), nil
			}))
			response, err := client.Do(context.Background(), testCase.endpoint, deviceCredential, testCase.body)
			if response != nil {
				response.Body.Close()
			}
			if calls.Load() != testCase.wantCalls || (err != nil) != testCase.wantErr {
				t.Fatalf("calls = %d (want %d), err = %v (want error: %v)", calls.Load(), testCase.wantCalls, err, testCase.wantErr)
			}
		})
	}
}

// R4: a session read over Link that got no answer goes once more; a write,
// a redirect, or anything over the local route never does.
func TestRelaySessionRetriesOnlyLinkReads(t *testing.T) {
	shortLinkRetryPause(t)
	lost := newCloudError(CloudErrOutcomeUnknown, "dispatch Cloud Relay request", errors.New("connection reset"))
	redirected := newCloudHTTPError(CloudErrOutcomeUnknown, "dispatch Cloud Relay request", http.StatusFound, false)
	for name, testCase := range map[string]struct {
		via       relayVia
		firstErr  error
		write     bool
		wantCalls int32
		wantErr   bool
	}{
		"Link read without an answer":      {via: relayViaCloud, firstErr: lost, wantCalls: 2},
		"Link write without an answer":     {via: relayViaCloud, firstErr: lost, write: true, wantCalls: 1, wantErr: true},
		"Link read Link answered 502":      {via: relayViaCloud, firstErr: &CloudError{Code: CloudErrOutcomeUnknown, Op: "dispatch Cloud Relay request", StatusCode: http.StatusBadGateway, gatewayFailed: true}, wantCalls: 2},
		"Link read the Relay answered 500": {via: relayViaCloud, firstErr: newCloudHTTPError(CloudErrOutcomeUnknown, "dispatch Cloud Relay request", http.StatusInternalServerError, false), wantCalls: 1, wantErr: true},
		"Link read redirected":             {via: relayViaCloud, firstErr: redirected, wantCalls: 1, wantErr: true},
		"local read without an answer":     {via: relayViaLocal, firstErr: syscall.ECONNRESET, wantCalls: 1, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s := &relaySession{
				transport: relayTransportSelection{
					BaseURL: cloudRelayVirtualBaseURL,
					Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
						if calls.Add(1) == 1 {
							return nil, testCase.firstErr
						}
						return relayAnswer(http.StatusOK, `{"ok":true,"data":{"status":200,"body":{"state":"on"}}}`), nil
					})},
					Credential: validCredential(41),
					Via:        testCase.via,
				},
				ctx: ctx, cancel: cancel, slots: make(chan struct{}, relaySessionMaxConcurrent), memo: map[string]*relayMemo{},
			}
			var err error
			if testCase.write {
				_, err = s.postService("light.turn_on", map[string]any{"entity_id": []string{"light.hall"}}, false)
			} else {
				_, _, err = s.coreGET("/api/states/light.hall")
			}
			if calls.Load() != testCase.wantCalls || (err != nil) != testCase.wantErr {
				t.Fatalf("calls = %d (want %d), err = %v (want error: %v)", calls.Load(), testCase.wantCalls, err, testCase.wantErr)
			}
		})
	}
}
