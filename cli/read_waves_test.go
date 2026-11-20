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

// PERF-04: planning a call reads the compact registry and the service list
// in one wave.
func TestPlanCallReadsRegistryAndServicesInOneWave(t *testing.T) {
	f := newFakeHA()
	f.ent("light.kitchen", "hue", "off", nil)
	wave := newReadWave("config/entity_registry/list_for_display", "/api/services")
	paths, log := setupFakeRelay(t, wave.reply(f.reply), true)
	h := &writeHarness{t: t, f: f, paths: paths, log: log}
	if env := h.plan("light.turn_on", "light.kitchen"); wave.late.Load() || env.Plan == nil {
		t.Fatalf("late %v plan %+v", wave.late.Load(), env.Plan)
	}
}
