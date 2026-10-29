package main

import (
	_ "embed"
	"fmt"
	"strings"
)

// The confirmation string catalogs (docs/work/2026-10-28-confirmation-
// localization-spec.md): every text a confirmation surface shows, one file
// per language, same format as the setup catalog. A plan speaks the
// language it stored (plan.Lang, sealed in channelSeal); a home whose
// language has no catalog reads English.
//
//go:embed confirm_strings.en.json
var confirmStringsEN []byte

var confirmCatalogs = map[string]map[string]setupEntry{
	"en": mustParseCatalog("confirm string catalog", confirmStringsEN),
}

// confirmLanguage maps Home Assistant's language ("de", "de-CH", "pt_BR")
// to a catalog; "en" when none fits.
func confirmLanguage(haLanguage string) string {
	base, _, _ := strings.Cut(strings.ReplaceAll(strings.ToLower(haLanguage), "_", "-"), "-")
	if _, ok := confirmCatalogs[base]; ok {
		return base
	}
	return "en"
}

// confirmT renders key in lang; kv is placeholder name/value pairs. An
// unknown lang is a programming error (plan.Lang is sealed and set by
// confirmLanguage) and panics like an unknown key.
func confirmT(lang, key string, kv ...any) string {
	catalog, ok := confirmCatalogs[lang]
	if !ok {
		panic("confirm string catalog: unknown language " + lang)
	}
	return renderCatalog(catalog, "confirm string catalog", key, kv, func(v any) string { return fmt.Sprint(v) })
}
