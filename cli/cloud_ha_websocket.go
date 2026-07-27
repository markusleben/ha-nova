package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

func (c *coderHAWebSocketConnection) Close() error {
	return c.conn.Close(websocket.StatusNormalClosure, "")
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
	endpoint := "wss://" + origin.Host + "/api/websocket"
	conn, status, err := dialer.Dial(ctx, endpoint, httpClient, maxMessageBytes)
	if err != nil {
		if isHTTPRedirect(status) {
			return nil, newCloudHTTPError(CloudErrRedirectRejected, "connect Home Assistant WebSocket", status, false)
		}
		if status == http.StatusUnauthorized {
			return nil, newCloudHTTPError(CloudErrUnauthorized, "connect Home Assistant WebSocket", status, false)
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

func (c *HAWebSocketClient) Call(ctx context.Context, commandType string, fields map[string]any, result any) error {
	if c == nil || c.conn == nil || c.gate == nil ||
		commandType == "" || len(commandType) > 128 ||
		strings.ContainsAny(commandType, "\x00\r\n") {
		return newCloudError(CloudErrInvalidInput, "call Home Assistant WebSocket", nil)
	}
	select {
	case <-ctx.Done():
		return newCloudError(CloudErrTimeout, "call Home Assistant WebSocket", ctx.Err())
	case <-c.gate:
	}
	defer func() { c.gate <- struct{}{} }()

	c.nextID++
	request := make(map[string]any, len(fields)+2)
	request["id"] = c.nextID
	request["type"] = commandType
	for key, value := range fields {
		if key == "id" || key == "type" || key == "" {
			return newCloudError(CloudErrInvalidInput, "call Home Assistant WebSocket", nil)
		}
		request[key] = value
	}
	data, err := json.Marshal(request)
	if err != nil || len(data) > haWebSocketMaxCommandBytes {
		return newCloudError(CloudErrHAProtocol, "encode Home Assistant WebSocket command", err)
	}
	if err := c.conn.Write(ctx, data); err != nil {
		return cloudRequestError("write Home Assistant WebSocket command", err)
	}
	responseData, err := c.conn.Read(ctx)
	if err != nil {
		return cloudHAWebSocketReadError(
			"read Home Assistant WebSocket result",
			err,
		)
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
	if err := json.Unmarshal(responseData, &response); err != nil ||
		response.ID != c.nextID || response.Type != "result" || response.Success == nil {
		return newCloudError(CloudErrHAProtocol, "decode Home Assistant WebSocket result", err)
	}
	if *response.Success {
		if response.Error != nil {
			return newCloudError(
				CloudErrHAProtocol,
				"decode Home Assistant WebSocket result",
				nil,
			)
		}
	} else {
		if response.Error == nil ||
			!validIdentifier(response.Error.Code, 128) ||
			(len(response.Result) != 0 &&
				string(response.Result) != "null") {
			return newCloudError(
				CloudErrHAProtocol,
				"decode Home Assistant WebSocket result",
				nil,
			)
		}
		code := CloudErrHAProtocol
		switch response.Error.Code {
		case "unauthorized":
			code = CloudErrUnauthorized
		case "forbidden":
			code = CloudErrForbidden
		}
		return newCloudError(code, "execute Home Assistant WebSocket command", nil)
	}
	if result == nil {
		return nil
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return newCloudError(CloudErrHAProtocol, "decode Home Assistant WebSocket result", nil)
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
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
