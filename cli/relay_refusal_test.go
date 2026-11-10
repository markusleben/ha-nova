package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Relay refuses some requests with HTTP 403 and its own code (a file
// under /config/www, a file type it never writes, file access set to read).
// Over the local transport (the NOVA App's chat-app bridge, a paired device
// on the home network), that refusal must reach the AI as itself, never as
// "the Relay denied the saved credential". Only a 401, or a 403 that names
// the credential or carries no Relay code, is a sign-in problem.

const wwwDenied = "'www' is never accessible through the relay (credentials, Home Assistant's own state store, or binary data)"

func TestLocalRelayRefusalIsNotACredentialProblem(t *testing.T) {
	envelope := func(code string) []byte {
		return []byte(`{"ok":false,"error":{"code":"` + code + `","message":"m"}}`)
	}
	for _, tc := range []struct {
		status int
		body   []byte
		want   bool
	}{
		{http.StatusUnauthorized, envelope("UNAUTHORIZED"), true},
		{http.StatusUnauthorized, nil, true},
		{http.StatusForbidden, envelope("SECURE_TRANSPORT_REQUIRED"), true},
		{http.StatusForbidden, envelope("DEVICE_REQUIRED"), true},
		{http.StatusForbidden, []byte("<html>Forbidden</html>"), true},
		{http.StatusForbidden, nil, true},
		{http.StatusForbidden, envelope("FILE_PATH_DENIED"), false},
		{http.StatusForbidden, envelope("FILE_TYPE_DENIED"), false},
		{http.StatusForbidden, envelope("FILE_ACCESS_READONLY"), false},
		{http.StatusForbidden, envelope("FILE_ACCESS_DISABLED"), false},
		{http.StatusForbidden, envelope("FILE_OWNER_MISMATCH"), false},
		{http.StatusForbidden, envelope("CORE_PATH_REFUSED"), false},
		{http.StatusBadRequest, envelope("FILE_NOT_TEXT"), false},
	} {
		if got := localRelayRefusedCredential(tc.status, tc.body); got != tc.want {
			t.Errorf("HTTP %d %s: %v, want %v", tc.status, tc.body, got, tc.want)
		}
	}
}

// Reads and writes on one local session: the Relay's 403 refusals keep
// their code; a 401 and a bare 403 still say how to repair the pairing.
func TestLocalSessionKeepsTheRelaysOwnRefusal(t *testing.T) {
	paths, _ := readRelay(t, func(endpoint string, body map[string]any) any {
		switch body["path"] {
		case "/config/www/mailbox":
			return fakeRelayStatus{http.StatusForbidden, "FILE_PATH_DENIED", wwwDenied}
		case "/config/packages/a.yaml":
			return fakeRelayStatus{http.StatusForbidden, "FILE_ACCESS_READONLY", "File access is set to 'read'."}
		case "/config/revoked.yaml":
			return fakeRelayStatus{http.StatusUnauthorized, "UNAUTHORIZED", "unknown credential"}
		case "/config/proxy.yaml":
			return fakeRelayStatus{http.StatusForbidden, "", ""}
		}
		return nil
	})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.send("files", []byte(`{"action":"list_dir","path":"/config/www/mailbox"}`))
	var call *relayCallError
	if !errors.As(err, &call) || call.code != "FILE_PATH_DENIED" || call.message != wwwDenied || strings.Contains(err.Error(), "saved local credential") {
		t.Fatalf("read of a denied path: %v", err)
	}
	_, err = s.sendWrite("files", []byte(`{"action":"write_file","path":"/config/packages/a.yaml","content":"a: 1"}`))
	if !errors.As(err, &call) || call.code != "FILE_ACCESS_READONLY" || strings.Contains(err.Error(), "saved local credential") {
		t.Fatalf("write with file access read: %v", err)
	}
	for _, file := range []string{"/config/revoked.yaml", "/config/proxy.yaml"} {
		_, err = s.send("files", []byte(`{"action":"read_file","path":"`+file+`"}`))
		if err == nil || errors.As(err, &call) || !strings.Contains(err.Error(), "the saved local credential; run: ha-nova setup") {
			t.Errorf("%s: %v", file, err)
		}
	}
}

// Inside the NOVA App (ha-nova serve) a refused sign-in names the App's
// restart, which mints the bridge's sign-in again; there is no setup to run.
func TestBridgeCredentialRefusalNamesTheAppRestart(t *testing.T) {
	bridgeRelay = &runtimeConfig{}
	t.Cleanup(func() { bridgeRelay = nil })
	message := localRelayAuthRepairMessage(http.StatusUnauthorized, nil, defaultServerProfileName, runtimeConfig{RelayBaseURL: "https://127.0.0.1:8792"})
	if !strings.Contains(message, "restart the NOVA Relay App") || strings.Contains(message, "ha-nova setup") {
		t.Fatalf("%q", message)
	}
}

// The reported case: deleting a camera picture under /config/www. The plan
// is refused with the real reason and who can remove it, not as a sign-in.
func TestPlanRawDeleteInADeniedFolderSaysWhy(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{"files read_file": func(map[string]any) any {
		return fakeRelayStatus{http.StatusForbidden, "FILE_PATH_DENIED", wwwDenied}
	}})
	exit, env := planRaw(t, paths, `[{"endpoint":"files","payload":{"action":"delete_file","path":"/config/www/mailbox/latest.jpg"}}]`)
	if exit != verbExitRefused || env.Error == nil || len(sent()) != 0 ||
		!strings.Contains(env.Error.Message, "can't read the file /config/www/mailbox/latest.jpg (FILE_PATH_DENIED)") ||
		!strings.Contains(env.Error.Next, "File editor") || strings.Contains(env.Error.Message+env.Error.Next, "saved local credential") {
		t.Fatalf("exit %d %+v sent %v", exit, env.Error, sent())
	}
}

// From a chat app (ha-nova serve, the App's bridge): at Manage everything a
// file read and a file delete in a denied folder name the Relay's refusal;
// below it, the level refuses before anything is sent. (readRelay: the
// verb harness's Relay refuses every files request as a verb's.)
func TestServeFileRefusalNamesTheRealCause(t *testing.T) {
	fastVerify(t)
	f := household(newFakeHA())
	paths, calls := readRelay(t, func(endpoint string, body map[string]any) any {
		if file, _ := body["path"].(string); endpoint == "files" && strings.HasPrefix(file, "/config/www/") {
			return fakeRelayStatus{http.StatusForbidden, "FILE_PATH_DENIED", wwwDenied}
		}
		if file, _ := body["path"].(string); endpoint == "files" && strings.HasPrefix(file, "/config/off/") {
			return fakeRelayStatus{http.StatusForbidden, "FILE_ACCESS_DISABLED", "File access is disabled. Set the 'file_access' option (read or readwrite) on the NOVA Relay App and restart it."}
		}
		return f.reply(httptest.NewRequest(http.MethodPost, "/"+endpoint, nil), endpoint, body)
	})
	files := func() (n int) {
		for _, c := range calls() {
			if c.endpoint == "files" {
				n++
			}
		}
		return n
	}
	read := map[string]any{"endpoint": "files", "payload": map[string]any{"action": "list_dir", "path": "/config/www/mailbox"}}
	remove := map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "files",
		"payload": map[string]any{"action": "delete_file", "path": "/config/www/mailbox/latest.jpg"}}}}
	s := serveOn(t, &writeHarness{t: t, f: f, paths: paths, log: &fakeRelayLog{}}, levels([]string{userA}, map[string]string{userA: "manage"}))
	for name, args := range map[string]map[string]any{"ha_read": read, "ha_plan": remove} {
		env := s.tool(userA, name, args)
		errInfo, _ := env["error"].(map[string]any)
		message, _ := errInfo["message"].(string)
		next, _ := errInfo["next"].(string)
		if words := message + " " + next; !strings.Contains(words, "FILE_PATH_DENIED") || strings.Contains(words, "saved local credential") || strings.Contains(words, "ha-nova setup") {
			t.Errorf("%s at Manage everything: %v", name, env)
		}
	}
	if files() != 2 { // the list and the delete's read for its copy; never the delete
		t.Fatalf("files requests %v", calls())
	}
	// A Relay answer the plan can't word itself stays RELAY_ERROR, but its
	// step says the Relay answered, never "can't reach its Relay".
	env := s.tool(userA, "ha_plan", map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "files",
		"payload": map[string]any{"action": "delete_file", "path": "/config/off/a.yaml"}}}})
	errInfo, _ := env["error"].(map[string]any)
	if message, _ := errInfo["message"].(string); envCode(env) != "RELAY_ERROR" || env["next_step"] != relayAnsweredNext || !strings.Contains(message, "FILE_ACCESS_DISABLED") {
		t.Errorf("file access off: %v", env)
	}
	for _, level := range []string{"ask", "use"} {
		s.settings(levels([]string{userA}, map[string]string{userA: level}))
		for name, args := range map[string]map[string]any{"ha_read": read, "ha_plan": remove} {
			if code := s.code(userA, name, args); code != "CHAT_LEVEL" {
				t.Errorf("%s at %s: %q", name, level, code)
			}
		}
	}
	if files() != 3 {
		t.Fatalf("files requests below Manage everything %v", calls())
	}
}
