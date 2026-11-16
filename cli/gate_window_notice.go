package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

func (n gateWindowNotice) text() (string, string) {
	return confirmT(n.Lang, "phone.window.title", "host", sayText(n.Computer.Name, false)),
		confirmT(n.Lang, "phone.window.body", "goal", sayText(strings.TrimSuffix(n.Title, "?"), false))
}

func (n gateWindowNotice) payload() map[string]any {
	title, body := n.text()
	hash := sha256.Sum256([]byte(n.Computer.ID))
	return map[string]any{"title": phoneCut(title, phoneTitleLimit), "message": body, "data": map[string]any{
		"tag": fmt.Sprintf("ha-nova-window-%x", hash[:8]), "push": map[string]any{"interruption-level": "passive"},
		"channel": "HA NOVA confirmations", "importance": "low", "alert_once": true,
	}}
}

func gateWindowEmit(paths runtimePaths, notice gateWindowNotice, questionTag string, latest bool) {
	s, err := newRelaySessionWithin(paths, verbOptions{}, gateCheckBudget)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Window confirmation notice could not open its Home Assistant connection")
		return
	}
	defer s.close()
	home, err := lookupPhones(s, nil)
	var target *phoneTarget
	if err == nil && phoneAdmin(home.users[notice.Link.UserID]) {
		for _, phone := range home.phones {
			if phone.Service == notice.Link.Service && phone.DeviceID == notice.Link.DeviceID && phone.UserID == notice.Link.UserID {
				target = &phone
				break
			}
		}
	}
	if target == nil {
		fmt.Fprintln(os.Stderr, "Window confirmation notice has no reachable linked administrator's phone")
		return
	}
	title, body := notice.text()
	payloads := []map[string]any{}
	if latest {
		payloads = append(payloads, notice.payload())
	}
	if questionTag != "" {
		payloads = append(payloads, phoneWithdrawnPayloads(title, body, questionTag)...)
	}
	for _, payload := range payloads {
		err := phoneNotify(s, target.Service, payload)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Window confirmation notice could not be sent to Home Assistant")
		}
	}
}
