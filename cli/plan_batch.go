package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Batch plans (docs/work/2026-10-29-batch-confirmation-spec.md): 2 to 20
// automation/script write and delete plans, in the order they run, as one
// plan the user confirms once. plan batch moves the member plans into the
// batch file, so none of them can be applied alone or join another batch;
// apply checks every member again, sends them in order, and stops at the
// first one that does not go through (verb_apply_batch.go).

const planBatchUsage = "ha-nova plan batch --plan <id> --plan <id> [--plan <id>]…"

const planBatchHelp = planBatchUsage + `

Read-only: makes 2 to 20 automation/script plans (plan write --domain
automation|script, plan template, plan delete automation.x|script.x) one
plan, in the order given, with one confirmation for all of them. Each one
is checked again now, and its own plan is used up. Apply checks each one
again right before sending it, sends them in order, and stops at the first
one that does not go through; nothing is rolled back.
Exit: 2 a plan changed since its preview, 3 needs confirmation, 4 refused.`

const batchMaxMembers = 20

const batchSplitNext = "Tell the user which item can't be in this batch and why (the message names its plan). Make the batch again without it; that one keeps its own preview ask and confirmation."

type planIDList []string

func (l *planIDList) String() string { return strings.Join(*l, ",") }

func (l *planIDList) Set(id string) error {
	*l = append(*l, id)
	return nil
}

func runPlanBatch(paths runtimePaths, args []string) int {
	var opts verbOptions
	var ids planIDList
	fs := flag.NewFlagSet("plan batch", flag.ContinueOnError)
	addVerbFlags(fs, &opts)
	fs.Var(&ids, "plan", "a plan id from plan write, plan template, or plan delete (p_ + 12 hex), once per plan, in the order they run")
	positional, err := parseVerbArgs(fs, &opts, args, planBatchHelp)
	if err == nil && (len(positional) != 0 || len(ids) == 0 || slices.ContainsFunc(ids, func(id string) bool { return !planIDPattern.MatchString(id) })) {
		err = errors.New("Usage: " + planBatchUsage)
	}
	if err != nil {
		return verbUsageError("plan", err)
	}
	return runReadVerb(paths, "plan", opts, func(s *relaySession, env *verbEnvelope) error {
		return planBatch(paths, s, env, ids)
	})
}

// planBatch claims every member (the rename apply uses), checks it, re-plans
// it, checks the order, and stores the batch; the member files go away only
// once the batch is saved, and every refusal hands them back untouched.
func planBatch(paths runtimePaths, s *relaySession, env *verbEnvelope, ids []string) error {
	if len(ids) < 2 || len(ids) > batchMaxMembers {
		return refusedWith("BATCH_SIZE", fmt.Sprintf("a batch holds 2 to %d plans, not %d", batchMaxMembers, len(ids)),
			"A single plan is applied on its own. More than 20: make several batches, each with its own confirmation.")
	}
	for i, id := range ids {
		if slices.Index(ids, id) != i {
			return refusedWith("BATCH_DUPLICATE", "plan "+id+" is named twice", "Name each plan once, in the order they run.")
		}
	}
	held := map[string]string{}
	saved := false
	defer func() {
		for open, hold := range held {
			if saved {
				_ = os.Remove(hold)
			} else {
				_ = os.Rename(hold, open)
			}
		}
	}()
	members := []storedPlan{}
	items := map[string]string{}
	for _, id := range ids {
		open := filepath.Join(planDir(paths), id+".json")
		hold := filepath.Join(planDir(paths), fmt.Sprintf("%s.batched-%d", id, os.Getpid()))
		if os.Rename(open, hold) != nil {
			return refusedWith("PLAN_NOT_FOUND", "plan "+id+" is missing, expired, already used, or in another batch; nothing was sent",
				"Plan that item again and show its preview, then make the batch again with its new plan id (the plans of an earlier batch are used up).")
		}
		held[open] = hold
		m, err := readPlanFile(hold)
		if err != nil {
			return refused("PLAN_INVALID", "plan "+id+" cannot be read or is not a valid plan")
		}
		if refusal := batchMemberRefusal(s, m, id); refusal != nil {
			return refusal
		}
		item := m.ConfigAction.Domain + ":" + m.ConfigAction.ConfigID
		if other, twice := items[item]; twice {
			return refusedWith("BATCH_DUPLICATE", fmt.Sprintf("plans %s and %s both change %s.%s", other, id, m.ConfigAction.Domain, m.ConfigAction.ConfigID),
				"A batch changes each automation or script once: plan the final version of that one only, then make the batch again.")
		}
		items[item] = id
		members = append(members, m)
	}
	rp, plan, err := computeBatchPlan(paths, s, env, members)
	if err == nil {
		err = batchOrder(rp)
	}
	if err == nil {
		err = savePlan(paths, plan, s.transport.Credential)
	}
	if saved = err == nil; !saved { // a sealed batch that is refused shows no plan
		env.Plan, env.Tier, env.Reasons, env.Members, env.Consumers, env.Coverage = nil, "", nil, nil, nil, nil
	}
	return err
}

// batchMemberRefusal: why plan id can't join a batch (nil: it can), checked
// as apply checks a plan before its re-plan.
func batchMemberRefusal(s *relaySession, m storedPlan, id string) *verbFailure {
	expires, timeErr := time.Parse(time.RFC3339, m.ExpiresAt)
	switch {
	case m.ID != id || timeErr != nil:
		return refused("PLAN_INVALID", "plan "+id+" cannot be read or is not a valid plan")
	case m.SchemaVersion != planSchemaVersion:
		return refused("PLAN_EXPIRED", "plan "+id+" was made by an earlier HA NOVA version")
	case batchUnsupported(m) != "":
		return refusedWith("BATCH_UNSUPPORTED", "plan "+id+" can't be in a batch: "+batchUnsupported(m), batchSplitNext)
	case m.Server != s.server:
		return refusedWith("SERVER_MISMATCH", fmt.Sprintf("plan %s belongs to server %q", id, m.Server), "A batch holds plans of one server: make it again from this server's plans only.")
	case !verbNow().Before(expires) || expires.After(verbNow().Add(planTTL+time.Minute)):
		return refused("PLAN_EXPIRED", "plan "+id+" expired")
	case !memberIntact(m) || m.Tier == tierTyped.String() && !planSealed(m, s.transport.Credential):
		return refused("PLAN_INVALID", "plan "+id+" was changed after it was made")
	}
	return nil
}

// batchUnsupported: why a plan's kind can't be a member ("" when it can).
// Only automation and script writes and deletes are; the rest keep their
// own confirmation and checks.
func batchUnsupported(m storedPlan) string {
	req, action := m.ConfigRequest, m.ConfigAction
	switch {
	case m.Kind == "batch":
		return "a batch can't hold another batch"
	case m.Kind == "call":
		return "a device call keeps its own confirmation"
	case m.Kind == "raw":
		return "a raw plan is already one plan of 1 to 20 steps"
	case m.Kind == "rooms":
		return "a room change is a plan of its own"
	case m.Kind == "revert" || m.Kind == "restore":
		return "an undo or a restore works on the undo stack or the snapshot store, one at a time"
	case req != nil && req.Domain == "dashboard":
		return "a dashboard change has its own snapshot and check"
	case m.Kind != "write" && m.Kind != "delete", req == nil || action == nil, req.Op != m.Kind || action.Op != m.Kind,
		req.Domain != "automation" && req.Domain != "script", action.Domain != req.Domain || action.ConfigID == "", req.validate() != nil:
		return "it is not an automation or script write or delete plan"
	}
	return ""
}

// validBatchMembers holds for every stored batch: apply and the window,
// phone, and form re-check the file with it before re-planning.
func validBatchMembers(members []storedPlan) error {
	if len(members) < 2 || len(members) > batchMaxMembers {
		return fmt.Errorf("a batch holds 2 to %d plans", batchMaxMembers)
	}
	ids, items := map[string]bool{}, map[string]bool{}
	for _, m := range members {
		item := ""
		if m.ConfigAction != nil {
			item = m.ConfigAction.Domain + ":" + m.ConfigAction.ConfigID
		}
		if why := batchUnsupported(m); why != "" || !memberIntact(m) || ids[m.ID] || items[item] {
			return fmt.Errorf("member %s is not a valid batch member", m.ID)
		}
		ids[m.ID], items[item] = true, true
	}
	return nil
}

// memberIntact: a member's fields still make its hash and its id derives
// from that hash, as apply checks a single plan (verb_apply.go), so the
// list, the ledger, and the ways back never come from an edited field.
func memberIntact(m storedPlan) bool {
	return intact(m) && m.ID == "p_"+planIdentity(strings.TrimPrefix(m.Hash, "sha256:"), m.Nonce)[:12]
}

// batchOrder: a member whose check reads another member's item (its config
// or its entity) must run before it, or it would change as soon as that one
// is sent. Two that read each other (automations on one light) run in the
// given order when the later one reads only the earlier one's config: its
// re-check excuses that verified write and nothing else (excusedByBatch).
// An entity is never excused, so a later one that reads the earlier one's
// entity moves first, and two that read each other's entities can't share
// a batch.
func batchOrder(rp batchReplan) error {
	rows, err := rp.members[0].e.fullRegistry()
	if err != nil {
		return err
	}
	entities := make([][]string, len(rp.members))
	for i, m := range rp.members {
		action := m.fresh.ConfigAction
		switch {
		case action.EntityID != "":
			entities[i] = []string{action.EntityID}
		case action.Domain == "script" && m.op() == "create":
			entities[i] = []string{"script." + action.ConfigID}
		}
		for _, row := range rows {
			if row.Platform == action.Domain && string(row.UniqueID) == action.ConfigID && !slices.Contains(entities[i], row.EntityID) {
				entities[i] = append(entities[i], row.EntityID)
			}
		}
	}
	reads := func(j, i int) (config, entity bool) {
		basis := asJSON(rp.members[j].fresh.Basis)
		configs, _ := basis["configs"].(map[string]any)
		acted, _ := basis["entities"].(map[string]any)
		action := rp.members[i].fresh.ConfigAction
		_, config = configs[action.Domain+":"+action.ConfigID]
		return config, slices.ContainsFunc(entities[i], func(id string) bool { _, read := acted[id]; return read })
	}
	for i := range rp.members {
		for j := i + 1; j < len(rp.members); j++ {
			first, later := rp.members[i].name(), rp.members[j].name()
			laterConfig, laterEntity := reads(j, i)
			firstConfig, firstEntity := reads(i, j)
			switch {
			case !laterConfig && !laterEntity, (firstConfig || firstEntity) && !laterEntity:
				continue
			case firstEntity && laterEntity:
				return refusedWith("BATCH_ORDER", fmt.Sprintf("%s and %s each check the other, so neither can run first", first, later),
					"Take them out of this batch: apply one of them on its own first, then plan the other again (its check then sees the change); the other items can stay one batch.")
			}
			return refusedWith("BATCH_ORDER", fmt.Sprintf("%s checks %s, so it must run first: move %s before %s", later, first, later, first),
				"Make the batch again with the same plan ids in that order.")
		}
	}
	return nil
}
