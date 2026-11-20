package main

import (
	"context"
	"sort"
	"sync"
	"time"
)

// sendPhones sends to every phone in parallel, bounded by the wait: a
// stalled notify service never holds apply past the confirmation deadline.
// It returns the services that accepted it, and a channel closed once
// every send has finished (one may outlive the wait).
func sendPhones(ctx context.Context, s *relaySession, notify func(*relaySession, string, map[string]any) error, targets []phoneTarget, body func(phoneTarget) map[string]any) ([]string, <-chan struct{}) {
	accepted, sending := make(chan string, len(targets)), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(len(targets))
	go func() { wg.Wait(); close(sending) }()
	for _, target := range targets {
		go func() {
			defer wg.Done()
			if notify(s, target.Service, body(target)) == nil {
				accepted <- target.Service
				return
			}
			accepted <- ""
		}()
	}
	var sent []string
	for range targets {
		select {
		case service := <-accepted:
			if service != "" {
				sent = append(sent, service)
			}
		case <-ctx.Done():
			return sent, sending
		}
	}
	sort.Strings(sent)
	return sent, sending
}

// tellPhones sends bodies to every phone the ask reached, in order per
// phone, phones in parallel, for at most until (best effort: a stale ask
// has dead buttons, since its ids died with the wait; a slow Relay never
// delays apply).
func tellPhones(s *relaySession, notify func(*relaySession, string, map[string]any) error, services []string, bodies []map[string]any, until time.Time) <-chan struct{} {
	done, landed := make(chan struct{}, len(services)), make(chan struct{})
	go func() {
		for range services {
			<-done
		}
		close(landed)
	}()
	for _, service := range services {
		go func() {
			for i, body := range bodies {
				if i > 0 && time.Now().After(until) {
					break
				}
				_ = notify(s, service, body)
			}
			done <- struct{}{}
		}()
	}
	select {
	case <-landed:
	case <-time.After(time.Until(until)):
	}
	return landed
}
