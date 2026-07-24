package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func detectedUnsupportedUnicodeEncoding(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0x00, 0x00, 0xFE, 0xFF}):
		return "UTF-32BE"
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE, 0x00, 0x00}):
		return "UTF-32LE"
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return "UTF-16BE"
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return "UTF-16LE"
	case looksLikeBOMLessUnicodeText(data, 4, 0):
		return "BOM-less UTF-32LE"
	case looksLikeBOMLessUnicodeText(data, 4, 3):
		return "BOM-less UTF-32BE"
	case looksLikeBOMLessUnicodeText(data, 2, 0):
		return "BOM-less UTF-16LE"
	case looksLikeBOMLessUnicodeText(data, 2, 1):
		return "BOM-less UTF-16BE"
	default:
		return ""
	}
}

// looksLikeBOMLessUnicodeText recognizes an unambiguous ASCII text prefix
// encoded as UTF-16/32. It is deliberately conservative: arbitrary legacy
// code pages cannot be identified reliably and must fall through to strict
// UTF-8 validation.
func looksLikeBOMLessUnicodeText(data []byte, width, asciiOffset int) bool {
	if len(data) < width {
		return false
	}
	unit := data[:width]
	for byteIndex, value := range unit {
		if byteIndex != asciiOffset && value != 0 {
			return false
		}
	}
	value := unit[asciiOffset]
	return value == '\t' || value == '\n' || value == '\r' || (value >= 0x20 && value <= 0x7E)
}

// normalizeUTF8Bytes accepts one optional leading UTF-8 BOM and otherwise
// preserves the input exactly. It never guesses or transcodes legacy code
// pages: an ambiguous conversion would merely replace one silent corruption
// path with another.
func normalizeUTF8Bytes(data []byte, source string) ([]byte, error) {
	normalized := bytes.TrimPrefix(data, utf8BOM)
	if bytes.HasPrefix(normalized, utf8BOM) {
		return nil, fmt.Errorf("%s contains more than one leading UTF-8 BOM; at most one is supported", source)
	}
	if encoding := detectedUnsupportedUnicodeEncoding(normalized); encoding != "" {
		return nil, fmt.Errorf("%s is not valid UTF-8: detected %s; only UTF-8 is supported", source, encoding)
	}
	if !utf8.Valid(normalized) {
		return nil, fmt.Errorf("%s is not valid UTF-8; its text encoding is unsupported or ambiguous", source)
	}
	return normalized, nil
}

func relayTextFileEncodingError(err error) error {
	return fmt.Errorf(
		"%w; nothing was sent. Windows PowerShell 5.1: read and write with System.IO.File plus an explicit System.Text.UTF8Encoding; a leading UTF-8 BOM is accepted, but default Get-Content/Set-Content, Out-File, and > are unsafe encoding boundaries",
		err,
	)
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
