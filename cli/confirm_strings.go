package main

import (
	_ "embed"
	"fmt"
	"regexp"
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

//go:embed confirm_strings.de.json
var confirmStringsDE []byte

var confirmCatalogs = map[string]map[string]setupEntry{
	"en": mustParseCatalog("confirm string catalog", confirmStringsEN),
	"de": mustParseCatalog("confirm string catalog (de)", confirmStringsDE),
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
	return renderCatalog(catalog, "confirm string catalog", key, kv, func(v any) string { return confirmQuote(lang, fmt.Sprint(v)) })
}

// sayQuotePair: a pair of quote marks sayText wrote; a name it quoted holds
// no quote mark of any language, so the pair is exactly its own.
var sayQuotePair = regexp.MustCompile(`“([^“”„‚‘’]*)”`)

// confirmQuoteMarks: a catalog's quote marks where they differ from
// sayText's “…”. Its own texts write them; confirmQuote re-marks the values
// they take, so English stays as it is.
var confirmQuoteMarks = map[string]string{"de": "„$1“"}

// confirmQuote: s with sayText's quotes marked as lang marks them
// („Haustür“).
func confirmQuote(lang, s string) string {
	if marks, ok := confirmQuoteMarks[lang]; ok {
		return sayQuotePair.ReplaceAllString(s, marks)
	}
	return s
}
