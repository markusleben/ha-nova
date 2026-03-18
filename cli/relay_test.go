package main

import (
	"strings"
	"testing"
)

func TestLoadRelayPayloadAcceptsSingleQuotedInlineJSON(t *testing.T) {
	payload, err := loadRelayPayload(relayRequestOptions{
		InlineJSON: `'{"type":"ping"}'`,
	})
	if err != nil {
		t.Fatalf("loadRelayPayload() error: %v", err)
	}
	if string(payload) != `{"type":"ping"}` {
		t.Fatalf("payload = %q", string(payload))
	}
}

func TestLoadRelayPayloadAcceptsDoubleQuotedWrappedJSON(t *testing.T) {
	payload, err := loadRelayPayload(relayRequestOptions{
		InlineJSON: `"{\"type\":\"ping\"}"`,
	})
	if err != nil {
		t.Fatalf("loadRelayPayload() error: %v", err)
	}
	if string(payload) != `"{\"type\":\"ping\"}"` {
		t.Fatalf("payload = %q", string(payload))
	}

	payload, err = loadRelayPayload(relayRequestOptions{
		InlineJSON: `"{"type":"ping"}"`,
	})
	if err != nil {
		t.Fatalf("loadRelayPayload() error: %v", err)
	}
	if string(payload) != `{"type":"ping"}` {
		t.Fatalf("payload = %q", string(payload))
	}
}

func TestLoadRelayPayloadKeepsValidPrimitiveJSON(t *testing.T) {
	payload, err := loadRelayPayload(relayRequestOptions{
		InlineJSON: `true`,
	})
	if err != nil {
		t.Fatalf("loadRelayPayload() error: %v", err)
	}
	if string(payload) != `true` {
		t.Fatalf("payload = %q", string(payload))
	}
}

func TestLoadRelayPayloadRejectsInvalidInlineJSONLocally(t *testing.T) {
	_, err := loadRelayPayload(relayRequestOptions{
		InlineJSON: `{type:"ping"}`,
	})
	if err == nil {
		t.Fatal("expected invalid inline JSON to fail locally")
	}
	if !strings.Contains(err.Error(), "inline JSON payload is not valid JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyJQFilterAcceptsSingleQuotedWrappedFilter(t *testing.T) {
	res, err := applyJQFilter(`'.data.unique_id'`, []byte(`{"data":{"unique_id":"abc123"}}`), true)
	if err != nil {
		t.Fatalf("applyJQFilter() error: %v", err)
	}
	if strings.TrimSpace(res.output) != "abc123" {
		t.Fatalf("unexpected output: %q", res.output)
	}
}

func TestApplyJQFilterAcceptsWrappedRegexFilter(t *testing.T) {
	input := `{"data":{"entities":[{"ei":"light.kitchen"},{"ei":"switch.kitchen"}]}}`
	filter := `"[.data.entities[] | select(.ei | test(\"^light\\\\.\")) | .ei]"`

	res, err := applyJQFilter(filter, []byte(input), false)
	if err != nil {
		t.Fatalf("applyJQFilter() error: %v", err)
	}
	if !strings.Contains(res.output, "light.kitchen") {
		t.Fatalf("unexpected output: %q", res.output)
	}
	if strings.Contains(res.output, "switch.kitchen") {
		t.Fatalf("unexpected output: %q", res.output)
	}
}
