package main

import (
	"context"
	"errors"
	"net"
	"net/url"
	"runtime"
	"syscall"
)

// Windows socket errors don't match the syscall.E* names; same meaning.
const (
	wsaENETUNREACH  = 10051
	wsaETIMEDOUT    = 10060
	wsaECONNREFUSED = 10061
	wsaEHOSTUNREACH = 10065
)

// reachProblem says in plain words why rawURL didn't answer: nothing
// listens on the port, or this computer can't get to the host at all.
// "" when the error is something else (TLS, HTTP status, a bad name).
func reachProblem(rawURL string, err error) string {
	u, parseErr := url.Parse(rawURL)
	if parseErr != nil || u.Hostname() == "" || err == nil {
		return ""
	}
	var errno syscall.Errno
	isErrno := errors.As(err, &errno)
	onWindows := runtime.GOOS == "windows"
	refused := errors.Is(err, syscall.ECONNREFUSED) || onWindows && isErrno && errno == wsaECONNREFUSED
	if refused {
		port := u.Port()
		if port == "" {
			port = map[string]string{"https": "443"}[u.Scheme]
			if port == "" {
				port = "80"
			}
		}
		return "Home Assistant's address (" + u.Hostname() + ") answers, but nothing listens on port " + port + ". Is Home Assistant running? Is the port right?"
	}
	var netErr net.Error
	unreachable := errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.ETIMEDOUT) || errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &netErr) && netErr.Timeout() ||
		onWindows && isErrno && (errno == wsaEHOSTUNREACH || errno == wsaENETUNREACH || errno == wsaETIMEDOUT)
	if !unreachable {
		return ""
	}
	text := "This computer can't reach Home Assistant at " + u.Hostname() + ". Is Home Assistant running?"
	if runtime.GOOS == "darwin" {
		text += " On a Mac, also check System Settings → Privacy & Security → Local Network for your terminal app."
	}
	return text
}
