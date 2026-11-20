package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// countingHAWebSocketConnection notes how many commands were written when
// the first result was read.
type countingHAWebSocketConnection struct {
	*fakeHAWebSocketConnection
	writtenAtFirstRead int
	read               bool
}

func (c *countingHAWebSocketConnection) Read(ctx context.Context) ([]byte, error) {
	if !c.read {
		c.read, c.writtenAtFirstRead = true, len(c.writes)
	}
	return c.fakeHAWebSocketConnection.Read(ctx)
}

func callAllClient(conn HAWebSocketConnection) *HAWebSocketClient {
	client := &HAWebSocketClient{conn: conn, gate: make(chan struct{}, 1)}
	client.gate <- struct{}{}
	return client
}

// PERF-03: the Link login's reads share one round trip. Every command is
// written before the first result is read, results are matched by id in
// any order, and each is judged on its own.
func TestCallAllWritesEveryCommandThenMatchesResultsByID(t *testing.T) {
	conn := &countingHAWebSocketConnection{fakeHAWebSocketConnection: &fakeHAWebSocketConnection{reads: [][]byte{
		[]byte(`{"id":3,"type":"result","success":true,"result":{"logged_in":true}}`),
		[]byte(`{"id":1,"type":"result","success":true,"result":{"id":"user-1"}}`),
		[]byte(`{"id":2,"type":"result","success":false,"error":{"code":"unauthorized"}}`),
	}}}
	var user HACurrentUser
	var tokens []HARefreshTokenMetadata
	var status HACloudStatus
	errs := callAllClient(conn).CallAll(context.Background(), []haCall{{"auth/current_user", nil, &user}, {"auth/refresh_tokens", nil, &tokens}, {"cloud/status", nil, &status}})
	if conn.writtenAtFirstRead != 3 || errs[0] != nil || user.ID != "user-1" || !IsCloudErrorCode(errs[1], CloudErrUnauthorized) || errs[2] != nil || !status.LoggedIn {
		t.Fatalf("written at first read %d, errs %v, user %+v, status %+v", conn.writtenAtFirstRead, errs, user, status)
	}
	for i, want := range []string{"auth/current_user", "auth/refresh_tokens", "cloud/status"} {
		var command map[string]any
		if err := json.Unmarshal(conn.writes[i], &command); err != nil || command["type"] != want || command["id"] != float64(i+1) {
			t.Fatalf("command %d = %s", i, conn.writes[i])
		}
	}
}

// A result for an id never sent, or sent twice, or a broken stream, fails
// every call still unanswered; an answered one keeps its outcome.
func TestCallAllFailsUnansweredCallsOnABrokenStream(t *testing.T) {
	for name, tc := range map[string]struct {
		second []byte
		err    error
		code   CloudErrorCode
	}{
		"duplicate id": {second: []byte(`{"id":1,"type":"result","success":true,"result":{"id":"user-2"}}`), code: CloudErrHAProtocol},
		"unknown id":   {second: []byte(`{"id":9,"type":"result","success":true,"result":{}}`), code: CloudErrHAProtocol},
		"read error":   {err: errors.New("connection reset")},
	} {
		t.Run(name, func(t *testing.T) {
			conn := &fakeHAWebSocketConnection{reads: [][]byte{[]byte(`{"id":1,"type":"result","success":true,"result":{"id":"user-1"}}`)}}
			if tc.second != nil {
				conn.reads = append(conn.reads, tc.second)
			}
			client := callAllClient(&secondReadFails{conn, tc.err})
			var user HACurrentUser
			var status HACloudStatus
			errs := client.CallAll(context.Background(), []haCall{{"auth/current_user", nil, &user}, {"cloud/status", nil, &status}, {"auth/refresh_tokens", nil, nil}})
			if errs[0] != nil || user.ID != "user-1" || errs[1] == nil || errs[2] == nil || (tc.code != "" && (!IsCloudErrorCode(errs[1], tc.code) || !IsCloudErrorCode(errs[2], tc.code))) {
				t.Fatalf("errs %v user %+v", errs, user)
			}
		})
	}
}

// secondReadFails returns err on the second read, when err is set.
type secondReadFails struct {
	*fakeHAWebSocketConnection
	err error
}

func (c *secondReadFails) Read(ctx context.Context) ([]byte, error) {
	if c.err != nil && len(c.reads) == 0 {
		return nil, c.err
	}
	return c.fakeHAWebSocketConnection.Read(ctx)
}
