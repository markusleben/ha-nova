package main

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// The owner's chat-app phone from this computer (contract
// docs/work/2026-10-19-chat-bridge-contract.md, "Setup and CLI (B4)"; spec
// docs/work/2026-10-24-chat-app-phone-move-spec.md): the phone linked here
// goes along as chat apps' first phone (owner_phone), and an App that moves
// it (phone_change) moves it to the phone linked here (owner_phone_move).

// Test seam: how long setup follows a move. Each phone asks for 90 s at most.
var chatAppsMoveWait = 4 * time.Minute

// chatAppsPhoneJob is a move the App started: the bridge's job state.
type chatAppsPhoneJob struct {
	ID       string `json:"id"`
	Status   string `json:"status"`   // running, then done, declined, timeout, rejected, changed, failed, or lost; unread: the App couldn't read it
	Step     string `json:"step"`     // old (the phone chat apps ask now), new
	Phone    string `json:"phone"`    // the phone that step asks; at the end, the one that ended it
	Platform string `json:"platform"` // ios, android, "" (unknown)
}

// handOverOwnerPhone gives the App the owner's phone linked on this computer
// while chat apps are on and the App has none yet (spec §11.4: the owner taps
// once). The state's phone is updated when it took it; err when it didn't.
func handOverOwnerPhone(client *CloudIngressClient, paths runtimePaths, owner string, state *chatAppsState) error {
	phone := ownerChatPhone(paths, owner, *state)
	if phone == nil || !state.Enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second) // the App tries a first phone again for 15 s
	defer cancel()
	fresh, err := chatAppsCall(ctx, client, nil) // the NOVA page may have set one meanwhile
	if err != nil {
		return err
	}
	if fresh.OwnerPhone != nil {
		state.OwnerPhone, state.OwnerPhoneID = fresh.OwnerPhone, fresh.OwnerPhoneID
		return nil
	}
	if _, err := chatAppsCall(ctx, client, map[string]any{"owner_phone": phone}); err != nil {
		return err
	}
	name := phone.Name
	state.OwnerPhone, state.OwnerPhoneID = &name, phone.DeviceID
	return nil
}

// chatAppsPhoneName names the phone that will confirm for chat apps: the
// App's own, else the owner's link on this computer that goes along; ""
// means a code in the chat.
func chatAppsPhoneName(paths runtimePaths, owner string, state chatAppsState) string {
	if state.OwnerPhone != nil {
		return terminalSafeName(*state.OwnerPhone)
	}
	if phone := ownerChatPhone(paths, owner, state); phone != nil {
		return phone.Name
	}
	return ""
}

// ownerChatPhone is the owner's phone linked on this computer, offered as
// chat apps' phone too (spec §11.4: the owner taps once). nil when the link
// isn't the owner's own or the App has one already.
func ownerChatPhone(paths runtimePaths, owner string, state chatAppsState) *chatAppsPhone {
	if state.OwnerPhone != nil {
		return nil
	}
	return ownerComputerPhone(paths, owner)
}

// ownerComputerPhone is the owner's own phone linked on this computer.
func ownerComputerPhone(paths runtimePaths, owner string) *chatAppsPhone {
	links, ok := phoneLinks(paths)
	entry, found := links[owner]
	if !ok || !found || owner == "" || entry.link == nil {
		return nil
	}
	l := entry.link
	name := terminalSafeName(l.Name)
	if l.UserID != owner || !chatAppsService.MatchString(l.Service) || !chatAppsDeviceID.MatchString(l.DeviceID) || name == "" || len(name) > 100 {
		return nil
	}
	return &chatAppsPhone{Service: l.Service, DeviceID: l.DeviceID, Name: name}
}

// moveChatAppsPhone asks the App to move chat apps to deviceID (the phone
// chat apps ask now OKs it first, then the new phone links), then follows
// the move: progress hears each read while it runs, and its end comes
// back. err: it didn't start (the App's refusal), or setup couldn't tell
// how it ended (errChatAppsUnknown).
func moveChatAppsPhone(ctx context.Context, client *CloudIngressClient, deviceID string, progress func(chatAppsPhoneJob)) (chatAppsPhoneJob, error) {
	startCtx, cancel := context.WithTimeout(ctx, 45*time.Second) // the App waits up to 30 s for its bridge
	started, err := chatAppsCall(startCtx, client, map[string]any{"owner_phone_move": map[string]any{"device_id": deviceID}})
	cancel()
	if err != nil {
		var refused *chatAppsRefused
		switch {
		case errors.As(err, &refused) && refused.status >= http.StatusInternalServerError && refused.status != http.StatusServiceUnavailable:
			err = errChatAppsUnknown // 504 CHAT_PHONE_UNKNOWN: only a 503 says nothing changed
		case !errors.As(err, &refused) && !IsCloudErrorCode(err, CloudErrOuterSessionExpired) && ctx.Err() == nil:
			err = errChatAppsUnknown // the answer was lost: it may have started
		}
		return chatAppsPhoneJob{}, err
	}
	if started.PhoneJob == nil || started.PhoneJob.ID == "" {
		return chatAppsPhoneJob{}, errChatAppsUnknown
	}
	id := started.PhoneJob.ID
	for deadline := time.Now().Add(chatAppsMoveWait); time.Now().Before(deadline); {
		select {
		case <-ctx.Done():
			return chatAppsPhoneJob{}, ctx.Err()
		case <-time.After(chatAppsPollEvery):
		}
		readCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		state, err := chatAppsCall(readCtx, client, nil)
		cancel()
		switch {
		case IsCloudErrorCode(err, CloudErrOuterSessionExpired):
			return chatAppsPhoneJob{}, errChatAppsUnknown
		case err != nil || state.PhoneJob == nil || state.PhoneJob.ID != id || state.PhoneJob.Status == "unread":
			// read again: Home Assistant or the App may be busy
		case state.PhoneJob.Status == "running":
			progress(*state.PhoneJob)
		default:
			return *state.PhoneJob, nil
		}
	}
	return chatAppsPhoneJob{}, errChatAppsUnknown
}
