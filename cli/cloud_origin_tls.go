package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"time"
)

const cloudOriginProofTimeout = 15 * time.Second

var verifyCloudOriginForOAuth = verifyCloudOriginTLS

// verifyCloudOriginTLS proves the browser destination before OAuth starts.
// The normal TLS handshake validates the user-entered host against public
// roots. ValidateCertificate additionally requires the same peer certificate
// to cover the canonical *.ui.nabu.casa name learned from DNS. A poisoned
// custom-domain CNAME can therefore not redirect sign-in to a server that owns
// only the custom hostname.
func verifyCloudOriginTLS(ctx context.Context, origin CloudOrigin) error {
	if origin.InputHost == "" || origin.CanonicalHost == "" {
		return newCloudError(
			CloudErrInvalidInput,
			"prove Home Assistant Cloud origin",
			nil,
		)
	}
	proofCtx, cancel := context.WithTimeout(ctx, cloudOriginProofTimeout)
	defer cancel()
	dialer := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: cloudOriginProofTimeout},
		Config: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: origin.InputHost,
		},
	}
	connection, err := dialer.DialContext(
		proofCtx,
		"tcp",
		net.JoinHostPort(origin.InputHost, "443"),
	)
	if err != nil {
		var networkError net.Error
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) ||
			(errors.As(err, &networkError) && networkError.Timeout()) {
			return newCloudError(
				CloudErrTimeout,
				"prove Home Assistant Cloud origin",
				err,
			)
		}
		var certificateError x509.CertificateInvalidError
		var hostnameError x509.HostnameError
		var unknownAuthority x509.UnknownAuthorityError
		var verificationError *tls.CertificateVerificationError
		if errors.As(err, &certificateError) ||
			errors.As(err, &hostnameError) ||
			errors.As(err, &unknownAuthority) ||
			errors.As(err, &verificationError) {
			return newCloudError(
				CloudErrIdentityMismatch,
				"prove Home Assistant Cloud origin",
				err,
			)
		}
		return newCloudError(
			CloudErrNetwork,
			"prove Home Assistant Cloud origin",
			err,
		)
	}
	defer connection.Close()
	tlsConnection, ok := connection.(*tls.Conn)
	if !ok {
		return newCloudError(
			CloudErrIdentityMismatch,
			"prove Home Assistant Cloud origin",
			nil,
		)
	}
	state := tlsConnection.ConnectionState()
	if len(state.VerifiedChains) == 0 || len(state.PeerCertificates) == 0 {
		return newCloudError(
			CloudErrIdentityMismatch,
			"prove Home Assistant Cloud origin",
			nil,
		)
	}
	return origin.ValidateCertificate(state.PeerCertificates[0])
}
