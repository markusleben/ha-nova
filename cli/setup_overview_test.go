package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func overviewFixture(t *testing.T) *setupFlow {
	t.Helper()
	resetServerProfileSelection(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	withClientRuntimeAvailability(t, map[string]bool{"claude": true, "codex": true, "opencode": true, "antigravity": true, "hermes": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	state := defaultInstallState()
	choices, err := buildSetupClientChoices(paths, state)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{HAHost: "home", HAURL: "http://home", RelayBaseURL: "http://home:8791"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.BinDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PublicBinary, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := windowAvailable
	windowAvailable = func() bool { return true }
	t.Cleanup(func() { windowAvailable = old })
	return &setupFlow{paths: paths, state: state, choices: choices}
}

func overviewByID(items []setupOverviewItem) map[string]setupOverviewItem {
	out := map[string]setupOverviewItem{}
	for _, item := range items {
		out[item.ID] = item
	}
	return out
}

func TestSetupOverviewEachDetectedAppUsesActualWiring(t *testing.T) {
	f := overviewFixture(t)
	items := overviewByID(f.setupOverview(setupPageControl{Window: true, Phone: "none"}, false))
	if items["claude"].State != "ok" || items["claude"].Action != "" {
		t.Fatalf("Claude = %+v", items["claude"])
	}
	for _, id := range []string{"codex", "opencode", "antigravity", "hermes"} {
		if got := items[id]; got.State != "todo" || got.Action != "clients" || got.Value != id {
			t.Errorf("%s = %+v", id, got)
		}
	}
	f.choices[0].Disabled = true
	if got := overviewByID(f.setupOverview(setupPageControl{}, false))[f.choices[0].Value]; !got.More || got.State == "todo" || got.Action != "" || got.Command != "" {
		t.Fatalf("an app not found here was offered as a step: %+v", got)
	}
}

func TestSetupOverviewGroupsSectionsInPageAndTerminalOrder(t *testing.T) {
	f := overviewFixture(t)
	f.choices[1].Disabled = true
	ctl := setupPageControl{Phone: "pick", Link: "offer", MCPConfig: "{}", Chat: &setupPageChat{State: "page_off"}}
	var got []string
	for _, item := range f.setupOverview(ctl, true) {
		label := item.Section + ":" + item.ID
		if item.More {
			label += "(more)"
		}
		got = append(got, label)
	}
	want := []string{"home:link", "apps:claude", "apps:opencode", "apps:antigravity", "apps:hermes", "apps:codex(more)", "apps:other",
		"confirm:window", "confirm:phone", "chat:chat", "chat:chat_phone"}
	if !slices.Equal(got, want) {
		t.Fatalf("order = %v\nwant %v", got, want)
	}
	var out bytes.Buffer
	renderSetupOverview(&out, f.setupOverview(ctl, true), nil)
	text := out.String()
	last := -1
	for _, key := range []string{"setup.overview.section_home", "setup.overview.section_apps", "setup.overview.more_label", "setup.overview.other_label", "setup.overview.section_confirm", "setup.overview.section_chat"} {
		at := strings.Index(text, setupT(key))
		if at <= last {
			t.Fatalf("%s out of order:\n%s", key, text)
		}
		last = at
	}
	if strings.Contains(text, "ha-nova setup codex") {
		t.Fatalf("an app not found here got a setup command:\n%s", text)
	}
}

func TestSetupOverviewUnknownDoesNotMeanOffOrDone(t *testing.T) {
	f := overviewFixture(t)
	for _, chat := range []*setupPageChat{
		{State: "page_on"}, {State: "page_off"}, {State: "noaddress"},
	} {
		items := overviewByID(f.setupOverview(setupPageControl{Phone: "unread", Chat: chat}, false))
		if items["phone"].State != "unknown" || items["chat_phone"].State != "unknown" || items["chat_phone"].Action != "nova" {
			t.Fatalf("unknown phone became known: %+v", items)
		}
		if _, present := items["link"]; present {
			t.Fatal("Link appeared without this home's remote access")
		}
	}
	items := overviewByID(f.setupOverview(setupPageControl{Phone: "linked", PhoneName: "Phone", Chat: &setupPageChat{State: "page_on"}}, false))
	if items["phone"].State != "ok" || items["chat_phone"].State != "unknown" {
		t.Fatalf("this computer's phone was mistaken for the chat-app phone: %+v", items)
	}
	for _, confirmed := range []bool{false, true} {
		items = overviewByID(f.setupOverview(setupPageControl{Chat: &setupPageChat{State: "on", PhoneKnown: true, Confirmed: confirmed}}, false))
		want := "todo"
		if confirmed {
			want = "ok"
		}
		if items["chat_phone"].State != want {
			t.Fatalf("known phone: %+v", items["chat_phone"])
		}
	}
}

func TestSetupOverviewWindowAndChatProblemsStayHonest(t *testing.T) {
	f := overviewFixture(t)
	ctl := setupPageControl{Chat: &setupPageChat{State: "page_on", Problem: setupPageText("setup.chat.on_noaddress")}}
	items := overviewByID(f.setupOverview(ctl, true))
	if items["window"].Action != "window" || items["chat"].State != "unknown" || items["link"].State != "unknown" {
		t.Fatalf("items = %+v", items)
	}
	windowAvailable = func() bool { return false }
	items = overviewByID(f.setupOverview(ctl, false))
	if items["window"].State != "unknown" || items["window"].Action != "" {
		t.Fatalf("window = %+v", items["window"])
	}
	ctl.Chat = &setupPageChat{State: "noaddress", Problem: setupPageText("setup.chat.on_noaddress")}
	if got := overviewByID(f.setupOverview(ctl, false))["chat"]; got.Msg.Key != "setup.overview.chat_problem" {
		t.Fatalf("chat apps on without an address were called off: %+v", got)
	}
}

func TestSetupOverviewTerminalGolden(t *testing.T) {
	f := overviewFixture(t)
	ctl := setupPageControl{Phone: "linked", PhoneName: "My phone", Link: "offer", Chat: &setupPageChat{State: "page_off"}}
	var err error
	ctl.MCPConfig, err = setupMCPConfig(f.paths)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderSetupOverview(&out, f.setupOverview(ctl, true), ctl.Chat)
	assertSetupGolden(t, "overview", out.String(), f.paths.Home, "<home>")
}

func TestSetupOverviewMCPSnippetPinsMultipleServersAndEscapesPaths(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, `{"schema_version":3,"default_server":"home","servers":{"home":{},"cabin":{}}}`)
	paths.PublicBinary = filepath.Join(t.TempDir(), `Program Files\HA NOVA\ha-nova.exe`)
	if err := os.WriteFile(paths.PublicBinary, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	activeServerProfileName = "cabin"
	config, err := setupMCPConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if json.Unmarshal([]byte(config), &result) != nil || result.Servers["ha-nova"].Command != paths.PublicBinary ||
		!slices.Equal(result.Servers["ha-nova"].Args, []string{"mcp", "--server", "cabin"}) {
		t.Fatalf("snippet = %s", config)
	}
	if !strings.Contains(config, `\\HA NOVA\\`) {
		t.Fatalf("backslashes were not escaped: %s", config)
	}
	f := overviewFixture(t)
	one, err := setupMCPConfig(f.paths)
	if err != nil || strings.Contains(one, "--server") {
		t.Fatalf("single-profile snippet = %s, %v", one, err)
	}
}

func TestSetupOverviewMCPSnippetKeepsPublicSymlinkAndUsesPortableExecutable(t *testing.T) {
	f := overviewFixture(t)
	paths := f.paths
	paths.PublicBinary = filepath.Join(t.TempDir(), "ha-nova")
	if err := os.Symlink(f.paths.PublicBinary, paths.PublicBinary); err != nil {
		t.Fatal(err)
	}
	config, err := setupMCPConfig(paths)
	if err != nil || !strings.Contains(config, paths.PublicBinary) {
		t.Fatalf("public symlink not preserved: %s, %v", config, err)
	}
	paths.PublicBinary = filepath.Join(t.TempDir(), "missing")
	old := executablePathForInstallSource
	t.Cleanup(func() { executablePathForInstallSource = old })
	executablePathForInstallSource = func() (string, error) { return f.paths.PublicBinary, nil }
	config, err = setupMCPConfig(paths)
	if err != nil || !strings.Contains(config, f.paths.PublicBinary) {
		t.Fatalf("portable executable not used: %s, %v", config, err)
	}
}

func TestSetupOverviewPhoneNoticesDoNotDisableChatApps(t *testing.T) {
	f := overviewFixture(t)
	for _, key := range []string{"setup.chat.phone_kept", "setup.chat.problem_phone"} {
		items := overviewByID(f.setupOverview(setupPageControl{Chat: &setupPageChat{State: "page_on", Problem: &setupPageMsg{Key: key}}}, false))
		if items["chat"].State != "ok" || items["chat"].Msg.Key != "setup.overview.chat_on" {
			t.Fatalf("phone note made chat apps unusable: %+v", items["chat"])
		}
	}
}

func TestSetupOverviewTerminalAfterSkillRepair(t *testing.T) {
	paths, cfg, state, _ := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{}, nil)
	var out bytes.Buffer
	f := setupFlow{paths: paths, cfg: cfg, state: state, current: setupState{ConfigOK: true}, selectedClients: []string{"claude"},
		ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("")), out: &out}}
	result := f.finish()
	if !result.done || result.code != 0 || !strings.Contains(out.String(), "Your setup") || !strings.Contains(out.String(), "Other AI apps") {
		t.Fatalf("repaired setup omitted the overview: %+v\n%s", result, &out)
	}
}

func TestSetupOverviewWindowActionWorksThroughPageAPI(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{}, nil)
	old := windowAvailable
	windowAvailable = func() bool { return true }
	t.Cleanup(func() { windowAvailable = old })
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	doc.top[windowSetting] = json.RawMessage(`"off"`)
	if err := writeJSONFileIfUnchanged(paths.ConfigFile, doc.top, 0o600, doc.source); err != nil {
		t.Fatal(err)
	}
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))
	page.waitFor("window setup", accepting("window"))
	page.click("window", "")
	page.waitFor("window on", func(s setupPageState) bool {
		return s.Control != nil && s.Control.Window && slices.Contains(s.Accepts, "finish")
	})
	page.click("finish", "")
	if code := setupPageExit(t, done, stdout); code != 0 || windowOff(paths) {
		t.Fatalf("window action exit %d\n%s", code, stdout)
	}
}

func TestSetupOverviewTerminalActionsKeepServerSelection(t *testing.T) {
	f := overviewFixture(t)
	f.paths.ConfigFile = writeTestConfigFile(t, `{"schema_version":3,"default_server":"cabin","servers":{"default":{},"cabin":{}}}`).ConfigFile
	activeServerProfileName = "default"
	items := overviewByID(f.setupOverview(setupPageControl{Phone: "pick", Link: "offer", Chat: &setupPageChat{State: "page_off"}}, true))
	for _, id := range []string{"codex", "phone", "chat", "link"} {
		if !strings.HasSuffix(items[id].Command, " --server default") {
			t.Errorf("%s loses selected server: %+v", id, items[id])
		}
	}
	if strings.Contains(items["window"].Command, "--server") {
		t.Fatal("the install-wide window setting got a server flag")
	}
}

func TestSetupOverviewPhoneHelpAcceptsServerWithoutLinking(t *testing.T) {
	f := overviewFixture(t)
	captureStdout(t, func() {
		if code := dispatch(f.paths, "ha-nova", []string{"setup", "confirm-phone", "--help", "--server", "cabin"}); code != 0 {
			t.Fatalf("phone help exit %d", code)
		}
	})
	if name, _ := requestedServerSelection(); name != "cabin" {
		t.Fatalf("phone action selected %q", name)
	}
}

func TestSetupOverviewUncertainChatAndPhoneStayUnknown(t *testing.T) {
	f := overviewFixture(t)
	chat := &setupPageChat{State: "offer", Problem: setupPageText("setup.chat.problem_unknown")}
	if item := overviewByID(f.setupOverview(setupPageControl{Chat: chat}, false))["chat"]; item.State != "unknown" {
		t.Fatalf("uncertain turn-on was called off: %+v", item)
	}
	f.ui = setupTextUI{out: io.Discard}
	chat = &setupPageChat{State: "on", Problem: setupPageText("setup.chat.problem_phone")}
	f.pageChatNote(chat, chatAppsState{})
	if item := overviewByID(f.setupOverview(setupPageControl{Chat: chat}, false))["chat_phone"]; item.State != "unknown" {
		t.Fatalf("uncertain handover was called not yet: %+v", item)
	}
}

func TestSetupOverviewRemoteOnlyChatUsesHomeAssistantInsteadOfUnsupportedCommands(t *testing.T) {
	f := overviewFixture(t)
	f.cfg.Cloud = &cloudLifecycleMetadata{Current: &cloudConnectionMetadata{Origin: "https://home.example"}}
	items := overviewByID(f.setupOverview(setupPageControl{Chat: &setupPageChat{State: "page_off"}}, true))
	for _, id := range []string{"chat", "chat_phone"} {
		if items[id].Command != "https://home.example" {
			t.Fatalf("remote-only action cannot work: %+v", items[id])
		}
	}
}

func TestSetupOverviewTurnOnReadsTheAppsCurrentPhone(t *testing.T) {
	f := overviewFixture(t)
	phone := "Already linked phone"
	app := &fakeChatApp{state: chatAppsState{Available: true, OwnerPhone: &phone}}
	ha := chatHAReady
	ha.ha.loaded = true
	useChatApps(t, app, &fakeChatHA{first: ha}, chatTestLink)
	f.ui = setupTextUI{out: io.Discard}
	chat := &setupPageChat{State: "offer"}
	offer := &chatAppsOffer{url: chatTestLink + chatAppsConnectorPath}
	f.pageChatOn(chat, offer, func() {})
	if !chat.PhoneKnown || !chat.Confirmed || !f.pageChatPhoned {
		t.Fatalf("the App's current phone was lost: %+v", chat)
	}
}

func TestSetupOverviewLoadingProblemSurvivesPhoneHandoverFailure(t *testing.T) {
	f := overviewFixture(t)
	app := &fakeChatApp{state: chatAppsState{Available: true, Enabled: true}, phoneErr: errSetupExit}
	useChatApps(t, app, &fakeChatHA{first: chatHAReady}, chatTestLink)
	if err := os.WriteFile(f.paths.ConfigFile, []byte(`{"schema_version":1,"confirm_phone":{"owner-id":{"service":"mobile_app_phone","device_id":"0123456789abcdef0123456789abcdef","user_id":"owner-id","name":"My phone"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f.ui = setupTextUI{out: io.Discard}
	chat := f.pageChatRow(&chatAppsOffer{state: app.state, url: chatTestLink + chatAppsConnectorPath})
	if chat.Problem == nil || chat.Problem.Key != "setup.chat.problem_slow" || chat.PhoneKnown {
		t.Fatalf("phone error hid the loading failure: %+v", chat)
	}
	if item := overviewByID(f.setupOverview(setupPageControl{Chat: chat}, false))["chat"]; item.State != "unknown" {
		t.Fatalf("unloaded chat apps were marked ready: %+v", item)
	}
}

func TestSetupOverviewWindowActionKeepsOtherSettings(t *testing.T) {
	f := overviewFixture(t)
	if err := os.MkdirAll(filepath.Dir(f.paths.ConfigFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.paths.ConfigFile, []byte(`{"confirm_window":"off","ha_host":"home"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if runConfirmWindowSetting(f.paths, []string{"on"}) != 0 || windowOff(f.paths) {
		t.Fatal("the existing window setting action did not turn it on")
	}
	data, err := os.ReadFile(f.paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil || doc["ha_host"] != "home" {
		t.Fatalf("other settings changed: %s", data)
	}
}
