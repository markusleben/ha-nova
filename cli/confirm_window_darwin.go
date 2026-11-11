package main

import (
	"cmp"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// macOS: /usr/bin/osascript running JavaScript for Automation with AppKit's
// NSAlert (SIP-protected, by absolute path, with an empty environment). The
// text sits in a fixed-width view, 560 points wide (windowFits counts for
// it); a text taller than the screen scrolls, never cut. Clicking it from
// another process needs Accessibility or input-posting permission, which a
// model can't grant itself; Cancel is the default button, so Return
// cancels, and Confirm has no key at all.

const windowShowing = "ha-nova-window-showing"

// windowScript prints exactly one answer line; the title, the body, the
// footer, the wait, and the Confirm and Cancel labels arrive as arguments,
// so no Home Assistant text is ever script source. The title is the alert's
// bold one; the footer is the text's last line, smaller and grey. Confirm
// is the second button by position, never a label matched as text. "--" ends osascript's
// options: a message starting with "-" stays an argument. Only no window
// server (a sandbox: no screen) ends the script without windowShowing
// (windowFailed); any other error, before or after the window shows, prints
// "error …" and is no answer.
var windowScript = []string{
	"ObjC.import('AppKit');",
	"function run(argv) {",
	"const room = $.NSScreen.mainScreen.visibleFrame.size.height - 200;", // no screen: throws; minus the alert's own frame with a one-line title
	"try {",
	"const [title, body, footer, wait, confirm, cancel] = argv;",
	"const app = $.NSApplication.sharedApplication;",
	"app.setActivationPolicy(1);", // accessory: no Dock icon
	"const width = 560;",
	"const head = $.NSTextField.wrappingLabelWithString($(title));", // measured only: a longer title leaves the text less room
	"head.font = $.NSFont.boldSystemFontOfSize(13);",
	"head.preferredMaxLayoutWidth = width;",
	"const fit = room - Math.max(0, Math.ceil(head.fittingSize.height) - 16);",
	"const shown = footer ? body + '\\n\\n' + footer : body;",
	"const text = $.NSTextView.alloc.initWithFrame($.NSMakeRect(0, 0, width, 0));",
	"text.string = $(shown);",
	"text.font = $.NSFont.systemFontOfSize(13);",
	"const small = $.NSMakeRange(shown.length - footer.length, footer.length);", // UTF-16 units, as NSString counts
	"text.textStorage.addAttributeValueRange($.NSFontAttributeName, $.NSFont.systemFontOfSize(11), small);",
	"text.textStorage.addAttributeValueRange($.NSForegroundColorAttributeName, $.NSColor.secondaryLabelColor, small);",
	"text.editable = false;",
	"text.drawsBackground = false;",
	"text.textContainer.lineFragmentPadding = 0;",
	"text.layoutManager.ensureLayoutForTextContainer(text.textContainer);",
	"const height = Math.ceil(text.layoutManager.usedRectForTextContainer(text.textContainer).size.height);",
	"text.setFrame($.NSMakeRect(0, 0, width, height));",
	"const bar = height > fit ? $.NSScroller.scrollerWidthForControlSizeScrollerStyle(0, 0) : 0;", // the scroller beside the text, never over it
	"const view = $.NSScrollView.alloc.initWithFrame($.NSMakeRect(0, 0, width + bar, Math.min(height, fit)));",
	"view.documentView = text;",
	"view.hasVerticalScroller = height > fit;",
	"view.scrollerStyle = 0;", // legacy: a scroller that never hides, so more text below is visible
	"view.autohidesScrollers = false;",
	"view.drawsBackground = false;",
	"const alert = $.NSAlert.alloc.init;",
	"alert.messageText = $(title);", // bold
	"alert.icon = $.NSImage.imageNamed('NSCaution');",
	"alert.accessoryView = view;",
	"alert.addButtonWithTitle($(cancel));",                        // first: the default, Return
	"alert.addButtonWithTitle($(confirm)).keyEquivalent = $('');", // Confirm: no key
	"app.performSelectorWithObjectAfterDelayInModes('abortModal', $(), Number(wait), $([$.NSModalPanelRunLoopMode]));",
	"$.NSRunningApplication.currentApplication.activateWithOptions(1);", // in front of the terminal or app that started it
	"console.log('" + windowShowing + "');",                             // stderr: about to show the window
	"const answer = alert.runModal;",
	"if (answer == 1000) return 'cancel';",         // NSAlertFirstButtonReturn
	"if (answer == 1001) return 'button Confirm';", // NSAlertSecondButtonReturn
	"return 'timeout';", // abortModal: the wait ran out
	"} catch (e) { return 'error ' + e; }",
	"}",
}

// nativeWindowAvailable: a logged-in desktop session (Aqua); SSH and
// background sessions are not.
func nativeWindowAvailable() bool {
	cmd := exec.Command("/bin/launchctl", "managername")
	cmd.Env = []string{}
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "Aqua"
}

// nativeButtons: a window with these two labels (gate_client.go); the first
// answers windowConfirmed, Cancel stays the default.
func nativeButtons(ctx context.Context, message, first, cancel string, wait time.Duration) windowAnswer {
	return nativeWindow(ctx, windowText{body: message, button: first, cancel: cancel}, wait)
}

func nativeWindow(ctx context.Context, w windowText, wait time.Duration) windowAnswer {
	args := []string{"-l", "JavaScript"}
	for _, line := range windowScript {
		args = append(args, "-e", line)
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/osascript", append(args, "--", cmp.Or(w.title, "HA NOVA"), w.body, w.footer, strconv.Itoa(int(wait.Seconds())), w.button, w.cancel)...)
	cmd.Env = []string{} // nothing from the caller's environment reaches the window
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return darwinAnswer(strings.TrimSpace(string(out)), err, ctx.Err() != nil, strings.Contains(stderr.String(), windowShowing))
}

// darwinAnswer reads osascript's result. Only a window that provably never
// showed is windowFailed (fallback E): osascript could not start, or the
// script never reached the window (it did not compile, or AppKit can't reach
// the window server, as in a sandbox). Anything after the script reached the
// window — killed, interrupted, an error — is no answer, never a fallback.
func darwinAnswer(out string, err error, ended, showing bool) windowAnswer {
	var exitErr *exec.ExitError
	switch {
	case err == nil && out == "cancel": // a Cancel given before we withdrew the window still counts
		return windowCancelled
	case ended:
		return windowNoAnswer
	case err != nil && !errors.As(err, &exitErr): // osascript could not be started
		return windowFailed
	case !showing && exitErr != nil && exitErr.ExitCode() > 0: // never reached the window
		return windowFailed
	case err == nil && out == "button Confirm":
		return windowConfirmed
	case exitErr != nil && exitErr.ExitCode() == -1: // ended by a signal: closed from outside
		return windowClosed
	}
	return windowNoAnswer
}
