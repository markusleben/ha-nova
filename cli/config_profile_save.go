package main

import "fmt"

// saveProfileConfig writes cfg into the selected profile of the on-disk
// document. All read-modify-write config sites go through here (via
// saveConfig), so none of them can flatten the servers map away.
func saveProfileConfig(paths runtimePaths, cfg runtimeConfig) error {
	doc, err := loadConfigDocumentOrEmpty(paths.ConfigFile)
	if err != nil {
		return fmt.Errorf(
			"read existing server configuration: %w",
			err,
		)
	}
	if err := validateSupportedConfigDocument(doc); err != nil {
		return err
	}
	name, err := saveTargetProfileName(doc)
	if err != nil {
		return err
	}
	top, err := doc.withProfile(name, cfg)
	if err != nil {
		return err
	}
	if len(doc.source) == 0 {
		return writeJSONFile(paths.ConfigFile, top, 0o600)
	}
	return writeJSONFileIfUnchanged(
		paths.ConfigFile,
		top,
		0o600,
		doc.source,
	)
}
