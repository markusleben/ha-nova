package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// One HA NOVA notification on the phone, never a stack: a result stays up
// to ten minutes on Android, and a new question next to it lands in a
// "Home Assistant (2)" group that must be opened before the question. So
// each result's tag is kept per phone (per home: two homes can have the same
// notify service name), and the next question to that phone
// clears it first; so is a question withdrawn because the computer
// answered, whose own clear iOS may not deliver (phoneReply.withdraw). Only
// ended asks are kept (their ids died with them), so a clear never removes
// an open question. Best effort: a lost write or clear leaves one old
// notification, as before.

func phoneResultsFile(paths runtimePaths) string {
	return filepath.Join(paths.CacheDir, "phone-results.json")
}

// phoneLastResults: the last result's tag per "<home owner id> <notify service>".
func phoneLastResults(paths runtimePaths) map[string]string {
	tags := map[string]string{}
	if raw, err := os.ReadFile(phoneResultsFile(paths)); err == nil {
		_ = json.Unmarshal(raw, &tags)
	}
	if tags == nil { // a file holding null
		tags = map[string]string{}
	}
	return tags
}

// notePhoneResult records tag as the last result on home's services.
func notePhoneResult(paths runtimePaths, home string, services []string, tag string) {
	tags := phoneLastResults(paths)
	for _, service := range services {
		tags[home+" "+service] = tag
	}
	if raw, err := json.Marshal(tags); err == nil && os.MkdirAll(paths.CacheDir, 0o700) == nil {
		_ = writeFileAtomic(phoneResultsFile(paths), raw, 0o600)
	}
}

// phoneClearWait bounds how long a question waits for its clear: a slow
// clear never eats the time to answer (the tags differ, so order is only
// cosmetic).
var phoneClearWait = time.Second

// clearingFirst sends the question after clearing the result shown on that
// phone (not before it: no moment with two notifications to group). The
// clear goes as high priority as the question: a dozing Android phone gets
// a normal one later, after the question.
func clearingFirst(shown map[string]string, home string, notify func(*relaySession, string, map[string]any) error) func(*relaySession, string, map[string]any) error {
	return func(s *relaySession, service string, body map[string]any) error {
		if old := shown[home+" "+service]; old != "" {
			cleared := make(chan struct{})
			go func() {
				_ = notify(s, service, map[string]any{"message": "clear_notification", "data": map[string]any{"tag": old, "priority": "high"}})
				close(cleared)
			}()
			select {
			case <-cleared:
			case <-time.After(phoneClearWait):
			}
		}
		return notify(s, service, body)
	}
}
