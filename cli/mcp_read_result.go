package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ha_read's results (mcp_read.go): Home Assistant's reasons, the camera
// image, jq and the size cap, and the offline snapshot and diff reads.

// upstreamFailure: a core answer of HTTP 400 or more, with Home Assistant's
// reason (the body's message, else an excerpt of the body).
func upstreamFailure(endpoint string, envelope []byte) error {
	var core struct {
		Data struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		} `json:"data"`
	}
	if endpoint != "core" && endpoint != "supervisor" || json.Unmarshal(envelope, &core) != nil || core.Data.Status < http.StatusBadRequest {
		return nil
	}
	message := fmt.Sprintf("Home Assistant answered HTTP %d", core.Data.Status)
	if endpoint == "supervisor" {
		message = fmt.Sprintf("the Supervisor answered HTTP %d", core.Data.Status)
	}
	if reason := upstreamReason(core.Data.Body); reason != "" {
		message += ": " + reason
	}
	return &verbFailure{exit: verbExitError, code: "UPSTREAM_" + strconv.Itoa(core.Data.Status), message: message,
		next: "Nothing was changed. Tell the user Home Assistant's reason, or correct the request."}
}

// upstreamReason: a body's message field, else the body as text, at most
// mcpReasonMax (the body is already redacted).
func upstreamReason(body json.RawMessage) string {
	var parsed struct {
		Message string `json:"message"`
	}
	var text string
	switch {
	case json.Unmarshal(body, &parsed) == nil && parsed.Message != "":
		text = parsed.Message
	case json.Unmarshal(body, &text) == nil:
	default:
		text = string(bytes.TrimSpace(body))
	}
	if text == "null" || text == "[]" || text == "{}" {
		return ""
	}
	return mcpMessage(text, mcpReasonMax)
}

// mcpMessage: Home Assistant's or a verb's text for an error message, with
// any live phone confirmation id hidden and at most max bytes.
func mcpMessage(text string, max int) string {
	quoted, _ := json.Marshal(text)
	_ = json.Unmarshal(redactSecrets(quoted, true), &text)
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// readCamera returns a camera frame as image content, decoded as
// `relay core --out-binary` decodes it.
func readCamera(env *verbEnvelope, data json.RawMessage) error {
	if failure := upstreamFailure("core", redactRawResponse("core", append(append([]byte(`{"ok":true,"data":`), data...), '}'))); failure != nil {
		return failure
	}
	frame, contentType, err := relayBinaryData(data)
	mimeType, _, _ := strings.Cut(strings.ToLower(contentType), ";")
	mimeType = strings.TrimSpace(mimeType)
	if mimeType == "image/jpg" || mimeType == "image/pjpeg" { // what many cameras, HA's demo camera too, send for JPEG
		mimeType = "image/jpeg"
	}
	switch {
	case errors.Is(err, errRelayBodyNotBinary):
		var core struct {
			Body json.RawMessage `json:"body"`
		}
		_ = json.Unmarshal(redactRawResponse("core", data), &core)
		message := "the camera sent no image"
		if reason := upstreamReason(core.Body); reason != "" {
			message += ": " + reason
		}
		return &verbFailure{exit: verbExitError, code: "READ_FAILED", message: message, next: "Nothing was changed. Tell the user Home Assistant's reason."}
	case err != nil:
		return err
	case !mcpCameraTypes[mimeType]:
		return &verbFailure{exit: verbExitError, code: "READ_FAILED", message: fmt.Sprintf("the camera sent a %q picture; chats show JPEG, PNG, GIF, and WebP", mcpMessage(mimeType, 64))}
	case base64.StdEncoding.EncodedLen(len(frame)) > mcpCameraMax: // Claude's limit is on the base64 data
		return &verbFailure{exit: verbExitError, code: "READ_FAILED", message: "the picture is too large for the chat (over 5 MiB once encoded)"}
	}
	env.Result = map[string]any{"mime_type": mimeType, "bytes": len(frame)}
	env.Image = &verbImage{Data: base64.StdEncoding.EncodeToString(frame), MimeType: mimeType}
	return nil
}

// readResult applies the jq filter (one output as is, several or none as a
// list; bounded in time and output) and refuses a result over
// mcpReadMaxResult: nothing partial.
func readResult(endpoint string, input []byte, run jqRun) (json.RawMessage, error) {
	tooLarge := func(size string) error {
		return &verbFailure{exit: verbExitError, code: "RESULT_TOO_LARGE", message: "the result is " + size + " over ha_read's 64 KiB; nothing of it is returned",
			next: "Filter with jq: read again with jq set to a filter that keeps only what you need, e.g. " + mcpTooLargeHint[endpoint] + "."}
	}
	result := input
	if run != nil {
		res, err := run(input)
		switch {
		case errors.Is(err, errJQChildFailed):
			return nil, &verbFailure{exit: verbExitError, code: "JQ_FAILED", message: "jq: the filter needed too much memory or time; use a simpler filter", next: "Nothing was changed. Use a simpler filter, or read less (a narrower path or message), and read again."}
		case errors.Is(err, errJQOutputLimit):
			return nil, tooLarge("(after jq)")
		case errors.Is(err, context.DeadlineExceeded):
			return nil, &verbFailure{exit: verbExitError, code: "JQ_FAILED", message: fmt.Sprintf("jq: the filter ran longer than %s and was stopped", mcpJQDeadline), next: "Nothing was changed. Use a simpler filter and read again."}
		case err != nil:
			return nil, &verbFailure{exit: verbExitError, code: "JQ_FAILED", message: mcpMessage("jq: "+err.Error(), mcpMessageMax), next: "Nothing was changed. Fix the filter and read again."}
		}
		outputs := strings.Split(strings.TrimSuffix(res.output, "\n"), "\n")
		switch {
		case res.output == "":
			result = []byte("[]")
		case len(outputs) == 1:
			result = []byte(outputs[0])
		default:
			result = []byte("[" + strings.Join(outputs, ",") + "]")
		}
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, result); err != nil {
		return nil, fmt.Errorf("the result is not JSON: %w", err)
	}
	if compact.Len() > mcpReadMaxResult {
		return nil, tooLarge(fmt.Sprintf("%d KiB,", (compact.Len()+1023)>>10))
	}
	return compact.Bytes(), nil
}

// runOfflineRead runs a read that needs no Relay (snapshot, diff) into the
// verb envelope.
func runOfflineRead(paths runtimePaths, endpoint string, run jqRun, read func() (json.RawMessage, error)) int {
	env := verbEnvelope{SchemaVersion: 1, Verb: "read", Status: "ok"}
	result, err := read()
	if err == nil {
		result, err = readResult(endpoint, result, run)
	}
	env.Notices = verbNotices(paths, "")
	exit := verbExitOK
	if err != nil {
		exit = setVerbError(&env, err)
	} else {
		env.Result = result
	}
	if !printVerbEnvelope(env) {
		return verbExitError
	}
	return exit
}

// offlineVerb runs a CLI verb in process and returns its stdout; a failure
// carries its stderr.
func offlineVerb(run func() int, ok func(exit int) bool) (string, error) {
	out, errOut, exit := captureVerb(run)
	if !ok(exit) {
		return "", &verbFailure{exit: verbExitError, code: "READ_FAILED", message: mcpMessage(errOut, mcpMessageMax)}
	}
	return out, nil
}

// readSnapshot is `ha-nova snapshot show|verify`; verify's result is
// "match" or "drift".
func readSnapshot(paths runtimePaths, action string, args []string) (json.RawMessage, error) {
	if action == "show" {
		out, err := offlineVerb(func() int { return runSnapshotShow(paths, args) }, func(exit int) bool { return exit == 0 })
		return json.RawMessage(out), err
	}
	out, err := offlineVerb(func() int { return runSnapshotVerify(paths, args) }, func(exit int) bool { return exit == 0 || exit == snapshotExitDrift })
	if err != nil {
		return nil, err
	}
	return json.Marshal(strings.TrimSpace(out))
}

// readDiff is `ha-nova diff`: its table rows, one string each.
func readDiff(paths runtimePaths, args []string) (json.RawMessage, error) {
	out, err := offlineVerb(func() int { return runDiffCommand(paths, args) }, func(exit int) bool { return exit == 0 })
	if err != nil {
		return nil, err
	}
	rows := []string{}
	if out != "" {
		rows = strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	}
	return json.Marshal(rows)
}
