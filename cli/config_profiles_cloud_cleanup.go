package main

import "encoding/json"

// withProfilePreservingInvalidInstallIdentity is limited to security cleanup.
// It preserves the exact existing install-wide value even when malformed, so
// unrelated config damage cannot block revocation. It never permits replacing
// or deleting an immutable value.
func (d *configDocument) withProfilePreservingInvalidInstallIdentity(
	name string,
	cfg runtimeConfig,
) (map[string]json.RawMessage, error) {
	return d.withProfileDocument(name, cfg, false, true)
}
