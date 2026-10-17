package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// The one-time code from HA NOVA (docs/work/2026-10-14-typed-verbs-spec.md
// §2.1 Code): four digits a typed plan carries and only the user types.

// confirmCode is four digits of HMAC-SHA256(Relay credential, identity ‖
// channel ‖ expiry) (docs/work/2026-10-17-confirmation-hardening-spec.md
// §2 B9): no plan file field yields it, a file edit (channel, expiry) changes
// it, and minting a plan file needs the credential, which never leaves the
// CLI. Uniform over 0000-9999 (bias below 10^-15); recomputed at apply.
func confirmCode(credential, idHex, channel, expiresAt string) string {
	mac := hmac.New(sha256.New, []byte(credential))
	mac.Write([]byte("ha-nova/confirm-code/v1\x00" + idHex + "\x00" + channel + "\x00" + expiresAt))
	return fmt.Sprintf("%04d", binary.BigEndian.Uint64(mac.Sum(nil)[:8])%10000)
}

// channelSeal binds a typed plan's channel, kind, and form text to its id
// under the Relay credential. Without it, a form plan edited to "chat" in the
// (user-writable) plan file would take argv codes against a code nobody was
// shown (the file's mismatch counter, which an editor resets, would not stop
// guessing), and an edited prompt would show the human a different action
// next to the real code.
func channelSeal(credential string, plan storedPlan) string {
	mac := hmac.New(sha256.New, []byte(credential))
	mac.Write([]byte("ha-nova/plan-seal/v1\x00" + plan.ID + "\x00" + plan.Channel + "\x00" + plan.Kind + "\x00" + plan.Prompt))
	return hex.EncodeToString(mac.Sum(nil))
}

// planSealed: the plan's seal matches (every typed plan carries one).
func planSealed(plan storedPlan, credential string) bool {
	return hmac.Equal([]byte(plan.ChannelSeal), []byte(channelSeal(credential, plan)))
}

// Where the human confirms a typed plan (spec §3; human-confirmation-
// channels spec §2). A form or window plan never takes a code from argv:
// only the human's answer in that form or window confirms it.
const (
	channelChat   = "chat"   // the code is in `say`; the model sees it too (fallback E)
	channelForm   = "form"   // the client showed confirmation forms and no window could open when the plan was made
	channelWindow = "window" // a native window on this computer asks (confirm_window.go)
	channelPhone  = "phone"  // the Home Assistant owner's phone asks (confirm_phone.go)
	// channelPhoneMissing is never stored: the pinned phone can't be reached,
	// so the plan is refused (confirm_phone_setup.go).
	channelPhoneMissing = "phone-missing"
	// channelPhoneUnpinned is never stored: the phone was found but its pin
	// could not be saved, so the plan is refused rather than unprotected.
	channelPhoneUnpinned = "phone-unpinned"
)

// humanChannel: the human answers in a window or on a phone; the plan's
// code never leaves this process.
func humanChannel(channel string) bool { return channel == channelWindow || channel == channelPhone }

// planChannel is set only by the MCP server, from the client's handshake,
// for the verb it runs; empty means the CLI detects it (detectChannel).
// humanAnswer is the code the CLI derives in process once the human
// confirmed in a form or window, for the apply it runs. Neither has a flag
// or an environment variable.
var (
	planChannel string
	humanAnswer string
)

// sayChatNote closes a chat plan's ask: the honest limit of fallback E.
const sayChatNote = "Note: the assistant can see this code too, so it only counts if you type it yourself in your reply."

var fourDigits = regexp.MustCompile(`^[0-9]{4}$`)

// typedCode is what the user typed as a code: spaces and invisible format
// characters (LRM, RLM, ZWSP) removed, and full-width (CJK keyboards),
// Arabic-Indic, Persian, and Devanagari digits read as 0-9, so "48 21" and
// "４８２１" are 4821. Nothing else is read: letters, punctuation, and "yes"
// stay as typed and never match.
func typedCode(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		for _, zero := range []rune{'０', '٠', '۰', '०'} {
			if r >= zero && r <= zero+9 {
				return '0' + r - zero
			}
		}
		return r
	}, s)
}

// sayTypedAsk ends a typed plan's `say`; goal is the plan's own action
// ("unlock “Front Door”"), so the code reads as bound to it.
func sayTypedAsk(code, goal string) string {
	return "Type " + code + " to " + goal + " — a plain yes can't confirm this."
}

// sayWindowAsk closes a window plan's `say`: the code is never shown.
const sayWindowAsk = "A confirmation window opens on this computer — confirm there."

// sayPhoneAsk closes a phone plan's `say`: the code is never shown.
const sayPhoneAsk = "HA NOVA sends a confirmation to your phone — tap Confirm there."

// sayFormAsk replaces sayTypedAsk where the client shows a confirmation
// form: the code is never in a tool result there (mcp_confirm.go).
const sayFormAsk = "Your app will ask for the four-digit one-time code from HA NOVA in a form — a plain yes can't confirm this."

// withholdAsk swaps the closing ask for sayFormAsk: the last "Type <code> to "
// outside “…”, since a Home Assistant name (always quoted; sayText strips
// quote marks from it) may contain the same words.
func withholdAsk(say, code string) string {
	at, depth := -1, 0
	for i, r := range say {
		switch {
		case r == '“':
			depth++
		case r == '”':
			depth--
		case depth == 0 && strings.HasPrefix(say[i:], "Type "+code+" to "):
			at = i
		}
	}
	if at < 0 { // unreachable (say and code come from one plan); fail closed
		return sayFormAsk
	}
	return say[:at] + sayFormAsk
}

// lowerFirst: "Unlock “Door”" → "unlock “Door”" (names are quoted, so only
// the verb changes).
func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToLower(r[0])
	}
	return string(r)
}
