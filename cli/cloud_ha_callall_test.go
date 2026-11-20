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
// any order.
func TestCallAllWritesEveryCommandThenMatchesResultsByID(t *testing.T) {
	conn := &countingHAWebSocketConnection{fakeHAWebSocketConnection: &fakeHAWebSocketConnection{reads: [][]byte{
		[]byte(`{"id":3,"type":"result","success":true,"result":{"logged_in":true}}`),
		[]byte(`{"id":1,"type":"result","success":true,"result":{"id":"user-1"}}`),
		[]byte(`{"id":2,"type":"result","success":true,"result":[]}`),
	}}}
	var user HACurrentUser
	var tokens []HARefreshTokenMetadata
	var status HACloudStatus
	errs := callAllClient(conn).CallAll(context.Background(), []haCall{{"auth/current_user", nil, &user, nil}, {"auth/refresh_tokens", nil, &tokens, nil}, {"cloud/status", nil, &status, nil}})
	if conn.writtenAtFirstRead != 3 || errs[0] != nil || user.ID != "user-1" || errs[1] != nil || errs[2] != nil || !status.LoggedIn {
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
			errs := client.CallAll(context.Background(), []haCall{{"auth/current_user", nil, &user, nil}, {"cloud/status", nil, &status, nil}, {"auth/refresh_tokens", nil, nil, nil}})
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

// A rejection ends the wave as soon as it is known: no later reply is
// awaited, the connection with replies still on the way is closed, and
// later calls are never judged.
func TestCallAllEndsAtTheFirstRejection(t *testing.T) {
	conn := &fakeHAWebSocketConnection{reads: [][]byte{[]byte(`{"id":1,"type":"result","success":true,"result":{"id":"user-2"}}`)}}
	mismatch := errors.New("another user")
	var user HACurrentUser
	errs := callAllClient(conn).CallAll(context.Background(), []haCall{
		{"auth/current_user", nil, &user, func(err error) error {
			if err == nil && user.ID != "user-1" {
				return mismatch
			}
			return err
		}},
		{"cloud/status", nil, &HACloudStatus{}, nil},
	})
	if !errors.Is(errs[0], mismatch) || errs[1] != errHACallNotJudged || len(conn.writes) != 2 || !conn.closed {
		t.Fatalf("errs %v writes %d closed %v", errs, len(conn.writes), conn.closed)
	}
}

// A write that fails after an earlier command was sent doesn't speak over
// that command's own rejection.
func TestCallAllKeepsAnEarlierRejectionOverALaterWriteFailure(t *testing.T) {
	conn := &writeFailsAfter{fakeHAWebSocketConnection: &fakeHAWebSocketConnection{reads: [][]byte{[]byte(`{"id":1,"type":"result","success":false,"error":{"code":"unauthorized"}}`)}}, ok: 1}
	errs := callAllClient(conn).CallAll(context.Background(), []haCall{{"auth/current_user", nil, &HACurrentUser{}, nil}, {"cloud/status", nil, &HACloudStatus{}, nil}})
	if !IsCloudErrorCode(errs[0], CloudErrUnauthorized) || errs[1] != errHACallNotJudged {
		t.Fatalf("errs %v", errs)
	}
}

// writeFailsAfter accepts ok writes, then fails every later one.
type writeFailsAfter struct {
	*fakeHAWebSocketConnection
	ok int
}

func (c *writeFailsAfter) Write(ctx context.Context, data []byte) error {
	if len(c.writes) >= c.ok {
		return errors.New("connection reset")
	}
	return c.fakeHAWebSocketConnection.Write(ctx, data)
}
