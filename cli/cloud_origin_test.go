package main

import (
	"context"
	"crypto/x509"
	"errors"
	"testing"
)

type fakeCloudResolver struct {
	canonical string
	err       error
	host      string
}

func TestCustomCloudOriginRequiresOneCertificateForBothNames(t *testing.T) {
	origin := CloudOrigin{
		InputOrigin:     "https://home.example.com",
		InputHost:       "home.example.com",
		CanonicalOrigin: "https://unit.ui.nabu.casa",
		CanonicalHost:   "unit.ui.nabu.casa",
		CustomDomain:    true,
	}
	if err := origin.ValidateCertificate(&x509.Certificate{
		DNSNames: []string{"home.example.com", "unit.ui.nabu.casa"},
	}); err != nil {
		t.Fatalf("dual-name certificate rejected: %v", err)
	}
	if err := origin.ValidateCertificate(&x509.Certificate{
		DNSNames: []string{"home.example.com"},
	}); !IsCloudErrorCode(err, CloudErrIdentityMismatch) {
		t.Fatalf("custom-only certificate error = %v", err)
	}
}

func (r *fakeCloudResolver) LookupCNAME(_ context.Context, host string) (string, error) {
	r.host = host
	return r.canonical, r.err
}

func TestResolveCanonicalNabuOrigin(t *testing.T) {
	direct, err := ResolveCanonicalNabuOrigin(
		context.Background(),
		"https://UNIT.ui.nabu.casa:443/",
		nil,
	)
	if err != nil {
		t.Fatalf("direct origin: %v", err)
	}
	if direct.CanonicalOrigin != "https://unit.ui.nabu.casa" ||
		direct.CanonicalHost != "unit.ui.nabu.casa" || direct.CustomDomain {
		t.Fatalf("direct = %+v", direct)
	}

	resolver := &fakeCloudResolver{canonical: "unit.ui.nabu.casa."}
	custom, err := ResolveCanonicalNabuOrigin(
		context.Background(),
		"https://home.example.com/",
		resolver,
	)
	if err != nil {
		t.Fatalf("custom origin: %v", err)
	}
	if resolver.host != "home.example.com" ||
		custom.InputOrigin != "https://home.example.com" ||
		custom.CanonicalOrigin != "https://unit.ui.nabu.casa" ||
		!custom.CustomDomain {
		t.Fatalf("custom = %+v resolver_host=%q", custom, resolver.host)
	}
}

func TestResolveCanonicalNabuOriginRejectsUnsafeInputAndCNAME(t *testing.T) {
	for _, raw := range []string{
		"http://unit.ui.nabu.casa",
		"https://unit.ui.nabu.casa:8443",
		"https://user@unit.ui.nabu.casa",
		"https://unit.ui.nabu.casa/path",
		"https://unit.ui.nabu.casa?token=x",
		"https://127.0.0.1",
		"https://localhost",
		" https://unit.ui.nabu.casa",
	} {
		if _, err := ResolveCanonicalNabuOrigin(context.Background(), raw, nil); err == nil {
			t.Errorf("accepted unsafe origin %q", raw)
		}
	}

	for _, canonical := range []string{"home.example.com.", "ui.nabu.casa.", "a.b.ui.nabu.casa.", "127.0.0.1."} {
		resolver := &fakeCloudResolver{canonical: canonical}
		if _, err := ResolveCanonicalNabuOrigin(
			context.Background(),
			"https://home.example.com",
			resolver,
		); err == nil {
			t.Errorf("accepted unsafe CNAME %q", canonical)
		}
	}

	resolver := &fakeCloudResolver{err: errors.New("DNS offline")}
	if _, err := ResolveCanonicalNabuOrigin(
		context.Background(),
		"https://home.example.com",
		resolver,
	); !IsCloudErrorCode(err, CloudErrNetwork) {
		t.Fatalf("DNS error = %v", err)
	}

	resolver = &fakeCloudResolver{err: context.DeadlineExceeded}
	if _, err := ResolveCanonicalNabuOrigin(
		context.Background(),
		"https://home.example.com",
		resolver,
	); !IsCloudErrorCode(err, CloudErrTimeout) {
		t.Fatalf("DNS timeout = %v", err)
	}
}
