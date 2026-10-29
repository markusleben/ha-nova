package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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
// guessing), an edited prompt would show the human a different action
// next to the real code, and an edited language would word the phone and
// window asks of apply differently from what the plan showed, and so would
// edited Home Assistant words.
func channelSeal(credential string, plan storedPlan) string {
	mac := hmac.New(sha256.New, []byte(credential))
	words, _ := json.Marshal(plan.Words) // map keys sort
	if len(plan.Words) == 0 {            // as stored: the file drops an empty map
		words = nil
	}
	fields, _ := json.Marshal([]string{plan.ID, plan.Channel, plan.Kind, plan.Lang, plan.Prompt, string(words)}) // unambiguous field bounds
	mac.Write(append([]byte("ha-nova/plan-seal/v3\x00"), fields...))
	return hex.EncodeToString(mac.Sum(nil))
}

// planSealed: the plan's seal matches (every typed plan carries one).
// Its language must be one this binary speaks (a newer binary's plan is
// refused, never a crash).
func planSealed(plan storedPlan, credential string) bool {
	return plan.Lang != "" && confirmLanguage(plan.Lang) == plan.Lang && hmac.Equal([]byte(plan.ChannelSeal), []byte(channelSeal(credential, plan)))
}

// Where the human confirms a typed plan (spec §3; human-confirmation-
// channels spec §2). A form or window plan never takes a code from argv:
// only the human's answer in that form or window confirms it.
const (
	channelChat   = "chat"   // the code is in `say`; the model sees it too (fallback E)
	channelForm   = "form"   // the client showed confirmation forms and no window could open when the plan was made
	channelWindow = "window" // a native window on this computer asks (confirm_window.go)
	channelPhone  = "phone"  // the linked phone asks (confirm_phone.go)
	// channelPhoneMissing is never stored: the linked phone can't be asked
	// now, so the plan is refused (confirm_phone_setup.go).
	channelPhoneMissing = "phone-missing"
	// channelPhoneUnlinked is never stored: #719's pin, no phone chosen yet,
	// so the plan is refused until one is linked, never downgraded.
	channelPhoneUnlinked = "phone-unlinked"
	// channelChatOff is never stored: ha-nova serve, no phone linked for
	// this user, and the owner turned chat codes off (mcp_serve_phone.go).
	channelChatOff = "chat-off"
)

// channelRefusal: a channel no plan may be stored with, and why, quoting
// phone texts in lang, read only then (the home's: the link ask speaks it,
// Decision 2).
func channelRefusal(channel string, lang func() string) *verbFailure {
	switch channel {
	case channelPhoneMissing:
		return refusedWith("CONFIRM_UNAVAILABLE", phoneMissingWhy, phoneMissingStep())
	case channelPhoneUnlinked:
		return refusedWith("CONFIRM_UNAVAILABLE", phoneUnlinkedWhy, phoneUnlinkedNext(lang()))
	case channelChatOff:
		return refusedWith("CONFIRM_UNAVAILABLE", chatCodesOffWhy(chatCall), chatCodesOffNext(chatCall))
	}
	return nil
}

// humanChannel: the human answers in a window or on a phone; the plan's
// code never leaves this process.
func humanChannel(channel string) bool { return channel == channelWindow || channel == channelPhone }

// planChannel is set only by the MCP server, from the client's handshake,
// for the verb it runs; empty means the CLI detects it (detectChannel).
// humanAnswer is the code the CLI derives in process once the human
// confirmed in a form or window, for the apply it runs. Neither has a flag
// or an environment variable. humanWhere: the channel that answered (window
// or phone; both ask for a window plan when a phone is linked), which apply
// reports as plan.channel.
var (
	planChannel string
	humanAnswer string
	humanWhere  string
)

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

// withholdAsk swaps the closing code block (and the chat note above it)
// for sayFormAsk, in the say's language. A Home Assistant name can't spoof
// the block: sayText strips line breaks and asterisks from it.
func withholdAsk(say, code string) string {
	head, lang, found := sayCodeHead(say, code)
	if !found { // unreachable (say and code come from one plan); fail closed
		return sayFormAsk("en")
	}
	return strings.TrimSuffix(head, "\n"+sayChatNote(lang)) + " " + sayFormAsk(lang)
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
