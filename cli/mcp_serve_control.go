package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// The bridge's control channel (docs/work/2026-10-24-chat-app-phone-move-
// spec.md): the NOVA App changes a user's chat-app phone only through it,
// on a second listener bound to loopback, with a secret of its own that
// Home Assistant never holds. The bridge is the only writer of
// phone-links.json. A move or a stop asks the phone linked now first; only
// override (a click on the NOVA page, never the App's machine API) replaces
// a phone that can never answer again.

const controlPrefix = "/control/v1/"

var (
	controlAskWait    = 90 * time.Second // each ask of a job: the phone linked now, then the new one
	controlJobPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// controlJob is one change of one user's link: a move or a stop runs once
// in the background, adopt and drop hold it while they write. Its fields
// are under bridge.mu. Jobs live in memory only: after a restart an id
// answers "lost", and a late tap then counts nowhere.
type controlJob struct {
	user     string
	status   string // running, done, declined, timeout, rejected, changed, failed
	step     string // "old" (the phone linked now) or "new": the step running, or at the end the last one
	phone    string // the phone that step asks
	platform string // that phone's (how it shows the buttons)
	to       string // a move: the new phone
	ended    time.Time
}

type controlRequest struct {
	User     string         `json:"user"`
	Action   string         `json:"action"` // adopt, move, stop, drop
	Phone    *chatAppsPhone `json:"phone"`
	Override bool           `json:"override"`
}

// controlAddr: the control listener binds a loopback IP only, so only the
// App in the same container reaches it.
func controlAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
		return errors.New("--control-listen needs a loopback address, e.g. 127.0.0.1:8795")
	}
	return nil
}

// enableControl reads the control secret, which must differ from the /mcp
// one: holding Home Assistant's copy of that one is never enough.
func (b *chatBridge) enableControl(secretFile string) error {
	secret, err := readServeSecret(secretFile, "control secret")
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(secret[:], b.secret[:]) == 1 {
		return errors.New("the control secret must differ from the secret the integration sends")
	}
	b.control, b.controlJobs = secret, map[string]*controlJob{}
	return nil
}

// serveControl: GET phones?user=, POST phone, GET phone/<job>?user=, GET
// activity, GET seen (mcp_serve_activity.go). The log line names the
// method, the path, and the HTTP status only.
func (b *chatBridge) serveControl(w http.ResponseWriter, r *http.Request) {
	start, status := time.Now(), http.StatusOK
	defer func() {
		b.log.Printf("control %s %q http=%d %s", r.Method, r.URL.Path, status, time.Since(start).Round(time.Millisecond))
	}()
	answer := func(code int, body map[string]any) {
		status = code
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	}
	auth := r.Header.Values("Authorization")
	token, bearer := "", false
	if len(auth) == 1 {
		token, bearer = strings.CutPrefix(auth[0], "Bearer ")
	}
	got := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(got[:], b.control[:]) != 1 || !bearer {
		answer(controlError(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized"))
		return
	}
	job, isJob := strings.CutPrefix(r.URL.Path, controlPrefix+"phone/")
	switch {
	case r.URL.Path == controlPrefix+"phones" && r.Method == http.MethodGet:
		answer(b.controlPhones(r.URL.Query().Get("user")))
	case r.URL.Path == controlPrefix+"phone" && r.Method == http.MethodPost:
		var req controlRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil || dec.More() {
			answer(controlError(http.StatusBadRequest, "INVALID_REQUEST", "a JSON object with user, action, and phone or override"))
			return
		}
		answer(b.controlStart(req))
	case isJob && r.Method == http.MethodGet:
		answer(b.controlJobState(job, r.URL.Query().Get("user")))
	case r.URL.Path == controlPrefix+"activity" && r.Method == http.MethodGet:
		answer(b.controlActivity())
	case r.URL.Path == controlPrefix+"seen" && r.Method == http.MethodGet:
		answer(b.controlSeen())
	default:
		answer(controlError(http.StatusNotFound, "NOT_FOUND", "not found"))
	}
}

func controlError(code int, errCode, message string) (int, map[string]any) {
	return code, map[string]any{"error": map[string]any{"code": errCode, "message": message}}
}

// controlLook is one user's phones and link as a job starts (or re-checks).
type controlLook struct {
	s       *relaySession
	home    phoneHome
	prev    *phoneLink // the user's entry as read; written over only while it is unchanged
	present bool
	current phoneSetup
	status  phoneStatus
}

// asUser runs run under mcpVerbLock as a chat call of user would: the
// phone code then reads that user's link from the bridge's store, and
// their level decides which phones confirm for them (phoneAllowed). An
// unreadable policy runs nothing (errSettingsUnread), never as "not allowed".
func (b *chatBridge) asUser(user string, run func()) error {
	policy, code, _ := b.policyFor(user) // before the lock: it may read Home Assistant's users
	switch {
	case code == http.StatusServiceUnavailable:
		return errSettingsUnread
	case policy == nil: // not allowed now: no phone confirms for them
		policy = &chatPolicy{user: user, key: chatUserKey(user), links: b.linksFile(), bridge: b}
	}
	mcpVerbLock.Lock()
	defer mcpVerbLock.Unlock()
	chatCall = policy.reread() // under the lock: the owner's settings as they are now
	defer func() { chatCall = nil }()
	run()
	return nil
}

// look reads user's link and phones with s, or with a new session as the
// bridge in the user's own data directory (s nil; closed on an error).
func (b *chatBridge) look(user string, s *relaySession) (*controlLook, error) {
	var look *controlLook
	var err error
	own := s == nil
	unread := b.asUser(user, func() {
		if own {
			paths := b.paths
			paths.CacheDir = filepath.Join(b.dataDir, "users", chatUserKey(user))
			if err = os.MkdirAll(paths.CacheDir, 0o700); err != nil {
				return
			}
			if s, err = b.session(paths); err != nil {
				return
			}
		}
		byUser, ok := readBridgeLinks(b.linksFile())
		if !ok {
			err = errors.New("phone-links.json is unreadable")
			return
		}
		links := bridgeEntries(byUser, user)
		look = &controlLook{s: s}
		look.prev, look.present = byUser[user]
		if look.home, err = lookupPhones(s, links); err != nil {
			return
		}
		look.current, look.status = look.home.status(links, nil)
		if look.present && (look.prev == nil || look.prev.UserID != user) { // as discoverConfirmer: null, or another user's link, never answers for this one
			look.current, look.status = phoneSetup{owner: look.home.owner, relayUser: look.home.relay, why: whyRemoved}, phoneMissing
		}
	})
	if unread != nil {
		err = unread
	}
	if err != nil {
		if own && s != nil {
			s.close()
		}
		return nil, err
	}
	return look, nil
}

// same: the user's entry is still the one look read.
func (l *controlLook) same(other *controlLook) bool {
	return other.present == l.present && (other.prev == nil) == (l.prev == nil) && (other.prev == nil || *other.prev == *l.prev)
}

// linkState: the user's link for the page; nil: none.
func (l *controlLook) linkState() map[string]any {
	if !l.present {
		return nil
	}
	state, name, device, platform := "ready", "", "", ""
	if l.prev != nil {
		name, device, platform = terminalSafeName(l.prev.Name), l.prev.DeviceID, l.prev.Platform
	}
	switch {
	case l.status == phoneReady:
		platform = l.current.targets[0].platform()
	case l.current.why.replaceable():
		state = "gone"
	default:
		state = "unavailable"
	}
	return map[string]any{"name": name, "device_id": device, "platform": platform, "state": state, "why": controlWhy[l.current.why]}
}

var controlWhy = map[phoneWhy]string{whyNone: "", whyRead: "unread", whyOwnerToken: "owner_token", whyRelayUser: "relay_user",
	whyDemoted: "not_allowed", whyRemoved: "removed", whyShared: "shared_name", whyUnreachable: "unreachable"}

// controlPhones: the phones user may link (their own, at Use the home or
// more, the linked one left out) and their link.
func (b *chatBridge) controlPhones(user string) (int, map[string]any) {
	if !haUserIDPattern.MatchString(user) {
		return controlError(http.StatusBadRequest, "INVALID_USER", "user must be a Home Assistant user id")
	}
	look, err := b.look(user, nil)
	if err != nil {
		return controlError(http.StatusServiceUnavailable, "PHONES_UNREADABLE", "HA NOVA can't read this home's phones right now")
	}
	defer look.s.close()
	if look.home.relay == look.home.owner {
		return controlOwnerToken()
	}
	phones := []map[string]any{}
	for _, t := range look.home.eligible(user) {
		if look.status != phoneReady || t.DeviceID != look.current.targets[0].DeviceID {
			phones = append(phones, map[string]any{"device_id": t.DeviceID, "name": terminalSafeName(t.Name), "platform": t.platform()})
		}
	}
	return http.StatusOK, map[string]any{"phones": phones, "link": look.linkState()}
}

// controlOwnerToken: a Relay signed in as the owner can't tell a tap from
// HA NOVA itself, so no phone is listed or linked (as linkPhone refuses).
func controlOwnerToken() (int, map[string]any) {
	return controlError(http.StatusConflict, "OWNER_TOKEN", "this NOVA App signs in to Home Assistant as the owner, so a tap on a phone can't be told apart from HA NOVA itself")
}

// controlStart checks an action and runs it: adopt, drop, and a stop with
// override at once; a move, or a stop that asks a phone, as a job. Every
// action holds the user's one change at a time, drop included.
func (b *chatBridge) controlStart(req controlRequest) (int, map[string]any) {
	switch {
	case !haUserIDPattern.MatchString(req.User):
		return controlError(http.StatusBadRequest, "INVALID_USER", "user must be a Home Assistant user id")
	case !slices.Contains([]string{"adopt", "move", "stop", "drop"}, req.Action):
		return controlError(http.StatusBadRequest, "INVALID_ACTION", "action must be adopt, move, stop, or drop")
	case (req.Action == "adopt" || req.Action == "move") && (req.Phone == nil || !chatAppsDeviceID.MatchString(req.Phone.DeviceID)):
		return controlError(http.StatusBadRequest, "INVALID_PHONE", "phone needs a device_id")
	}
	id, job := b.reserveJob(req.User)
	if job == nil {
		return controlError(http.StatusConflict, "PHONE_CHANGE_RUNNING", "a phone change for this user is still running")
	}
	var look *controlLook
	started := false
	defer func() {
		if started {
			return
		}
		if look != nil {
			look.s.close()
		}
		b.mu.Lock()
		delete(b.controlJobs, id)
		b.mu.Unlock()
	}()
	policy, code, why := b.policyFor(req.User)
	switch {
	case code == http.StatusServiceUnavailable:
		return controlError(code, "SETTINGS_UNREADABLE", why)
	case req.Action == "drop" && policy != nil:
		return controlError(http.StatusConflict, "USER_STILL_ALLOWED", "only a user no longer allowed in chat apps is dropped without a question")
	case req.Action == "drop":
		byUser, ok := readBridgeLinks(b.linksFile())
		if !ok {
			return controlError(http.StatusServiceUnavailable, "LINKS_UNWRITABLE", "phone-links.json can't be read or written")
		}
		prev, present := byUser[req.User]
		return b.controlWrite(req.User, prev, present, nil, chatRemoved)
	case policy == nil:
		return controlError(http.StatusForbidden, "USER_NOT_ALLOWED", "this Home Assistant user may not use chat apps")
	}
	var err error
	if look, err = b.look(req.User, nil); err != nil {
		return controlError(http.StatusServiceUnavailable, "PHONES_UNREADABLE", "HA NOVA can't read this home's phones right now")
	}
	if look.home.relay == look.home.owner {
		return controlOwnerToken()
	}
	var chosen *phoneTarget
	for _, t := range look.home.eligible(req.User) {
		if req.Action != "stop" && t.DeviceID == req.Phone.DeviceID {
			chosen = &t
		}
	}
	gone := look.status == phoneMissing && look.current.why.replaceable()
	switch {
	case req.Action == "adopt" && look.present:
		return controlError(http.StatusConflict, "PHONE_EXISTS", "this user has a chat-app phone already; a move asks it first")
	case req.Action != "stop" && chosen == nil:
		return controlError(http.StatusConflict, "PHONE_NOT_ELIGIBLE", "that phone isn't one of this user's phones that can confirm")
	case req.Action == "adopt": // the first phone, from a computer link its user's tap made: no question, Home Assistant's own listing
		return b.controlWrite(req.User, nil, false, chatPhoneLink(*chosen), chatPhoneOK)
	case req.Action == "move" && look.status == phoneReady && look.current.targets[0].DeviceID == chosen.DeviceID:
		return controlError(http.StatusConflict, "PHONE_ALREADY_LINKED", "that phone is linked already")
	case req.Action == "stop" && !look.present:
		return controlError(http.StatusConflict, "NO_PHONE", "no phone is linked for this user")
	case look.status == phoneMissing && !gone:
		return controlError(http.StatusConflict, "OLD_PHONE_UNAVAILABLE", "the phone linked now can't be asked right now ("+controlWhy[look.current.why]+")")
	case gone && !req.Override:
		return controlError(http.StatusConflict, "OLD_PHONE_GONE", "the phone linked now can never answer again ("+controlWhy[look.current.why]+"); only the NOVA page replaces it")
	case gone && req.Action == "stop":
		return b.controlWrite(req.User, look.prev, true, nil, chatAllowed)
	}
	first, step := chosen, "new"
	if look.status == phoneReady {
		first, step = &look.current.targets[0], "old"
	}
	b.step(job, step, *first, chosen)
	started = true
	go b.runPhoneJob(job, look, chosen, policy.codes)
	return http.StatusAccepted, map[string]any{"job": id, "status": "running"}
}

// reserveJob holds user's one change at a time before anything is read;
// nil: one is running. Results nobody read go after 10 minutes.
func (b *chatBridge) reserveJob(user string) (string, *controlJob) {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw) // never fails (crypto/rand)
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, job := range b.controlJobs {
		switch {
		case job.status == "running" && job.user == user:
			return "", nil
		case job.status != "running" && time.Since(job.ended) > 10*time.Minute:
			delete(b.controlJobs, id)
		}
	}
	id, job := hex.EncodeToString(raw), &controlJob{user: user, status: "running"}
	b.controlJobs[id] = job
	return id, job
}

// controlJobState: a job of user's; another user's job reads as lost, as
// an unknown id does.
func (b *chatBridge) controlJobState(id, user string) (int, map[string]any) {
	switch {
	case !controlJobPattern.MatchString(id):
		return controlError(http.StatusBadRequest, "INVALID_JOB", "not a job id")
	case !haUserIDPattern.MatchString(user):
		return controlError(http.StatusBadRequest, "INVALID_USER", "user must be a Home Assistant user id")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	job := b.controlJobs[id]
	if job == nil || job.user != user {
		return http.StatusOK, map[string]any{"status": "lost"}
	}
	state := map[string]any{"status": job.status}
	if job.step != "" {
		state["step"], state["phone"], state["platform"] = job.step, job.phone, job.platform
	}
	if job.to != "" {
		state["to"] = job.to
	}
	return http.StatusOK, state
}
