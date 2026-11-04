package main

import (
	"context"
	"crypto/sha256"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// R4 on the real pinned transport: a local connection dropped before the
// Relay answered goes once more; plain HTTP on the TLS port or a wrong pin is
// dialed once.
func TestLocalSessionReadRetriesOnlyDroppedConnections(t *testing.T) {
	shortLinkRetryPause(t)
	for name, testCase := range map[string]struct {
		drop            int32
		plain, wrongPin bool
		wantConns       int32
		wantErr         bool
	}{
		"dropped once": {drop: 1, wantConns: 2},
		"plain HTTP":   {plain: true, wantConns: 1, wantErr: true},
		"wrong pin":    {wrongPin: true, wantConns: 1, wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				_, _ = writer.Write([]byte(`{"ok":true,"data":{"status":200,"body":{"state":"on"}}}`))
			}))
			listener := &droppingListener{Listener: server.Listener, drop: testCase.drop}
			server.Listener = listener
			pin := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			if testCase.plain {
				server.Start()
			} else {
				server.StartTLS()
				if !testCase.wrongPin {
					sum := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
					pin = pairB64.EncodeToString(sum[:])
				}
			}
			defer server.Close()
			client := spkiPinnedClient(pin)
			defer client.CloseIdleConnections()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s := &relaySession{
				transport: relayTransportSelection{BaseURL: "https://" + listener.Addr().String(), Client: client, Credential: validCredential(41), Via: relayViaLocal},
				ctx:       ctx, cancel: cancel, slots: make(chan struct{}, relaySessionMaxConcurrent), memo: map[string]*relayMemo{},
			}
			_, _, err := s.coreGET("/api/states/light.hall")
			if listener.accepted.Load() != testCase.wantConns || (err != nil) != testCase.wantErr {
				t.Fatalf("connections = %d (want %d), err = %v (want error: %v)", listener.accepted.Load(), testCase.wantConns, err, testCase.wantErr)
			}
		})
	}
}

// droppingListener resets its first drop connections before they are served.
type droppingListener struct {
	net.Listener
	drop     int32
	accepted atomic.Int32
}

func (l *droppingListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil || l.accepted.Add(1) > l.drop {
			return conn, err
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = conn.Close()
	}
}
