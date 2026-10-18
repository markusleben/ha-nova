package main

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func renderLook(look *setupFirstLook) string {
	out := &bytes.Buffer{}
	renderSetupFirstLook(out, look, "Open Claude Code and try asking:")
	return out.String()
}

func lookOf(r firstLookResult, lock *firstSeed) *setupFirstLook {
	return &setupFirstLook{firstLookResult: r, Lock: lock}
}

func TestSetupFirstLookRendersRealNames(t *testing.T) {
	got := renderLook(lookOf(firstLookResult{
		Home: map[string]int{"areas": 5, "devices": 68, "automations": 1, "entities": 512},
		Observations: []firstLookFinding{
			{Kind: "battery_low", EntityID: "sensor.door_battery", Name: "Door battery", Area: "Hall", Value: "7%", Count: 2},
			{Kind: "automation_unavailable", EntityID: "automation.x", Name: "Night mode", Count: 1},
		},
		Seeds: map[string]firstSeed{
			"area":       {AreaID: "living", Name: "Living room"},
			"light":      {EntityID: "light.sofa", Name: "Sofa lamp", Area: "Living room"},
			"climate":    {EntityID: "sensor.living_temp", Name: "Living temperature", Area: "Living room"},
			"cover":      {EntityID: "cover.terrace", Name: "Terrace blind"},
			"automation": {EntityID: "automation.morning", Name: "Morning"},
		},
	}, &firstSeed{EntityID: "lock.front", Name: "Front door"}))
	want := `  Your home: 5 rooms, 68 devices, 1 automation.

  Worth a look:
    - Low battery: Door battery (Hall) is at 7% (and 1 more).
    - The automation "Night mode" can't run right now.

  Open Claude Code and try asking:

    "Turn on Sofa lamp"
    "How warm is it in Living room?"
    "Why did the automation 'Morning' run last time?"

  Ask to unlock Front door and HA NOVA first asks you to confirm it yourself; a plain yes in chat isn't enough, and nothing happens until you do.

`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestSetupFirstLookWithoutSeedsOrObservationsNamesNothing(t *testing.T) {
	got := renderLook(lookOf(firstLookResult{Home: map[string]int{"devices": 3}, Observations: []firstLookFinding{}, Seeds: map[string]firstSeed{}}, nil))
	for _, want := range []string{"Your home: 3 devices.", `"What's in my home?"`, `"List my automations"`,
		"HA NOVA never unlocks a lock or opens a gate or garage door until you confirm it yourself; a plain yes in chat isn't enough."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	for _, never := range []string{"Worth a look", "rooms", "Ask to", "living room"} {
		if strings.Contains(got, never) {
			t.Errorf("unexpected %q:\n%s", never, got)
		}
	}
}

func TestSetupFirstLookSomeSeedsAndCodeCover(t *testing.T) {
	got := renderLook(lookOf(firstLookResult{Seeds: map[string]firstSeed{
		"climate": {EntityID: "climate.hall", Name: "Hall thermostat"},
		"cover":   {EntityID: "cover.blind", Name: "Office blind"},
		// the map falls back to the entity_id when nothing has a name
		"light": {EntityID: "light.unnamed", Name: "light.unnamed"},
	}}, &firstSeed{EntityID: "cover.garage", Name: "Garage door"}))
	for _, want := range []string{`"Make Hall thermostat a bit warmer"`, `"Close Office blind"`,
		"Ask to open Garage door and HA NOVA first asks you to confirm it yourself; a plain yes in chat isn't enough, and nothing happens until you do."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	for _, never := range []string{"Your home", "Turn on", "light.unnamed"} {
		if strings.Contains(got, never) {
			t.Errorf("unexpected %q:\n%s", never, got)
		}
	}
}

func TestSetupFirstLookObservationWording(t *testing.T) {
	for _, c := range []struct {
		f    firstLookFinding
		want string
	}{
		{firstLookFinding{Kind: "battery_low", EntityID: "binary_sensor.remote", Name: "Remote", Value: "low", Count: 1}, "Low battery: Remote is low."},
		{firstLookFinding{Kind: "integration_problem", Domain: "met", State: "setup_retry", Count: 3}, "An integration keeps trying to connect (and 2 more)."},
		{firstLookFinding{Kind: "integration_problem", Domain: "hue", State: "setup_error", Count: 1}, "An integration couldn't start."},
		{firstLookFinding{Kind: "automation_never_triggered", EntityID: "automation.n", Name: "Never", Count: 1}, `The automation "Never" has never run.`},
		{firstLookFinding{Kind: "unavailable_long", EntityID: "sensor.hub", Name: "Hub", Area: "Garage", Value: "12d", Count: 4}, "Hub (Garage) has not reported anything for at least 12 days (and 3 more)."},
		{firstLookFinding{Kind: "unavailable_long", EntityID: "sensor.hub", Name: "Hub", Value: "x", Count: 1}, "Hub has not reported anything for at least a week."},
		{firstLookFinding{Kind: "something_new", EntityID: "sensor.x", Name: "X", Count: 1}, ""},
	} {
		if got := firstLookNote(c.f); got != c.want {
			t.Errorf("%s: got %q, want %q", c.f.Kind, got, c.want)
		}
	}
}

func TestSetupFirstLookPrivacy(t *testing.T) {
	got := renderLook(lookOf(firstLookResult{
		Observations: []firstLookFinding{
			{Kind: "unavailable_long", EntityID: "device_tracker.phone", Name: "Anna phone", Value: "9d", Count: 1},
			{Kind: "battery_low", EntityID: "person.anna", Name: "Anna", Value: "5%", Count: 1},
			{Kind: "integration_problem", Domain: "owner@example.com", State: "setup_error", Count: 1},
		},
		Seeds: map[string]firstSeed{
			"light": {EntityID: "zone.home", Name: "Home zone"},
			"area":  {AreaID: "x", Name: "Bad\x1b[2Jroom\a\u202e\u200b"},
		},
	}, &firstSeed{EntityID: "person.anna", Name: "Anna"}))
	for _, never := range []string{"Anna", "Home zone", "owner@example.com", "\x1b", "\a", "\u202e", "\u200b"} {
		if strings.Contains(got, never) {
			t.Errorf("leaked %q:\n%q", never, got)
		}
	}
	if !strings.Contains(got, `"What's on in Bad[2Jroom?"`) || !strings.Contains(got, "An integration couldn't start.") {
		t.Errorf("expected lines missing:\n%q", got)
	}
}

func TestSetupFirstLookSkipsOnErrorPanicAndTimeout(t *testing.T) {
	original, budget := setupFirstLookRead, setupFirstLookBudget
	t.Cleanup(func() { setupFirstLookRead, setupFirstLookBudget = original, budget })
	setupFirstLookBudget = 20 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	for name, read := range map[string]func(runtimePaths) (setupFirstLook, error){
		"error": func(runtimePaths) (setupFirstLook, error) { return setupFirstLook{}, errors.New("relay down") },
		"panic": func(runtimePaths) (setupFirstLook, error) { panic("bad data") },
		"timeout": func(runtimePaths) (setupFirstLook, error) {
			<-release
			return setupFirstLook{}, nil
		},
	} {
		setupFirstLookRead = read
		out := &bytes.Buffer{}
		start := time.Now()
		if look := loadSetupFirstLook(out, runtimePaths{}); look != nil {
			t.Errorf("%s: got a first look %+v", name, look)
		}
		if out.Len() != 0 {
			t.Errorf("%s: printed %q", name, out.String())
		}
		if time.Since(start) > 3*time.Second {
			t.Errorf("%s: took %s", name, time.Since(start))
		}
	}
}

func TestSetupCompleteBannerShowsFirstLookOnlyWhenLoaded(t *testing.T) {
	without := &bytes.Buffer{}
	renderSetupCompleteBanner(without, []string{"claude"}, nil)
	if !strings.Contains(without.String(), `"Turn off the living room light"`) || strings.Contains(without.String(), "Your home") {
		t.Errorf("banner without a first look changed:\n%s", without.String())
	}
	with := &bytes.Buffer{}
	renderSetupCompleteBanner(with, []string{"claude"}, lookOf(firstLookResult{Home: map[string]int{"areas": 2},
		Seeds: map[string]firstSeed{"light": {EntityID: "light.desk", Name: "Desk"}}}, nil))
	got := with.String()
	for _, want := range []string{"Setup complete!", "Your home: 2 rooms.", "and try asking:", `"Turn on Desk"`, "ha-nova doctor"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "living room light") {
		t.Errorf("generic invented prompt next to the first look:\n%s", got)
	}
	cloud := &bytes.Buffer{}
	renderSetupCloudFallbackReadyBanner(cloud, lookOf(firstLookResult{Home: map[string]int{"areas": 2}}, nil))
	if !strings.Contains(cloud.String(), "Your home: 2 rooms.") || !strings.Contains(cloud.String(), "Try asking:") {
		t.Errorf("cloud banner without the first look:\n%s", cloud.String())
	}
}

func firstLookHome(extra map[string]map[string]any) mapFixture {
	f := smallHome()
	for id, attrs := range extra {
		f.entities = append(f.entities, ent(id, "", "zha", "e_zha", nil))
		f.states = append(f.states, st(id, "closed", attrs))
	}
	return f
}

func readFirstLookFrom(t *testing.T, f mapFixture) *setupFirstLook {
	t.Helper()
	paths, _ := setupVerbRelayTest(t, func(r *http.Request, endpoint string, payload map[string]any) any {
		return f.reply(r, endpoint, payload)
	})
	look := loadSetupFirstLook(&bytes.Buffer{}, paths)
	if look == nil {
		t.Fatal("no first look")
	}
	return look
}

// The real read path in-process: the fake Relay serves the map fixture and
// refuses anything that is not a read (I1 oracle in setupFakeRelay).
func TestSetupFirstLookReadsTheHomeReadOnly(t *testing.T) {
	got := renderLook(readFirstLookFrom(t, firstLookHome(map[string]map[string]any{
		"lock.front":   {"friendly_name": "Front door"},
		"cover.garage": {"friendly_name": "Garage door", "device_class": "garage"},
	})))
	for _, want := range []string{"Your home: 4 rooms, 6 devices, 6 automations.", "Low battery: Remote battery (Bedroom) is low (and 2 more).",
		`"Turn on Kitchen light"`, "Ask to unlock Front door and HA NOVA first asks you to confirm it yourself"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	for _, never := range []string{"Someone", "person.", "owner@example.com", "do-not-leak", "Close Garage door"} {
		if strings.Contains(got, never) {
			t.Errorf("leaked %q:\n%s", never, got)
		}
	}
}

func TestSetupFirstLookCodeSeed(t *testing.T) {
	gate := map[string]map[string]any{
		"cover.blind": {"friendly_name": "Blind", "device_class": "blind"},
		"cover.gate":  {"friendly_name": "Gate", "device_class": "gate"},
		// a keypad lock is refused and handed to the HA UI: never the example
		"lock.keypad": {"friendly_name": "Keypad", "code_format": `^\d{4}$`},
	}
	look := readFirstLookFrom(t, firstLookHome(gate))
	if look.Lock == nil || look.Lock.EntityID != "cover.gate" {
		t.Errorf("code seed %+v, want the gate", look.Lock)
	}
	if look.Seeds["cover"].EntityID != "cover.blind" {
		t.Errorf("cover seed %+v, want the blind (a gate closes at once)", look.Seeds["cover"])
	}
	gate["lock.side"] = map[string]any{"friendly_name": "Side door"}
	if look := readFirstLookFrom(t, firstLookHome(gate)); look.Lock == nil || look.Lock.EntityID != "lock.side" {
		t.Errorf("code seed %+v, want the lock", look.Lock)
	}
}

// The safety example stays setup-only: `ha-nova map --first-look` never
// carries it, so skills never build an everyday prompt from it.
func TestMapFirstLookCarriesNoCodeSeed(t *testing.T) {
	_, env, _ := runMap(t, firstLookHome(map[string]map[string]any{"lock.front": {"friendly_name": "Front door"}}), "--first-look")
	if strings.Contains(string(env.Result), "lock.front") {
		t.Errorf("map first look names the lock: %s", env.Result)
	}
}

func TestMapFirstLookClimateSeedIsNeverAPhone(t *testing.T) {
	f := smallHome()
	f.entities = append(f.entities, ent("sensor.phone_temp", "d_phone", "mobile_app", "e_mobile", nil))
	f.states = append(f.states, st("sensor.phone_temp", "30", map[string]any{"device_class": "temperature", "friendly_name": "Phone temperature"}))
	var states []map[string]any
	for _, s := range f.states {
		if s["entity_id"] != "sensor.temp" {
			states = append(states, s)
		}
	}
	f.states = states
	_, env, _ := runMap(t, f, "--first-look")
	if r := decodeInto[firstLookResult](t, env.Result); r.Seeds["climate"].EntityID != "" {
		t.Errorf("climate seed %+v, want none (a phone is no room sensor)", r.Seeds["climate"])
	}
}
