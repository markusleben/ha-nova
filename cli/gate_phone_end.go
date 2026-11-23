package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
)

var gatePhoneRef = regexp.MustCompile(`^[a-f0-9]{64}$`)

// gatePhonePanel: NOVA's panel as the App names it (nova-approve.ts novaPanel).
var gatePhonePanel = regexp.MustCompile(`^/app/[a-z0-9_]{1,64}$`)

type gateWindowNotice struct {
	AskRef   string    `json:"ask_ref"`
	Seq      uint64    `json:"seq"`
	Outcome  string    `json:"outcome"`
	Link     phoneLink `json:"link"`
	Lang     string    `json:"lang"`
	Title    string    `json:"title"`
	Computer struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"computer"`
}

func (n gateWindowNotice) valid() bool {
	_, language := confirmCatalogs[n.Lang]
	return gatePhoneRef.MatchString(n.AskRef) && n.Seq > 0 && n.Outcome == "window" && language && n.Title != "" && len(n.Title) <= 1024 &&
		len(n.Computer.ID) > 0 && len(n.Computer.ID) <= 256 && len(n.Computer.Name) > 0 && len(n.Computer.Name) <= 256 &&
		phoneServicePattern.MatchString(n.Link.Service) && n.Link.DeviceID != "" && n.Link.UserID != ""
}

type gatePhoneEnd struct {
	at         time.Time
	registered bool
	claimed    bool
	notice     *gateWindowNotice
	tag        string
	original   phoneSetup
	done       chan struct{}
}

type gatePhoneEnds struct {
	mu        sync.Mutex
	entries   map[string]*gatePhoneEnd
	computers map[string]*gateWindowQueue
	send      func(runtimePaths, gateWindowNotice, string, bool, phoneSetup)
}

type gateWindowQueue struct {
	mu     sync.Mutex
	latest string
	seq    uint64
	at     time.Time
}

type gatePhoneEndKey struct{}
type gatePhoneRun struct {
	ends     *gatePhoneEnds
	ref      string
	detached bool
}

func (e *gatePhoneEnds) entry(ref string) *gatePhoneEnd {
	for key, end := range e.entries {
		if time.Since(end.at) > gatePhoneMaxWait+time.Minute {
			delete(e.entries, key)
		}
	}
	for key, queue := range e.computers {
		if time.Since(queue.at) > gatePhoneMaxWait+time.Minute {
			delete(e.computers, key)
		}
	}
	if end := e.entries[ref]; end != nil {
		return end
	}
	if len(e.entries) >= 1024 {
		return nil
	}
	end := &gatePhoneEnd{at: time.Now()}
	e.entries[ref] = end
	return end
}

func (e *gatePhoneEnds) phone(ctx context.Context, paths runtimePaths, in gatePhoneRequest) (gatePhoneAnswer, error) {
	if in.AskRef == "" {
		return gatePhone(ctx, paths, in)
	}
	e.mu.Lock()
	end := e.entry(in.AskRef)
	if end == nil {
		e.mu.Unlock()
		return gatePhoneAnswer{}, errors.New("too many phone asks are being tracked")
	}
	if end.registered || end.notice != nil {
		e.mu.Unlock()
		return gatePhoneAnswer{Answer: "none"}, nil
	}
	end.registered = true
	e.mu.Unlock()
	defer func() {
		notice, done := e.claim(in.AskRef, "", phoneSetup{})
		done()
		if notice != nil {
			go e.emit(paths, *notice, "")
		}
	}()
	answer, err := gatePhone(context.WithValue(ctx, gatePhoneEndKey{}, &gatePhoneRun{ends: e, ref: in.AskRef}), paths, in)
	e.mu.Lock()
	windowWon := end.notice != nil
	e.mu.Unlock()
	if windowWon {
		return gatePhoneAnswer{Answer: "none"}, nil
	}
	return answer, err
}

func (e *gatePhoneEnds) claim(ref, tag string, original phoneSetup) (*gateWindowNotice, func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	end := e.entries[ref]
	if end == nil || end.claimed {
		return nil, func() {}
	}
	end.claimed, end.tag = true, tag
	original.targets = append([]phoneTarget(nil), original.targets...)
	end.original = original
	if end.notice != nil {
		return end.notice, func() {}
	}
	end.done = make(chan struct{})
	return nil, func() { close(end.done) }
}

func (e *gatePhoneEnds) handler(paths runtimePaths, w http.ResponseWriter, r *http.Request) {
	var in gateWindowNotice
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || !in.valid() || dec.Decode(new(any)) != io.EOF {
		http.Error(w, "the request must identify a window-approved ask and its linked phone", http.StatusBadRequest)
		return
	}
	e.mu.Lock()
	end := e.entry(in.AskRef)
	if end == nil {
		e.mu.Unlock()
		http.Error(w, "too many phone asks are being tracked", http.StatusServiceUnavailable)
		return
	}
	if end.notice != nil {
		e.mu.Unlock()
	} else {
		end.notice = &in
		if e.computers == nil {
			e.computers = map[string]*gateWindowQueue{}
		}
		queue := e.computers[in.Computer.ID]
		if queue == nil {
			queue = &gateWindowQueue{}
			e.computers[in.Computer.ID] = queue
		}
		if in.Seq > queue.seq {
			queue.latest, queue.seq = in.AskRef, in.Seq
		}
		queue.at = time.Now()
		tag, done := end.tag, end.done
		send := !end.registered || end.claimed
		e.mu.Unlock()
		if send {
			go func() {
				if done != nil {
					<-done
				}
				e.emit(paths, in, tag)
			}()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"recorded": true})
}

func (e *gatePhoneEnds) emit(paths runtimePaths, notice gateWindowNotice, tag string) {
	e.mu.Lock()
	queue := e.computers[notice.Computer.ID]
	e.mu.Unlock()
	if queue == nil {
		return
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	e.mu.Lock()
	latest := e.computers[notice.Computer.ID] == queue && queue.latest == notice.AskRef
	original := phoneSetup{}
	if end := e.entries[notice.AskRef]; end != nil {
		original = end.original
	}
	e.mu.Unlock()
	if latest || tag != "" {
		e.send(paths, notice, tag, latest, original)
	}
}
