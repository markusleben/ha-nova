package main

import (
	"context"
	"net/http"
)

type gatePhoneProgressKey struct{}

type gatePhoneProgress struct {
	started, sent bool
	w             http.ResponseWriter
}

func (p *gatePhoneProgress) accepted() {
	if p.sent {
		return
	}
	p.sent = true
	p.w.Header().Set("Content-Type", "application/json")
	p.w.Header().Set("X-Ha-Nova-Phone-Sent", "1")
	p.w.WriteHeader(http.StatusOK)
	_ = http.NewResponseController(p.w).Flush()
}

func gatePhoneWithProgress(paths runtimePaths, w http.ResponseWriter, r *http.Request, in gatePhoneRequest, ends *gatePhoneEnds) (gatePhoneAnswer, error) {
	progress := &gatePhoneProgress{w: w}
	ctx := context.WithValue(r.Context(), gatePhoneProgressKey{}, progress)
	answer, err := ends.phone(ctx, paths, in)
	if err != nil && progress.sent {
		return gatePhoneAnswer{Answer: "unavailable"}, nil
	}
	if err == nil && !progress.started {
		answer.NotAsked = true
	}
	return answer, err
}
