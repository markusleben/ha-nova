package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type setupOverviewItem struct {
	ID      string        `json:"id"`
	State   string        `json:"state"`
	Label   string        `json:"label"`
	Msg     *setupPageMsg `json:"msg"`
	Action  string        `json:"action,omitempty"`
	Value   string        `json:"value,omitempty"`
	Command string        `json:"command,omitempty"`
	Snippet string        `json:"snippet,omitempty"`
	Section string        `json:"section"`
	More    bool          `json:"more,omitempty"`
}

var setupOverviewSections = []string{"home", "apps", "confirm", "chat"}

func setupOverviewSection(id string) string {
	switch id {
	case "ha", "app", "computer", "link":
		return "home"
	case "window", "phone":
		return "confirm"
	case "chat", "chat_phone":
		return "chat"
	}
	return "apps"
}

func (f *setupFlow) setupOverview(ctl setupPageControl, remote bool) []setupOverviewItem {
	items := []setupOverviewItem{}
	_, profileCount := selectedServerProfileStatus(f.paths)
	add := func(id, state, label string, msg *setupPageMsg, action, command string) {
		if profileCount > 1 && id != "window" && strings.HasPrefix(command, "ha-nova ") {
			command += " --server " + activeServerProfile()
		}
		items = append(items, setupOverviewItem{ID: id, State: state, Label: label, Msg: msg, Action: action, Command: command, Section: setupOverviewSection(id)})
	}
	for _, choice := range f.choices {
		if choice.Value == "all" {
			continue
		}
		if choice.Disabled {
			add(choice.Value, "unknown", choice.Label, setupPageText("setup.overview.client_missing"), "", "")
			items[len(items)-1].More = true
			continue
		}
		if clientReadyNow(f.paths, f.state, choice.Value) {
			add(choice.Value, "ok", choice.Label, setupPageText("setup.overview.client_on"), "", "")
		} else {
			add(choice.Value, "todo", choice.Label, setupPageText("setup.overview.client_off"), "clients", "ha-nova setup "+choice.Value)
			items[len(items)-1].Value = choice.Value
		}
	}
	switch {
	case ctl.Window:
		add("window", "ok", setupT("setup.page.control.window_key"), setupPageText("setup.overview.window_on"), "", "")
	case windowAvailable():
		add("window", "todo", setupT("setup.page.control.window_key"), setupPageText("setup.overview.window_off"), "window", "ha-nova setup confirm-window on")
	default:
		add("window", "unknown", setupT("setup.page.control.window_key"), setupPageText("setup.overview.window_unavailable"), "", "")
	}
	phoneLabel := setupT("setup.overview.phone_label")
	switch ctl.Phone {
	case "linked":
		add("phone", "ok", phoneLabel, setupPageText("setup.overview.phone_on", "phone", ctl.PhoneName), "phone", "ha-nova setup confirm-phone")
	case "pick":
		add("phone", "todo", phoneLabel, setupPageText("setup.overview.phone_off"), "phone", "ha-nova setup confirm-phone")
	case "waiting", "moving":
		add("phone", "run", phoneLabel, setupPageText("setup.overview.phone_wait"), "phone", "")
	case "none":
		add("phone", "todo", phoneLabel, setupPageText("setup.overview.phone_none"), "phone", "ha-nova setup confirm-phone")
	case "unavailable":
		add("phone", "unknown", phoneLabel, setupPageText("setup.overview.phone_unavailable"), "", "ha-nova setup confirm-phone")
	default:
		add("phone", "unknown", phoneLabel, setupPageText("setup.overview.phone_unread"), "phone", "ha-nova setup confirm-phone")
	}
	if chat := ctl.Chat; chat != nil {
		if chat.Problem != nil && chat.Problem.Key == "setup.chat.problem_unknown" {
			add("chat", "unknown", setupT("setup.chat.key"), setupPageText("setup.overview.chat_unknown"), "chat", "")
		} else if chat.State == "run" || chat.State == "signin" || chat.State == "expired" {
			add("chat", "unknown", setupT("setup.chat.key"), setupPageText("setup.overview.chat_wait"), "chat", "")
		} else if chat.State == "on" || chat.State == "page_on" || chat.State == "noaddress" && chat.Problem != nil {
			state, msg := "ok", setupPageText("setup.overview.chat_on")
			if chat.Problem != nil && chat.Problem.Key != "setup.chat.phone_kept" && chat.Problem.Key != "setup.chat.phone_page" && chat.Problem.Key != "setup.chat.problem_phone" {
				state, msg = "unknown", setupPageText("setup.overview.chat_problem")
			}
			add("chat", state, setupT("setup.chat.key"), msg, "chat", "ha-nova chat-apps status")
		} else {
			add("chat", "todo", setupT("setup.chat.key"), setupPageText("setup.overview.chat_off"), "chat", "ha-nova chat-apps on")
		}
		switch {
		case chat.Confirmed:
			add("chat_phone", "ok", setupT("setup.overview.chat_phone_label"), setupPageText("setup.overview.chat_phone_on"), "", "")
		case !chat.PhoneKnown:
			add("chat_phone", "unknown", setupT("setup.overview.chat_phone_label"), setupPageText("setup.overview.chat_phone_unread"), "nova", f.cfg.HAURL)
		default:
			add("chat_phone", "todo", setupT("setup.overview.chat_phone_label"), setupPageText("setup.overview.chat_phone_off"), "nova", f.cfg.HAURL)
		}
		if f.cfg.RelayBaseURL == "" && f.cfg.Cloud.ready() {
			for i := range items {
				if items[i].ID == "chat" || items[i].ID == "chat_phone" {
					items[i].Command = f.cfg.Cloud.Current.Origin
				}
			}
		}
	}
	if remote {
		switch {
		case f.cfg.Cloud.ready():
			msg := setupPageText("setup.overview.link_saved")
			if f.cfg.RoutePolicy == routePolicyAutomatic {
				msg = setupPageText("setup.overview.link_on")
			}
			add("link", "ok", setupT("setup.page.control.link_key"), msg, "", "")
		case ctl.Link == "offer" || ctl.Link == "run":
			add("link", "todo", setupT("setup.page.control.link_key"), setupPageText("setup.overview.link_off"), "link", "ha-nova cloud add")
		default:
			add("link", "unknown", setupT("setup.page.control.link_key"), setupPageText("setup.overview.link_unavailable"), "", "ha-nova cloud status")
		}
	}
	if ctl.MCPConfig != "" {
		add("other", "unknown", setupT("setup.overview.other_label"), setupPageText("setup.overview.other"), "mcp", "")
		items[len(items)-1].Snippet = ctl.MCPConfig
	}
	rank := func(item setupOverviewItem) int {
		switch {
		case item.ID == "other":
			return 3
		case item.More:
			return 2
		}
		return map[string]int{"home": 0, "apps": 1, "confirm": 4, "chat": 5}[item.Section]
	}
	sort.SliceStable(items, func(i, j int) bool { return rank(items[i]) < rank(items[j]) })
	return items
}

func setupMCPConfig(paths runtimePaths) (string, error) {
	command := paths.PublicBinary
	info, err := os.Stat(command)
	if os.IsNotExist(err) {
		command, err = executablePathForInstallSource()
	} else if err == nil && info.IsDir() {
		return "", fmt.Errorf("HA NOVA's program path is a folder: %s", command)
	}
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(command) {
		return "", fmt.Errorf("HA NOVA's program path is not absolute: %s", command)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return "", err
	}
	args := []string{"mcp"}
	if len(doc.profileNames()) > 1 {
		args = append(args, "--server", activeServerProfile())
	}
	entry := struct {
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env,omitempty"`
	}{Command: command, Args: args}
	if os.Getenv(configDirEnv) != "" { // a GUI app doesn't inherit it
		entry.Env = map[string]string{configDirEnv: paths.ConfigDir}
	}
	data, err := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"ha-nova": entry}}, "", "  ")
	return string(data), err
}

func renderSetupOverview(out io.Writer, items []setupOverviewItem, chat *setupPageChat) {
	headings := map[string]string{
		"home":    setupT("setup.overview.section_home"),
		"apps":    setupT("setup.overview.section_apps"),
		"confirm": setupT("setup.overview.section_confirm"),
		"chat":    setupT("setup.overview.section_chat"),
	}
	fmt.Fprintf(out, "\n  %s\n", setupT("setup.overview.title"))
	section, more := "", []string{}
	flushMore := func() {
		if len(more) > 0 {
			fmt.Fprintf(out, "  · %s: %s\n", setupT("setup.overview.more_label"), setupT("setup.overview.more", "apps", strings.Join(more, ", ")))
			more = nil
		}
	}
	for _, item := range items {
		if item.Section != section {
			flushMore()
			section = item.Section
			fmt.Fprintf(out, "\n  %s\n", headings[section])
		}
		if item.More {
			more = append(more, item.Label)
			continue
		}
		flushMore()
		mark := "·"
		if item.State == "ok" {
			mark = "✓"
		}
		fmt.Fprintf(out, "  %s %s: %s\n", mark, item.Label, item.Msg.plain())
		if item.Command != "" {
			fmt.Fprintf(out, "    %s\n", item.Command)
		}
		if item.Snippet != "" {
			fmt.Fprintln(out, item.Snippet)
		}
	}
	flushMore()
	if chat != nil && chat.URL != "" {
		printChatAppsHowTo(out, chat.URL)
	}
}

func printCompletedSetupOverview(out io.Writer, paths runtimePaths, cfg runtimeConfig) error {
	state, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		return err
	}
	choices, err := buildSetupClientChoices(paths, state)
	if err != nil {
		return err
	}
	f := setupFlow{paths: paths, cfg: cfg, state: state, choices: choices, pageRerunning: true}
	ctl := setupPageControl{Window: windowAvailable() && !windowOff(paths)}
	ctl.MCPConfig, err = setupMCPConfig(paths)
	if err != nil {
		return err
	}
	f.pagePhones(&ctl)
	origin := chatAppsLinkOrigin(cfg, setupLinkRemoteOrigin(cfg))
	if origin != "" && f.setupLinkOffered(out) {
		ctl.Link = "offer"
	}
	ctl.Chat = f.pageRerunChat(func() string { return chatAppsLinkOrigin(cfg, origin) })
	host := terminalSafeName(cfg.HAHost)
	if host == "" && cfg.Cloud.ready() {
		host = cfg.Cloud.Current.Origin
	}
	home := []setupOverviewItem{
		{ID: "ha", State: "ok", Label: setupT("setup.page.ha.key"), Msg: setupPageText("setup.page.rerun.ha", "host", host), Section: "home"},
		{ID: "app", State: "ok", Label: setupT("setup.page.app.key"), Msg: setupPageText("setup.page.app.running"), Section: "home"},
		{ID: "computer", State: "ok", Label: setupT("setup.page.rerun.computer_key"), Msg: setupPageText("setup.page.rerun.computer_ok"), Section: "home"},
	}
	renderSetupOverview(out, append(home, f.setupOverview(ctl, origin != "")...), ctl.Chat)
	return nil
}
