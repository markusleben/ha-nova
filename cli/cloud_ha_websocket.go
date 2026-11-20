package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

const (
	haWebSocketMaxMessageBytes = 4 << 20
	haWebSocketMaxCommandBytes = 64 << 10
)

type HAWebSocketConnection interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Close() error
}

type HAWebSocketDialer interface {
	Dial(context.Context, string, *http.Client, int64) (HAWebSocketConnection, int, error)
}

type HAWebSocketClientOptions struct {
	HTTPClient      *http.Client
	Dialer          HAWebSocketDialer
	MaxMessageBytes int64
}

type HAWebSocketClient struct {
	conn   HAWebSocketConnection
	gate   chan struct{}
	nextID int64
}

type coderHAWebSocketDialer struct{}

type coderHAWebSocketConnection struct {
	conn *websocket.Conn
}

func (coderHAWebSocketDialer) Dial(
	ctx context.Context,
	endpoint string,
	httpClient *http.Client,
	maxMessageBytes int64,
) (HAWebSocketConnection, int, error) {
	conn, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient:      httpClient,
		CompressionMode: websocket.CompressionDisabled,
	})
	status := 0
	if response != nil {
		status = response.StatusCode
	}
	if err != nil {
		return nil, status, err
	}
	conn.SetReadLimit(maxMessageBytes)
	return &coderHAWebSocketConnection{conn: conn}, status, nil
}

func (c *coderHAWebSocketConnection) Read(ctx context.Context) ([]byte, error) {
	messageType, data, err := c.conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	if messageType != websocket.MessageText {
		return nil, newCloudError(CloudErrHAProtocol, "read Home Assistant WebSocket", nil)
	}
	return data, nil
}

func (c *coderHAWebSocketConnection) Write(ctx context.Context, data []byte) error {
	return c.conn.Write(ctx, websocket.MessageText, data)
}

// Close skips the close handshake: on a stalled network it waits up to 10 s
// for a peer that has nothing left to say, past every caller's deadline.
func (c *coderHAWebSocketConnection) Close() error {
	return c.conn.CloseNow()
}

func DialHAWebSocket(
	ctx context.Context,
	canonicalOrigin, accessToken string,
	options HAWebSocketClientOptions,
) (*HAWebSocketClient, error) {
	origin, err := ParseCanonicalNabuOrigin(canonicalOrigin)
	if err != nil {
		return nil, err
	}
	return dialHAWebSocketForOrigin(ctx, origin, accessToken, options)
}

func dialHAWebSocketForOrigin(
	ctx context.Context,
	origin *url.URL,
	accessToken string,
	options HAWebSocketClientOptions,
) (*HAWebSocketClient, error) {
	if !validSecretText(accessToken, 8192) {
		return nil, newCloudError(CloudErrInvalidInput, "authenticate Home Assistant WebSocket", nil)
	}
	maxMessageBytes := options.MaxMessageBytes
	if maxMessageBytes <= 0 || maxMessageBytes > 16<<20 {
		maxMessageBytes = haWebSocketMaxMessageBytes
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = coderHAWebSocketDialer{}
	}
	httpClient := cloudNoRedirectHTTPClient(options.HTTPClient, 15*time.Second)
	scheme := "wss"
	if origin.Scheme == "http" {
		scheme = "ws"
	}
	endpoint := scheme + "://" + origin.Host + "/api/websocket"
	conn, status, err := dialer.Dial(ctx, endpoint, httpClient, maxMessageBytes)
	if err != nil {
		if isHTTPRedirect(status) {
			return nil, newCloudHTTPError(CloudErrRedirectRejected, "connect Home Assistant WebSocket", status, false)
		}
		if status == http.StatusUnauthorized {
			return nil, newCloudHTTPError(CloudErrUnauthorized, "connect Home Assistant WebSocket", status, false)
		}
		if status != 0 && status < http.StatusInternalServerError { // rejected, not lost: a Link login is not retried (R1)
			return nil, newCloudHTTPError(CloudErrNetwork, "connect Home Assistant WebSocket", status, false)
		}
		return nil, cloudRequestError("connect Home Assistant WebSocket", err)
	}
	client := &HAWebSocketClient{conn: conn, gate: make(chan struct{}, 1)}
	client.gate <- struct{}{}
	if err := client.authenticate(ctx, accessToken); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}

func (c *HAWebSocketClient) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// haCall is one command of a CallAll wave, where its result decodes, and
// check, which judges that outcome (nil: the outcome as it is).
type haCall struct {
	commandType string
	fields      map[string]any
	result      any
	check       func(error) error
}

func (c *HAWebSocketClient) Call(ctx context.Context, commandType string, fields map[string]any, result any) error {
	return c.CallAll(ctx, []haCall{{commandType, fields, result, nil}})[0]
}

var errHACallNotJudged = newCloudError(CloudErrHAProtocol, "call Home Assistant WebSocket after an earlier command failed", nil)

// CallAll writes every command, then reads one result per command, matched
// by its id: reads that don't depend on each other share one round trip.
// Results are judged (as Call judges one, then check) in the calls' order as
// soon as every earlier one is judged, and the first failure ends the wave at
// once: a later call can't delay it or speak over it. Calls after a failure
// are never judged (errHACallNotJudged); a connection left with unread
// replies is closed.
func (c *HAWebSocketClient) CallAll(ctx context.Context, calls []haCall) []error {
	errs, answered := make([]error, len(calls)), make([]bool, len(calls))
	invalid := newCloudError(CloudErrInvalidInput, "call Home Assistant WebSocket", nil)
	fill := func(err error) []error {
		for i := range errs {
			if !answered[i] {
				errs[i] = err
			}
		}
		return errs
	}
	if c == nil || c.conn == nil || c.gate == nil {
		return fill(invalid)
	}
	for _, call := range calls {
		if call.commandType == "" || len(call.commandType) > 128 || strings.ContainsAny(call.commandType, "\x00\r\n") {
			return fill(invalid)
		}
		for key := range call.fields {
			if key == "id" || key == "type" || key == "" {
				return fill(invalid)
			}
		}
	}
	select {
	case <-ctx.Done():
		return fill(newCloudError(CloudErrTimeout, "call Home Assistant WebSocket", ctx.Err()))
	case <-c.gate:
	}
	defer func() { c.gate <- struct{}{} }()

	judged := 0
	judge := func() bool { // the answered prefix, in order; false at its first failure
		for judged < len(calls) && answered[judged] {
			if check := calls[judged].check; check != nil {
				errs[judged] = check(errs[judged])
			}
			if errs[judged] != nil {
				for i := judged + 1; i < len(calls); i++ {
					errs[i] = errHACallNotJudged
				}
				return false
			}
			judged++
		}
		return true
	}
	sent := make(map[int64]int, len(calls))
	closeIfPending := func() { // replies still on the way: no later call may read them as its own
		for _, i := range sent {
			if !answered[i] {
				_ = c.conn.Close()
				return
			}
		}
	}
	end := func(err error) []error { // every unanswered call fails with err; the earliest failure is judged first
		closeIfPending()
		for i := range calls {
			if !answered[i] {
				answered[i], errs[i] = true, err
			}
		}
		judge()
		return errs
	}
	var writeErr error
	for i, call := range calls {
		c.nextID++
		request := make(map[string]any, len(call.fields)+2)
		request["id"] = c.nextID
		request["type"] = call.commandType
		for key, value := range call.fields {
			request[key] = value
		}
		data, err := json.Marshal(request)
		if err != nil || len(data) > haWebSocketMaxCommandBytes {
			writeErr = newCloudError(CloudErrHAProtocol, "encode Home Assistant WebSocket command", err)
			break
		}
		if err := c.conn.Write(ctx, data); err != nil {
			writeErr = cloudRequestError("write Home Assistant WebSocket command", err)
			break
		}
		sent[c.nextID] = i
	}
	for range sent {
		responseData, err := c.conn.Read(ctx)
		if err != nil {
			return end(cloudHAWebSocketReadError("read Home Assistant WebSocket result", err))
		}
		var response struct {
			ID      int64           `json:"id"`
			Type    string          `json:"type"`
			Success *bool           `json:"success"`
			Result  json.RawMessage `json:"result"`
			Error   *struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		err = json.Unmarshal(responseData, &response)
		i, ok := sent[response.ID]
		if err != nil || !ok || answered[i] || response.Type != "result" || response.Success == nil {
			return end(newCloudError(CloudErrHAProtocol, "decode Home Assistant WebSocket result", err))
		}
		code := ""
		if response.Error != nil {
			code = response.Error.Code
		}
		answered[i] = true
		errs[i] = decodeHAWebSocketResult(*response.Success, response.Error != nil, code, response.Result, calls[i].result)
		if !judge() {
			closeIfPending()
			return errs
		}
	}
	if writeErr != nil {
		return end(writeErr)
	}
	return errs
}

// decodeHAWebSocketResult judges one result envelope: a success carries no
// error, a failure carries a valid error code and no result.
func decodeHAWebSocketResult(success, hasError bool, code string, raw json.RawMessage, result any) error {
	if success {
		if hasError {
			return newCloudError(CloudErrHAProtocol, "decode Home Assistant WebSocket result", nil)
		}
	} else {
		if !hasError || !validIdentifier(code, 128) || (len(raw) != 0 && string(raw) != "null") {
			return newCloudError(CloudErrHAProtocol, "decode Home Assistant WebSocket result", nil)
		}
		errCode := CloudErrHAProtocol
		switch code {
		case "unauthorized":
			errCode = CloudErrUnauthorized
		case "forbidden":
			errCode = CloudErrForbidden
		}
		return newCloudError(errCode, "execute Home Assistant WebSocket command", nil)
	}
	if result == nil {
		return nil
	}
	if len(raw) == 0 || string(raw) == "null" {
		return newCloudError(CloudErrHAProtocol, "decode Home Assistant WebSocket result", nil)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return newCloudError(CloudErrHAProtocol, "decode Home Assistant WebSocket result", err)
	}
	return nil
}

func (c *HAWebSocketClient) authenticate(ctx context.Context, accessToken string) error {
	requiredData, err := c.conn.Read(ctx)
	if err != nil {
		return cloudHAWebSocketReadError(
			"read Home Assistant authentication challenge",
			err,
		)
	}
	var required struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(requiredData, &required); err != nil || required.Type != "auth_required" {
		return newCloudError(CloudErrHAProtocol, "decode Home Assistant authentication challenge", err)
	}
	authData, err := json.Marshal(struct {
		Type        string `json:"type"`
		AccessToken string `json:"access_token"`
	}{Type: "auth", AccessToken: accessToken})
	if err != nil {
		return newCloudError(CloudErrHAProtocol, "encode Home Assistant authentication", err)
	}
	if err := c.conn.Write(ctx, authData); err != nil {
		return cloudRequestError("write Home Assistant authentication", err)
	}
	resultData, err := c.conn.Read(ctx)
	if err != nil {
		return cloudHAWebSocketReadError(
			"read Home Assistant authentication result",
			err,
		)
	}
	var result struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(resultData, &result); err != nil {
		return newCloudError(CloudErrHAProtocol, "decode Home Assistant authentication result", err)
	}
	switch result.Type {
	case "auth_ok":
		return nil
	case "auth_invalid":
		return newCloudError(CloudErrUnauthorized, "authenticate Home Assistant WebSocket", nil)
	default:
		return newCloudError(CloudErrHAProtocol, "decode Home Assistant authentication result", nil)
	}
}

func cloudHAWebSocketReadError(op string, err error) error {
	if errors.Is(err, websocket.ErrMessageTooBig) {
		return newCloudError(CloudErrResponseTooLarge, op, err)
	}
	return cloudRequestError(op, err)
}
