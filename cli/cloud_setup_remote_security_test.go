package main

import (
	"strings"
	"testing"
)

func TestCanonicalCloudAppURLNeverUsesCustomDiscoveryAlias(t *testing.T) {
	const ingressRoot = "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789"
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		t.Fatal(err)
	}
	origin := CloudOrigin{
		InputOrigin:     "https://home.example.com",
		InputHost:       "home.example.com",
		CanonicalOrigin: "https://unit.ui.nabu.casa",
		CanonicalHost:   "unit.ui.nabu.casa",
		CustomDomain:    true,
	}
	app := HAAddonInfo{
		Slug:         appSlug,
		State:        "started",
		Version:      "1.2.3",
		Ingress:      true,
		IngressEntry: ingressRoot,
		IngressURL:   ingressRoot + haNOVAIngressUIEntry,
	}

	target, err := canonicalCloudAppURL(origin, app)
	if err != nil {
		t.Fatalf("canonicalCloudAppURL: %v", err)
	}
	want := "https://unit.ui.nabu.casa/app/" + appSlug
	if target != want {
		t.Fatalf("target = %q, want %q", target, want)
	}
	if strings.Contains(target, "/api/hassio_ingress/") {
		t.Fatalf("browser target exposed machine ingress capability: %q", target)
	}
}

func TestCanonicalCloudAppURLRejectsUnverifiedIngressPath(t *testing.T) {
	origin, err := cloudOriginFromCanonical("https://unit.ui.nabu.casa")
	if err != nil {
		t.Fatal(err)
	}
	appSlug, err := selectedCloudNOVAAppSlug()
	if err != nil {
		t.Fatal(err)
	}
	_, err = canonicalCloudAppURL(origin, HAAddonInfo{
		Slug:         appSlug,
		State:        "started",
		Version:      "1.2.3",
		Ingress:      true,
		IngressEntry: "/api/hassio_ingress/abcdefghijklmnopqrstuvwxyzABCDEFGH123456789",
		IngressURL:   "https://evil.invalid/",
	})
	if !IsCloudErrorCode(err, CloudErrAppNotReady) {
		t.Fatalf("invalid ingress target error = %v", err)
	}
}
