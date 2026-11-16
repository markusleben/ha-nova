package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

const gateNoticeChannel = "HA NOVA confirmations"

func (n gateWindowNotice) text() (string, string) {
	return confirmT(n.Lang, "phone.window.title", "host", sayText(n.Computer.Name, false)),
		confirmT(n.Lang, "phone.window.body", "goal", sayText(strings.TrimSuffix(n.Title, "?"), false))
}

func (n gateWindowNotice) payload() map[string]any {
	title, body := n.text()
	hash := sha256.Sum256([]byte(n.Computer.ID))
	return map[string]any{"title": phoneCut(title, phoneTitleLimit), "message": body, "data": map[string]any{
		"tag": fmt.Sprintf("ha-nova-window-%x", hash[:8]), "push": map[string]any{"interruption-level": "passive"},
		"channel": gateNoticeChannel, "importance": "low", "alert_once": true,
	}}
}

func gateWindowEmit(paths runtimePaths, notice gateWindowNotice, questionTag string, latest bool, original phoneSetup) {
	services := make([]string, 0, len(original.targets))
	for _, target := range original.targets {
		services = append(services, target.Service)
	}
	if questionTag != "" {
		notePhoneResult(paths, original.owner, services, questionTag)
	}
	s, err := newRelaySessionWithin(paths, verbOptions{}, gateCheckBudget)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Window confirmation notice could not open its Home Assistant connection")
		return
	}
	defer s.close()
	if questionTag != "" {
		withdraw := phoneWithdrawnPayloads(confirmT(notice.Lang, "outcome.ended"), confirmT(notice.Lang, "outcome.line.ended_elsewhere"), questionTag)
		data := withdraw[0]["data"].(map[string]any)
		data["channel"], data["importance"] = gateNoticeChannel, "low"
		for _, service := range services {
			for _, payload := range withdraw {
				if err := phoneNotify(s, service, payload); err != nil {
					fmt.Fprintln(os.Stderr, "Window confirmation question could not be withdrawn from Home Assistant")
				}
			}
		}
	}
	if !latest {
		return
	}
	home, err := gatePhoneHome(s, notice.Link)
	target := home.linked
	if err != nil || target == nil || target.Service == "" || target.DeviceID != notice.Link.DeviceID || !phoneAdmin(home.users[notice.Link.UserID]) {
		fmt.Fprintln(os.Stderr, "Window confirmation notice has no reachable linked administrator's phone")
		return
	}
	if err := phoneNotify(s, target.Service, notice.payload()); err != nil {
		fmt.Fprintln(os.Stderr, "Window confirmation notice could not be sent to Home Assistant")
	}
}
