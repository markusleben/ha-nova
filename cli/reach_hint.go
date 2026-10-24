package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"syscall"
)

// Windows socket errors don't match the syscall.E* names; same meaning.
const (
	wsaENETUNREACH  = 10051
	wsaETIMEDOUT    = 10060
	wsaECONNREFUSED = 10061
	wsaEHOSTUNREACH = 10065
)

// reachProblem says in plain words why the Home Assistant address rawURL
// didn't answer: nothing listens on the port, or this computer can't get to
// the host at all. "" for anything else (a bad name, TLS, a slow answer, an
// HTTP error): only a failed connect counts.
func reachProblem(rawURL string, err error) string {
	u, parseErr := url.Parse(rawURL)
	var op *net.OpError
	if parseErr != nil || u.Hostname() == "" || !errors.As(err, &op) || op.Op != "dial" {
		return ""
	}
	var errno syscall.Errno
	isErrno := errors.As(op, &errno)
	is := func(e syscall.Errno, onWindows syscall.Errno) bool {
		return errors.Is(op, e) || runtime.GOOS == "windows" && isErrno && errno == onWindows
	}
	if is(syscall.ECONNREFUSED, wsaECONNREFUSED) {
		port := u.Port()
		if port == "" {
			port = map[string]string{"https": "443"}[u.Scheme]
			if port == "" {
				port = "80"
			}
		}
		return "Home Assistant's address (" + u.Hostname() + ") answers, but nothing listens on port " + port + ". Is Home Assistant running? Is the port right?"
	}
	noRoute := is(syscall.EHOSTUNREACH, wsaEHOSTUNREACH) || is(syscall.ENETUNREACH, wsaENETUNREACH)
	if !noRoute && !op.Timeout() && !errors.Is(op, context.DeadlineExceeded) && !is(syscall.ETIMEDOUT, wsaETIMEDOUT) {
		return ""
	}
	text := "This computer can't reach Home Assistant at " + u.Hostname() + ". Is Home Assistant running, and is this computer on the same network?"
	// macOS blocks apps from the home network until allowed; it shows as "no route".
	if runtime.GOOS == "darwin" && noRoute && homeNetworkHost(u.Hostname()) {
		text += " On a Mac, also check System Settings → Privacy & Security → Local Network for your terminal app (or the AI app that runs HA NOVA)."
	}
	return text
}

// homeNetworkHost: a private or link-local IP, a .local name, or a name
// without a dot (homeassistant).
func homeNetworkHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return strings.HasSuffix(strings.ToLower(host), ".local") || !strings.Contains(host, ".")
}

func probeHTTP(url string) error {
	return probeHTTPWithClient(httpClient, url)
}

func probeHTTPWithClient(client *http.Client, url string) error {
	req, err := http.NewRequest("GET", strings.TrimRight(url, "/"), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
