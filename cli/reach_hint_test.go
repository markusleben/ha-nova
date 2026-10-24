package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// dialErr is what net/http returns when the dial fails: never a real host.
func dialErr(cause error) error {
	return &url.Error{Op: "Get", URL: "http://192.168.1.20:8123", Err: &net.OpError{
		Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: cause},
	}}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestReachProblemSaysWhyHomeAssistantDoesNotAnswer(t *testing.T) {
	mac := "System Settings → Privacy & Security → Local Network"
	cases := []struct {
		name, url string
		err       error
		want      string // "" = no plain hint
	}{
		{"no route", "http://192.168.1.20:8123", dialErr(syscall.EHOSTUNREACH), "This computer can't reach Home Assistant at 192.168.1.20. Is Home Assistant running?"},
		{"no network", "http://192.168.1.20:8123", dialErr(syscall.ENETUNREACH), "can't reach Home Assistant at 192.168.1.20"},
		{"connect timeout", "http://homeassistant.local:8123", &url.Error{Op: "Get", URL: "x", Err: &net.OpError{Op: "dial", Err: timeoutErr{}}}, "can't reach Home Assistant at homeassistant.local"},
		{"deadline", "http://192.168.1.20:8123", &url.Error{Op: "Get", URL: "x", Err: context.DeadlineExceeded}, "can't reach Home Assistant"},
		{"refused", "http://192.168.1.20:8123", dialErr(syscall.ECONNREFUSED), "Home Assistant's address (192.168.1.20) answers, but nothing listens on port 8123. Is Home Assistant running? Is the port right?"},
		{"refused, default https port", "https://ha.example:443", dialErr(syscall.ECONNREFUSED), "nothing listens on port 443"},
		{"refused, no port", "https://ha.example", dialErr(syscall.ECONNREFUSED), "nothing listens on port 443"},
		{"bad name is something else", "http://nope.invalid:8123", &url.Error{Op: "Get", URL: "x", Err: &net.DNSError{Err: "no such host", Name: "nope.invalid"}}, ""},
		{"an HTTP error is something else", "http://192.168.1.20:8123", errors.New("HTTP 502"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reachProblem(tc.url, tc.err)
			if tc.want == "" {
				if got != "" {
					t.Fatalf("want no hint, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want it to contain %q", got, tc.want)
			}
			// The Mac line belongs to a computer that can't get there, only on a Mac.
			wantMac := runtime.GOOS == "darwin" && !strings.Contains(tc.name, "refused")
			if strings.Contains(got, mac) != wantMac {
				t.Fatalf("Mac line shown = %v, want %v: %q", !wantMac, wantMac, got)
			}
		})
	}
}

// doctor: the plain hint replaces the raw dial error, from a fake dialer.
func TestDoctorSaysPlainlyWhenHomeAssistantCantBeReached(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	httpClient = &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.EHOSTUNREACH}}
	}}}
	originalHealth, originalWSPing := fetchRelayHealthForReadiness, probeRelayWSPingForReadiness
	t.Cleanup(func() { fetchRelayHealthForReadiness, probeRelayWSPingForReadiness = originalHealth, originalWSPing })
	fetchRelayHealthForReadiness = func(string, string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.1.12"}`), nil
	}
	exit, output := captureCommandOutput(t, func() int { return runDoctor(paths, nil) })
	u, _ := url.Parse(cfg.HAURL)
	if exit == 0 || !strings.Contains(output, "This computer can't reach Home Assistant at "+u.Hostname()+". Is Home Assistant running?") {
		t.Fatalf("doctor output lacks the plain reach hint:\n%s", output)
	}
	if strings.Contains(output, "no route to host") {
		t.Fatalf("doctor shows the raw dial error:\n%s", output)
	}
}
