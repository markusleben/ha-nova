package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// ha-nova mcp (stdio) runs ha_apply as a job, as ha-nova serve does
// (mcp_serve_apply.go; #919): an AI app gives up on a tool call after about
// a minute, and a batch in a home with many automations runs for minutes.
// The call waits until stdioApplyWait after it came in, then answers
// applying; calling ha_apply again with that plan_id waits on the same job,
// which never sends anything twice, and its outcome is handed out once
// (kept for planTTL until then): a plan whose apply failed before sending
// can then run again, as without a job. While a job runs, every other call but ha_skill runs nothing
// and answers BUSY. Nothing cancels or repeats a job: a timed-out or
// cancelled call leaves it running, and the server waits for it before it
// exits on end of input, SIGTERM, SIGINT, or SIGHUP (stdioEndAfterChanges);
// a second signal or a SIGKILL ends it at once.

// stdioApplyWait: longer than mcpWindowWait, so a call never answers
// applying while the window still asks; shorter than the common 60 s
// tool-call limit. A variable so tests can shrink it.
var stdioApplyWait = 50 * time.Second

// stdioJob: a call while an apply job runs or holds an outcome: ha_apply of
// that plan waits on it; any other call but ha_skill answers BUSY while one
// runs. nil: the call runs as usual.
func (srv *mcpServer) stdioJob(name string, args map[string]json.RawMessage) map[string]any {
	for old, job := range srv.jobs { // an outcome nobody came back for
		select {
		case <-job.done: // ended is set before done closes
			if time.Since(job.ended) > planTTL {
				delete(srv.jobs, old)
			}
		default:
		}
	}
	id := text(args, "plan_id")
	if job := srv.jobs[id]; name == "ha_apply" && job != nil {
		return srv.stdioAwait(id, job)
	}
	if name == mcpSkillTool.name || srv.runningJob() == "" {
		return nil
	}
	return srv.chatBusy(name, args, chatBusyWhy)
}

// runningJob: the plan id of the apply job that still runs; "" when none.
func (srv *mcpServer) runningJob() string {
	for id, job := range srv.jobs {
		select {
		case <-job.done:
		default:
			return id
		}
	}
	return ""
}

// stdioApply starts ha_apply of a plan this computer stores as a job and
// waits on it (stdioAwait); nil when the plan can't be read, so the verb
// runs as usual and says why. answered and forms are the call's own: a
// form's answer confirms only this run.
func (srv *mcpServer) stdioApply(args map[string]json.RawMessage, argv []string, answered string, forms bool) map[string]any {
	id := text(args, "plan_id")
	if !planIDPattern.MatchString(id) {
		return nil
	}
	plan, err := readPlanFile(filepath.Join(planDir(srv.paths), id+".json"))
	if err != nil {
		return nil
	}
	job := &chatJob{done: make(chan struct{}), lang: confirmLanguage(plan.Lang)}
	if srv.jobs == nil {
		srv.jobs = map[string]*chatJob{}
	}
	srv.jobs[id] = job
	mcpVerbLock.Lock() // free: no job runs, and the loop holds it only inside a call
	go func() {
		gateApproval = &job.asking // one job at a time: every other call answers BUSY
		defer func() {
			gateApproval = nil
			if p := recover(); p != nil { // after runLocked, which has unlocked
				job.result = chatStopped("ha_apply", applyCrashed(fmt.Sprint("internal error: ", p)))
			}
			job.ended = time.Now()
			close(job.done)
		}()
		result := srv.runLocked("ha_apply", answered, forms, func(call *verbCall) int { return applyVerb(call, srv.paths, pinServer("ha_apply", argv, srv.pin)) })
		job.result = withPlanSteps(result, "ha_apply", args)
	}()
	return srv.stdioAwait(id, job)
}

// stdioAwait waits for job until stdioApplyWait after the call came in, then
// hands out its outcome (once), or answers waiting_for_approval while its
// Relay ask waits for the person, else applying.
func (srv *mcpServer) stdioAwait(id string, job *chatJob) map[string]any {
	timer := time.NewTimer(time.Until(srv.since.Add(stdioApplyWait)))
	defer timer.Stop()
	select {
	case <-job.done:
	case <-timer.C:
		select {
		case <-job.done: // ended as the wait did
		default:
			if phase := job.asking.Load(); phase != 0 {
				return waitingForApproval(id, job.lang, phase == gateLinking)
			}
			return applying(id, job.lang, false)
		}
	}
	delete(srv.jobs, id)
	return maps.Clone(job.result)
}

// waitingForApproval: the job's Relay ask waits for the person (link: the
// phone-link ask before it); nothing of this step was sent (gate_client.go).
func waitingForApproval(id, lang string, link bool) map[string]any {
	wait, say := "HA NOVA waits for the user's OK on their linked phone or in Home Assistant (the NOVA page)", confirmT(lang, "say.relay_waiting")
	if link {
		wait, say = "the user's phone first asks whether it gets HA NOVA's confirmations; the change itself is confirmed after that, and a no there stops it", confirmT(lang, "say.relay_link_waiting")
	}
	next := fmt.Sprintf("Nothing of this step was sent yet: %s. Tell the user `say`, then call ha_apply again with plan_id %s and no confirm_code right away, in this same answer: it joins the same run, and nothing is sent twice. You can't act once your answer ends, so never tell the user you will report back on your own; if you end your answer before the outcome, tell them to ask you again once they confirmed. Never ask for a code.", wait, id)
	envelope := map[string]any{"schema_version": 1, "verb": "apply", "status": "waiting_for_approval", "plan": map[string]any{"id": id}, "say": say, "next_step": next}
	text, _ := json.Marshal(envelope)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "structuredContent": envelope, "isError": false}
}

// applyCrashed: apply failed with an internal error, maybe after it sent.
func applyCrashed(why string) *verbFailure {
	return &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN", message: strings.TrimSpace(why),
		next: "Tell the user HA NOVA can't tell whether the change was made: they can check in Home Assistant. Never apply this plan again."}
}

// stdioEndAfterChanges: a SIGTERM, SIGINT, or SIGHUP (one not ignored since
// start) ends ha-nova mcp as before, but only once no verb runs, so an
// apply job that sent finishes first, while its window or phone ask, open
// or still to come, ends unanswered (planAsks); while it waits, new calls answer BUSY. A second
// signal ends it at once, as one did before: a confirmation window's own
// re-sent signal (askWindowUntilSignal, endBySignal) too, and nothing was
// sent then.
func stdioEndAfterChanges() {
	if runtime.GOOS == "windows" {
		return
	}
	signal.Ignore(syscall.SIGPIPE) // an AI app that closed its end gets write errors; a running change still finishes
	signals := make(chan os.Signal, 1)
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP} {
		if !signal.Ignored(sig) {
			signal.Notify(signals, sig)
		}
	}
	end := func(ended os.Signal) {
		signal.Reset()
		if self, err := os.FindProcess(os.Getpid()); err == nil {
			_ = self.Signal(ended)
		}
	}
	go func() {
		ended := <-signals
		go func() { end(<-signals) }()
		endPlanAsks()      // an ask open or still to come sends nothing
		mcpVerbLock.Lock() // held until the end: a running change finishes, and nothing new starts
		end(ended)
	}()
}

// awaitJobs waits for every running apply job: the server never exits in
// the middle of a change.
func (srv *mcpServer) awaitJobs() {
	for _, job := range srv.jobs {
		<-job.done
	}
}
