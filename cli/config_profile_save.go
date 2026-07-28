package main

import (
	"bytes"
	"fmt"
)

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

func saveProfileConfigIfUnchanged(
	paths runtimePaths,
	cfg runtimeConfig,
	expected []byte,
	expectedExists bool,
) ([]byte, error) {
	cfg.SchemaVersion = configSchemaVersion
	doc, err := loadConfigDocumentOrEmpty(paths.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf(
			"read existing server configuration: %w",
			err,
		)
	}
	if expectedExists && !bytes.Equal(doc.source, expected) {
		return nil, errConditionalJSONConflictRestored
	}
	if !expectedExists && len(doc.source) != 0 {
		return nil, errConditionalJSONConflictRestored
	}
	if err := validateSupportedConfigDocument(doc); err != nil {
		return nil, err
	}
	name, err := saveTargetProfileName(doc)
	if err != nil {
		return nil, err
	}
	top, err := doc.withProfile(name, cfg)
	if err != nil {
		return nil, err
	}
	if !expectedExists {
		return writeJSONFileIfAbsentSnapshot(
			paths.ConfigFile,
			top,
			0o600,
		)
	}
	return writeJSONFileIfUnchangedSnapshot(
		paths.ConfigFile,
		top,
		0o600,
		expected,
	)
}
