package main

import (
	"bytes"
	"encoding/json"
	"errors"
)

type oauthTokenSuccess struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresIn    json.RawMessage
}

// Home Assistant may add informational fields to a successful token response.
// Stream the object so those remain compatible while duplicate security fields
// cannot be collapsed by encoding/json's last-value-wins behavior.
func decodeOAuthTokenSuccess(data []byte) (oauthTokenSuccess, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return oauthTokenSuccess{}, errors.New(
			"OAuth token response is not a JSON object",
		)
	}
	var payload oauthTokenSuccess
	seenKnown := make(map[string]bool, 4)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return oauthTokenSuccess{}, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return oauthTokenSuccess{}, errors.New(
				"OAuth token response has an invalid field name",
			)
		}
		switch key {
		case "access_token", "refresh_token", "token_type", "expires_in":
			if seenKnown[key] {
				return oauthTokenSuccess{}, errors.New(
					"OAuth token response has duplicate security fields",
				)
			}
			seenKnown[key] = true
		}
		switch key {
		case "access_token":
			err = decoder.Decode(&payload.AccessToken)
		case "refresh_token":
			err = decoder.Decode(&payload.RefreshToken)
		case "token_type":
			err = decoder.Decode(&payload.TokenType)
		case "expires_in":
			err = decoder.Decode(&payload.ExpiresIn)
		default:
			var ignored json.RawMessage
			err = decoder.Decode(&ignored)
		}
		if err != nil {
			return oauthTokenSuccess{}, err
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return oauthTokenSuccess{}, errors.New(
			"OAuth token response JSON object is incomplete",
		)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return oauthTokenSuccess{}, err
	}
	return payload, nil
}
