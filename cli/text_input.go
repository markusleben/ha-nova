package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// normalizeUTF8Bytes accepts one optional leading UTF-8 BOM and otherwise
// preserves the input exactly. It never guesses or transcodes legacy code
// pages: an ambiguous conversion would merely replace one silent corruption
// path with another.
func normalizeUTF8Bytes(data []byte, source string) ([]byte, error) {
	normalized := bytes.TrimPrefix(data, utf8BOM)
	if !utf8.Valid(normalized) {
		return nil, fmt.Errorf("%s is not valid UTF-8", source)
	}
	return normalized, nil
}

func validateUTF8String(value string, source string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", source)
	}
	return nil
}

func strictJSONBytes(data []byte, source string) ([]byte, error) {
	normalized, err := normalizeUTF8Bytes(data, source)
	if err != nil {
		return nil, err
	}
	if !json.Valid(normalized) {
		return nil, fmt.Errorf("%s is not valid JSON", source)
	}
	return normalized, nil
}

func unmarshalStrictJSON(data []byte, source string, target any) error {
	normalized, err := strictJSONBytes(data, source)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(normalized, target); err != nil {
		return fmt.Errorf("cannot decode %s: %w", source, err)
	}
	return nil
}
