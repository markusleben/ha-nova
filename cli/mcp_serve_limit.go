package main

import (
	"sync"
	"time"
)

// rateLimiter: `ha-nova serve --rate-limit <n>`, at most n requests per
// minute per Home Assistant user, in a fixed one-minute window; the rest get
// 429 until it is over. Off (0) unless set: the NOVA App runs without it.
// On the hosted demo every visitor signs in as the same user, so they share
// one window (docs/work/2026-10-20-demo-add-to-claude-spec.md).
type rateLimiter struct {
	limit int
	mu    sync.Mutex
	hits  map[string]rateWindow
}

type rateWindow struct {
	start time.Time
	n     int
}

func (l *rateLimiter) allow(user string) bool {
	if l.limit == 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string]rateWindow{}
	}
	w := l.hits[user]
	if time.Since(w.start) >= time.Minute {
		w = rateWindow{start: time.Now()}
	}
	w.n++
	l.hits[user] = w
	return w.n <= l.limit
}
