package main

import (
	"encoding/json"
	"os"
)

// loadConfigDocumentOrEmpty backs the save path. Only an absent config starts
// fresh. An unreadable document fails closed so setup, Cloud rotation, or a
// profile mutation can never erase recoverable sibling state.
func loadConfigDocumentOrEmpty(path string) (*configDocument, error) {
	doc, err := loadConfigDocument(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &configDocument{top: map[string]json.RawMessage{}}, nil
		}
		return nil, err
	}
	if doc.top == nil {
		doc.top = map[string]json.RawMessage{}
	}
	return doc, nil
}
