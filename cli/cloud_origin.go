package main

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const canonicalNabuSuffix = ".ui.nabu.casa"
const cloudOriginResolutionTimeout = 15 * time.Second

var canonicalNabuHostPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.ui\.nabu\.casa$`)
var dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type CloudCNAMEResolver interface {
	LookupCNAME(context.Context, string) (string, error)
}

type NetCloudCNAMEResolver struct {
	Resolver *net.Resolver
}

func (r NetCloudCNAMEResolver) LookupCNAME(ctx context.Context, host string) (string, error) {
	resolver := r.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return resolver.LookupCNAME(ctx, host)
}

type CloudOrigin struct {
	InputOrigin     string
	InputHost       string
	CanonicalOrigin string
	CanonicalHost   string
	CustomDomain    bool
}

func ParseCanonicalNabuOrigin(raw string) (*url.URL, error) {
	parsed, err := parseStrictCloudOrigin(raw)
	if err != nil || !canonicalNabuHostPattern.MatchString(parsed.Hostname()) {
		return nil, newCloudError(CloudErrInvalidInput, "validate canonical Nabu Casa origin", err)
	}
	return parsed, nil
}

func ResolveCanonicalNabuOrigin(ctx context.Context, raw string, resolver CloudCNAMEResolver) (CloudOrigin, error) {
	input, err := parseStrictCloudOrigin(raw)
	if err != nil {
		return CloudOrigin{}, err
	}
	inputHost := input.Hostname()
	result := CloudOrigin{
		InputOrigin: input.String(),
		InputHost:   inputHost,
	}
	if canonicalNabuHostPattern.MatchString(inputHost) {
		result.CanonicalOrigin = input.String()
		result.CanonicalHost = inputHost
		return result, nil
	}
	if resolver == nil {
		return CloudOrigin{}, newCloudError(CloudErrInvalidInput, "resolve custom Cloud domain", nil)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, cloudOriginResolutionTimeout)
	defer cancel()
	canonical, err := resolver.LookupCNAME(lookupCtx, inputHost)
	if err != nil {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return CloudOrigin{}, newCloudError(
				CloudErrTimeout,
				"resolve custom Cloud domain",
				err,
			)
		}
		return CloudOrigin{}, newCloudError(CloudErrNetwork, "resolve custom Cloud domain", err)
	}
	canonical = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(canonical), "."))
	if canonical == inputHost || !canonicalNabuHostPattern.MatchString(canonical) {
		return CloudOrigin{}, newCloudError(CloudErrInvalidInput, "resolve custom Cloud domain", nil)
	}
	result.CanonicalOrigin = "https://" + canonical
	result.CanonicalHost = canonical
	result.CustomDomain = true
	return result, nil
}

func (o CloudOrigin) ValidateCertificate(certificate *x509.Certificate) error {
	if certificate == nil || o.CanonicalHost == "" {
		return newCloudError(CloudErrInvalidInput, "validate Cloud certificate", nil)
	}
	if err := certificate.VerifyHostname(o.CanonicalHost); err != nil {
		return newCloudError(CloudErrIdentityMismatch, "validate Cloud certificate", err)
	}
	if o.CustomDomain {
		if err := certificate.VerifyHostname(o.InputHost); err != nil {
			return newCloudError(CloudErrIdentityMismatch, "validate custom Cloud certificate", err)
		}
	}
	return nil
}

func parseStrictCloudOrigin(raw string) (*url.URL, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || len(raw) > 2048 {
		return nil, newCloudError(CloudErrInvalidInput, "validate Cloud origin", nil)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" ||
		parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" ||
		parsed.Fragment != "" || parsed.ForceQuery || parsed.RawPath != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return nil, newCloudError(CloudErrInvalidInput, "validate Cloud origin", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || strings.HasSuffix(host, ".") || net.ParseIP(host) != nil ||
		!validDNSName(host) {
		return nil, newCloudError(CloudErrInvalidInput, "validate Cloud origin", nil)
	}
	if port := parsed.Port(); port != "" && port != "443" {
		return nil, newCloudError(CloudErrInvalidInput, "validate Cloud origin", nil)
	}
	parsed.Scheme = "https"
	parsed.Host = host
	parsed.Path = ""
	parsed.RawPath = ""
	return parsed, nil
}

func validDNSName(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !dnsLabelPattern.MatchString(label) {
			return false
		}
	}
	return true
}
