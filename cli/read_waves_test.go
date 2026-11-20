package main

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// readWave holds each awaited read until all of them have arrived: reads
// sent one after another wait out the timeout and mark the wave late.
type readWave struct {
	mu      sync.Mutex
	want    map[string]bool
	arrived map[string]bool
	all     chan struct{}
	late    atomic.Bool
}

func newReadWave(keys ...string) *readWave {
	w := &readWave{want: map[string]bool{}, arrived: map[string]bool{}, all: make(chan struct{})}
	for _, key := range keys {
		w.want[key] = true
	}
	return w
}

func (w *readWave) reply(next func(*http.Request, string, map[string]any) any) func(*http.Request, string, map[string]any) any {
	return func(r *http.Request, endpoint string, p map[string]any) any {
		key := fmt.Sprint(p["type"])
		if endpoint == "core" {
			key = fmt.Sprint(p["path"])
		}
		if w.want[key] {
			w.mu.Lock()
			if !w.arrived[key] {
				w.arrived[key] = true
				if len(w.arrived) == len(w.want) {
					close(w.all)
				}
			}
			w.mu.Unlock()
			select {
			case <-w.all:
			case <-time.After(2 * time.Second):
				w.late.Store(true)
			}
		}
		return next(r, endpoint, p)
	}
}

// PERF-04: a search sends its four independent reads in one wave.
func TestFindSendsItsReadsInOneWave(t *testing.T) {
	f := newFakeHA()
	f.ent("light.kitchen", "hue", "on", nil)
	wave := newReadWave("config/area_registry/list", "config/device_registry/list", "/api/states", "config/entity_registry/list_for_display")
	paths, _ := setupFakeRelay(t, wave.reply(f.reply), false)
	exit, env := runVerbForTest(t, func() int { return runFindCommand(paths, []string{"kitchen"}) })
	if exit != 0 || wave.late.Load() || env.Error != nil {
		t.Fatalf("exit %d late %v error %+v", exit, wave.late.Load(), env.Error)
	}
}

// PERF-04: a failed first read stops a search at once; it doesn't wait
// for the slower reads that went out with it.
func TestFindStopsAtItsFirstFailedRead(t *testing.T) {
	f := newFakeHA()
	f.fail["config/area_registry/list"] = true
	paths, _ := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		if p["path"] == "/api/states" {
			time.Sleep(3 * time.Second)
		}
		return f.reply(r, endpoint, p)
	}, false)
	start := time.Now()
	exit, env := runVerbForTest(t, func() int { return runFindCommand(paths, []string{"kitchen"}) })
	if took := time.Since(start); exit == 0 || env.Error == nil || took > 2*time.Second {
		t.Fatalf("exit %d error %+v took %s", exit, env.Error, took)
	}
}

// --match resolves serially: a plan reads only what its own checks read.
func TestPlanMatchDoesNotWarmFindReads(t *testing.T) {
	f := newFakeHA()
	f.ent("light.kitchen", "hue", "off", nil)
	f.fail["config/area_registry/list"] = true
	paths, log := setupFakeRelay(t, f.reply, true)
	h := &writeHarness{t: t, f: f, paths: paths, log: log}
	_, _ = h.run(runPlanCommand, "call", "light.turn_on", "--match", "kitchen")
	for _, wsType := range log.wsTypes() {
		if wsType == "config/device_registry/list" {
			t.Fatalf("plan --match read the device registry after the area registry failed: %v", log.wsTypes())
		}
	}
}

// PERF-08: get --references sends the reads that need no answer first in
// one wave, get's related check included.
func TestGetReferencesSendsItsFirstReadsInOneWave(t *testing.T) {
	wave := newReadWave("config/entity_registry/list", "/api/states", "get_panels", "lovelace/dashboards/list", "energy/get_prefs", "search/related")
	paths, _ := setupVerbRelayTest(t, wave.reply(refHome(t, nil)))
	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"sensor.pixel_7_battery", "--references"}) })
	if exit != 0 || wave.late.Load() {
		t.Fatalf("exit %d late %v env %+v", exit, wave.late.Load(), env.Error)
	}
	if _, hits := referencesOf(t, env); len(hits) == 0 {
		t.Fatal("no hits")
	}
}
