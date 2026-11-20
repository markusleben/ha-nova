package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// An ask the command's wait left open (0.32 spec §6): ha-nova apply waits
// about 90 seconds, the App's ask lives 3 minutes. When the command's own
// wait ends first, the ask stays open and its id is kept beside the stored
// plan (<id>.ask), and the next apply --plan of that plan joins it: an OK
// given meanwhile counts, and nobody is asked twice. The kept id is a hint,
// never an approval: the App holds the ask and its grant, and a grant
// covers only the very bytes it was asked for.

type gateKeptAsk struct {
	AskID    string   `json:"ask_id"`
	Digest   string   `json:"digest"` // the asked requests (gateAskDigest)
	Channels []string `json:"channels"`
	Code     bool     `json:"code,omitempty"` // it takes the chat code the person saw; never the digits
}

func gateKeptPath(paths runtimePaths, planID string) string {
	return filepath.Join(planDir(paths), planID+".ask")
}

func gateAskDigest(reqs []gateRequest) string {
	sum := sha256.New()
	for _, r := range reqs {
		key := gateKey(r)
		sum.Write(key[:])
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// gateKeepAsk keeps the open ask for the plan's next apply. An error: it
// couldn't be kept, so nothing will join it (gateAsk ends it).
func gateKeepAsk(paths runtimePaths, planID string, kept gateKeptAsk) error {
	data, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	path := gateKeptPath(paths, planID)
	temp := fmt.Sprintf("%s.%d", path, os.Getpid())
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

// gateTakeAsk reads and removes the plan's kept ask: ok only for one kept
// for exactly these requests. Whether it is still open, and approved, only
// the App says (gateAsk polls it).
func gateTakeAsk(paths runtimePaths, planID string, reqs []gateRequest) (gateKeptAsk, bool) {
	path := gateKeptPath(paths, planID)
	data, err := os.ReadFile(path)
	_ = os.Remove(path)
	var kept gateKeptAsk
	if err != nil || json.Unmarshal(data, &kept) != nil {
		return gateKeptAsk{}, false
	}
	return kept, kept.AskID != "" && kept.Digest == gateAskDigest(reqs)
}

// gateDropKept ends the ask kept for a plan that is used up or refused for
// good (applyPlan): nothing can use its answer any more.
func (s *relaySession) gateDropKept(planID string) {
	path := gateKeptPath(s.paths, planID)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = os.Remove(path)
	var kept gateKeptAsk
	if json.Unmarshal(data, &kept) == nil && kept.AskID != "" {
		s.gateCancel(kept.AskID)
	}
}

// gateWaiting: the command's wait ended while the person can still
// confirm; nothing was sent, and the plan stays (applyPlan's release).
func gateWaiting(planID, lang string, window bool, channels []string) *verbFailure {
	phase := gateAskingOn(channels)
	if window {
		phase = gateWindowPhase(channels)
	}
	say, next := relayWaitingSay(lang, phase), fmt.Sprintf("Tell the user `say`. Run ha-nova apply --plan %s again right away (give the command up to 2 minutes): it waits for the same ask and doesn't ask again, and an OK given meanwhile counts. Never ask for a code.", planID)
	if slices.Contains(channels, "code") { // the App's chat code (gate_code.go): the person types it back
		say, next = confirmT(lang, "say.relay_code_waiting"), fmt.Sprintf("Tell the user `say` and end your turn. Only if their next message is exactly the four digits HA NOVA showed, run ha-nova apply --confirm <those digits> --plan %s. Never type, guess, or reuse a code yourself.", planID)
	}
	return &verbFailure{exit: verbExitConfirmation, status: "waiting_for_approval", code: "CONFIRM_WAITING", say: say,
		message: "the user hasn't confirmed yet, and the ask stays open for a few minutes; nothing was sent",
		next:    next,
		cause:   errNothingSent}
}

// relayWaitingSay: what a waiting answer tells the person, by the phase
// that names where they confirm (gateAskingOn).
func relayWaitingSay(lang string, phase int32) string {
	switch phase {
	case gateAskingWindow:
		return confirmT(lang, "say.relay_window_waiting")
	case gateAskingWindowOnly:
		return confirmT(lang, "say.relay_window_only_waiting")
	case gateLinking:
		return confirmT(lang, "say.relay_link_waiting")
	case gateAskingPhone:
		return confirmT(lang, "say.relay_waiting_phone")
	case gateAskingPage:
		return confirmT(lang, "say.relay_waiting_page")
	}
	return confirmT(lang, "say.relay_waiting")
}
