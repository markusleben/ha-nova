package main

import (
	"encoding/json"
	"net/http"
)

// gateTextsHandler answers /texts: the NOVA approval page's own words
// (nova/src/http/page-texts.ts) in Home Assistant's language, read as an
// ask reads it. The caller checked the bearer. The Relay fills {minutes} and
// {count} with a number; the approver's name enters only here, through
// sayText.
func gateTextsHandler(paths runtimePaths, w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil {
		http.Error(w, "the request must be one JSON object with name", http.StatusBadRequest)
		return
	}
	s, err := newRelaySessionWithin(paths, verbOptions{}, gateCheckBudget)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	lang := s.homeLanguage()
	s.close()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"lang": lang, "texts": gatePageTexts(lang, in.Name)})
}

// gatePageTexts: every page text in lang, placeholders for numbers kept.
func gatePageTexts(lang, name string) map[string]string {
	return map[string]string{
		"page.title":                confirmT(lang, "page.title"),
		"page.heading":              confirmT(lang, "page.heading"),
		"page.done_approved":        confirmT(lang, "page.done_approved"),
		"page.done_cancelled":       confirmT(lang, "page.done_cancelled"),
		"page.done_gone":            confirmT(lang, "page.done_gone"),
		"page.ends_in":              confirmT(lang, "page.ends_in", "minutes", "{minutes}"),
		"page.approve":              confirmT(lang, "page.approve"),
		"window.cancel":             confirmT(lang, "window.cancel"),
		"page.empty":                confirmT(lang, "page.empty"),
		"page.more":                 confirmT(lang, "page.more", "count", "{count}"),
		"page.signed_in":            confirmT(lang, "page.signed_in", "name", sayText(name, true)),
		"page.back":                 confirmT(lang, "page.back"),
		"page.returning":            confirmT(lang, "page.returning"),
		"page.cross_site":           confirmT(lang, "page.cross_site"),
		"page.expired":              confirmT(lang, "page.expired"),
		"page.open_again":           confirmT(lang, "page.open_again"),
		"page.approver_unknown":     confirmT(lang, "page.approver_unknown"),
		"page.approver_unavailable": confirmT(lang, "page.approver_unavailable"),
		"page.approver_not_admin":   confirmT(lang, "page.approver_not_admin"),
		"page.approver_relay_user":  confirmT(lang, "page.approver_relay_user"),
		"page.waiting_one":          confirmT(lang, "page.waiting_one"),
		"page.waiting_many":         confirmT(lang, "page.waiting_many", "count", "{count}"),
		"page.open":                 confirmT(lang, "page.open"),
		"page.paused":               confirmT(lang, "page.paused"),
		"page.revoke_device":        confirmT(lang, "page.revoke_device"),
		"page.link":                 confirmT(lang, "page.link"),
		"page.paused_heading":       confirmT(lang, "page.paused_heading"),
		"page.paused_left":          confirmT(lang, "page.paused_left", "minutes", "{minutes}"),
		"page.resume":               confirmT(lang, "page.resume"),
	}
}
