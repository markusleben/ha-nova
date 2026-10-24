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
		mac       bool   // the Mac line, on a Mac
	}{
		{"no route", "http://192.168.1.20:8123", dialErr(syscall.EHOSTUNREACH), "This computer can't reach Home Assistant at 192.168.1.20. Is Home Assistant running, and is this computer on the same network?", true},
		{"no network", "http://homeassistant:8123", dialErr(syscall.ENETUNREACH), "can't reach Home Assistant at homeassistant.", true},
		{"no route to a public name", "https://ha.example.com", dialErr(syscall.EHOSTUNREACH), "can't reach Home Assistant at ha.example.com.", false},
		{"connect timeout", "http://homeassistant.local:8123", &url.Error{Op: "Get", URL: "x", Err: &net.OpError{Op: "dial", Err: timeoutErr{}}}, "can't reach Home Assistant at homeassistant.local.", false},
		{"connect deadline", "http://192.168.1.20:8123", &url.Error{Op: "Get", URL: "x", Err: &net.OpError{Op: "dial", Err: context.DeadlineExceeded}}, "can't reach Home Assistant at 192.168.1.20.", false},
		{"refused", "http://192.168.1.20:8123", dialErr(syscall.ECONNREFUSED), "Home Assistant's address (192.168.1.20) answers, but nothing listens on port 8123. Is Home Assistant running? Is the port right?", false},
		{"refused, default https port", "https://ha.example:443", dialErr(syscall.ECONNREFUSED), "nothing listens on port 443", false},
		{"refused, no port", "https://ha.example", dialErr(syscall.ECONNREFUSED), "nothing listens on port 443", false},
		{"a slow answer is something else", "http://192.168.1.20:8123", &url.Error{Op: "Get", URL: "x", Err: context.DeadlineExceeded}, "", false},
		{"a read timeout is something else", "http://192.168.1.20:8123", &url.Error{Op: "Get", URL: "x", Err: &net.OpError{Op: "read", Err: timeoutErr{}}}, "", false},
		{"bad name is something else", "http://nope.invalid:8123", &url.Error{Op: "Get", URL: "x", Err: &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "nope.invalid"}}}, "", false},
		{"an HTTP error is something else", "http://192.168.1.20:8123", errors.New("HTTP 502"), "", false},
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
			if wantMac := tc.mac && runtime.GOOS == "darwin"; strings.Contains(got, mac) != wantMac {
				t.Fatalf("Mac line shown = %v, want %v: %q", !wantMac, wantMac, got)
			}
		})
	}
}

// doctor: the plain hint replaces the raw dial error, from a fake dialer,
// and the NOVA App line after it doesn't repeat it.
func TestDoctorSaysPlainlyWhenHomeAssistantCantBeReached(t *testing.T) {
	paths, cfg := doctorTestSetup(t)
	original := httpClient
	t.Cleanup(func() { httpClient = original })
	httpClient = &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: syscall.EHOSTUNREACH}}
	}}}
	originalHealth, originalWSPing := fetchRelayHealthForReadiness, probeRelayWSPingForReadiness
	t.Cleanup(func() { fetchRelayHealthForReadiness, probeRelayWSPingForReadiness = originalHealth, originalWSPing })
	fetchRelayHealthForReadiness = func(string, string) ([]byte, error) { return nil, dialErr(syscall.EHOSTUNREACH) }
	exit, output := captureCommandOutput(t, func() int { return runDoctor(paths, nil) })
	u, _ := url.Parse(cfg.HAURL)
	if exit == 0 || !strings.Contains(output, "This computer can't reach Home Assistant at "+u.Hostname()+". Is Home Assistant running, and") {
		t.Fatalf("doctor output lacks the plain reach hint:\n%s", output)
	}
	if !strings.Contains(output, "HA NOVA can't reach the NOVA App either.") || strings.Contains(output, "no route to host") {
		t.Fatalf("doctor shows the raw dial error:\n%s", output)
	}
}
