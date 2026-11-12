package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The update advisor's breaking-change scan (update-advisor spec → Breaking
// changes): the "Backward-incompatible changes" section of each monthly Home
// Assistant release post, read from the website's source repository and
// matched to this home's integrations. Release text is data: every title and
// text is defanged, and only a completed scan is ever reported as checked.

var releaseNotesBase = "https://raw.githubusercontent.com/home-assistant/home-assistant.io/current/source"

const (
	releaseNotesMaxMonths  = 12
	releaseNotesMaxBytes   = 1 << 20
	releaseNotesMaxDomains = 10
)

var releaseNotesClient = &http.Client{Timeout: 10 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

var (
	releasePostLink = regexp.MustCompile(`\(/blog/(\d{4})/(\d{2})/(\d{2})/(release-[0-9a-z-]+)/\)`)
	detailsBlock    = regexp.MustCompile(`(?s)\{% details "([^"]*)" %\}(.*?)\{% enddetails %\}`)
	integrationRef  = regexp.MustCompile(`(?m)^\[[^\]]*\]: /integrations/([a-z0-9_]+)/?\s*$`)
	referenceLine   = regexp.MustCompile(`(?m)^\[[^\]]+\]: .*$`)
	creditLine      = regexp.MustCompile(`(?m)^\(\[@.*$`)
	liquidTerm      = regexp.MustCompile(`\{% term "?([^"%]+?)"?\s*%\}`)
	liquidTag       = regexp.MustCompile(`\{%.*?%\}`)
	markdownLink    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

type releaseChange struct {
	Release     string   `json:"release"`
	Title       string   `json:"title"`
	Domains     []string `json:"domains,omitempty"`
	Text        string   `json:"text"`
	Link        string   `json:"link"`
	Automations []string `json:"automations,omitempty"`
	Scripts     []string `json:"scripts,omitempty"`
}

type releaseMonth struct {
	release, link string
	changes       []releaseChange
	err           error
}

// scanBreaking: every monthly release after the installed Core up to the one
// offered, the newest releaseNotesMaxMonths of them.
func (a *updateAdvisor) scanBreaking() map[string]any {
	out := map[string]any{"source": "Home Assistant release notes (home-assistant.io), section Backward-incompatible changes"}
	if a.coreTo == a.coreFrom {
		out["status"] = "no_core_update"
		return out
	}
	out["repairs_breaking"] = a.repairsBreaking()
	months := releaseMonths(a.coreFrom, a.coreTo)
	if len(months) == 0 {
		out["status"] = "not_scanned" // a patch release: no such section
		return out
	}
	missing := []map[string]string{}
	if len(months) > releaseNotesMaxMonths {
		for _, m := range months[:len(months)-releaseNotesMaxMonths] {
			missing = append(missing, map[string]string{"release": m, "reason": "more than 12 releases: only the newest 12 are scanned"})
		}
		months = months[len(months)-releaseNotesMaxMonths:]
	}
	read := make([]releaseMonth, len(months))
	var wg sync.WaitGroup
	for i, m := range months {
		wg.Add(1)
		go func() { defer wg.Done(); read[i] = readReleaseMonth(m) }()
	}
	wg.Wait()
	matched, general, other, scanned := []releaseChange{}, []releaseChange{}, 0, []string{}
	for _, m := range read {
		if m.err != nil {
			missing = append(missing, map[string]string{"release": m.release, "reason": m.err.Error()})
			continue
		}
		scanned = append(scanned, m.release)
		for _, c := range m.changes {
			switch {
			case len(c.Domains) == 0:
				general = append(general, c)
			case slices.ContainsFunc(c.Domains, func(d string) bool { return a.domains[d] }):
				matched = append(matched, c)
			default:
				other++
			}
		}
	}
	a.affectedBy(matched)
	out["scanned"], out["matched"], out["general"], out["other_integrations"] = scanned, matched, general, other
	switch {
	case len(scanned) == 0:
		out["status"], out["missing"] = "unavailable", missing
	case len(missing) > 0:
		out["status"], out["missing"] = "incomplete", missing
	default:
		out["status"] = "checked"
	}
	if out["status"] != "checked" {
		a.gap("breaking_changes", fmt.Sprintf("%d of %d releases not read", len(missing), len(missing)+len(scanned)))
	}
	return out
}

// releaseMonths: the monthly releases after from's, up to to's ("2026.9").
func releaseMonths(from, to string) []string {
	f, okF := haVersion(from)
	t, okT := haVersion(to)
	if !okF || !okT {
		return nil
	}
	var out []string
	for i := f[0]*12 + f[1]; i < t[0]*12+t[1]; i++ { // months stored 1-12
		y, m := (i+1)/12, (i+1)%12
		if m == 0 {
			y, m = y-1, 12
		}
		out = append(out, strconv.Itoa(y)+"."+strconv.Itoa(m))
	}
	return out
}

// readReleaseMonth: the changelog page's header links the release post; the
// post's Backward-incompatible changes section holds one details block per
// change, integration changes with a /integrations/<domain>/ reference.
func readReleaseMonth(release string) releaseMonth {
	out := releaseMonth{release: release}
	header, err := fetchReleaseNotes("changelogs/core-"+release+".markdown", 1024)
	if err != nil {
		out.err = err
		return out
	}
	post := releasePostLink.FindStringSubmatch(header)
	if post == nil {
		out.err = errors.New("the changelog links no release post")
		return out
	}
	out.link = fmt.Sprintf("https://www.home-assistant.io/blog/%s/%s/%s/%s/#backward-incompatible-changes", post[1], post[2], post[3], post[4])
	text, err := fetchReleaseNotes(fmt.Sprintf("_posts/%s-%s-%s-%s.markdown", post[1], post[2], post[3], post[4]), releaseNotesMaxBytes)
	if err != nil {
		out.err = err
		return out
	}
	start := strings.Index(text, "\n## Backward-incompatible changes")
	if start < 0 {
		out.err = errors.New("the release post has no Backward-incompatible changes section")
		return out
	}
	section := text[start+1:]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	for _, block := range detailsBlock.FindAllStringSubmatch(section, -1) {
		c := releaseChange{Release: release, Title: sayTextUpTo(block[1], 80, false), Link: out.link}
		for _, ref := range integrationRef.FindAllStringSubmatch(block[2], -1) {
			if !slices.Contains(c.Domains, ref[1]) {
				c.Domains = append(c.Domains, ref[1])
			}
		}
		body := creditLine.ReplaceAllString(referenceLine.ReplaceAllString(strings.ReplaceAll(block[2], " > ", " → "), ""), "")
		body = markdownLink.ReplaceAllString(liquidTag.ReplaceAllString(liquidTerm.ReplaceAllString(body, "$1"), ""), "$1")
		c.Text = sayTextUpTo(body, 600, false)
		out.changes = append(out.changes, c)
	}
	return out
}

// fetchReleaseNotes reads one file of the release notes source: 200 (or 206
// for a ranged header read), at most limit bytes; no redirects.
func fetchReleaseNotes(path string, limit int64) (string, error) {
	req, err := http.NewRequest(http.MethodGet, releaseNotesBase+"/"+path, nil)
	if err != nil {
		return "", err
	}
	ranged := limit < releaseNotesMaxBytes
	if ranged {
		req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", limit-1))
	}
	resp, err := releaseNotesClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("the release notes could not be read: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", errors.New("the release notes are not published (yet)")
	case resp.StatusCode != http.StatusOK && !(ranged && resp.StatusCode == http.StatusPartialContent):
		return "", fmt.Errorf("the release notes answered HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	switch {
	case err != nil:
		return "", fmt.Errorf("the release notes could not be read: %w", err)
	case int64(len(body)) > limit && !ranged:
		return "", errors.New("the release post is larger than 1 MB")
	}
	return string(body[:min(int64(len(body)), limit)]), nil
}

// affectedBy names the automations and scripts that use a matched change's
// integrations (search/related), for at most releaseNotesMaxDomains of them.
func (a *updateAdvisor) affectedBy(changes []releaseChange) {
	var domains []string
	for _, c := range changes {
		for _, d := range c.Domains {
			if a.domains[d] && !slices.Contains(domains, d) {
				domains = append(domains, d)
			}
		}
	}
	sort.Strings(domains)
	if len(domains) > releaseNotesMaxDomains {
		a.gap("affected_automations", fmt.Sprintf("only the first %d of %d integrations", releaseNotesMaxDomains, len(domains)))
		domains = domains[:releaseNotesMaxDomains]
	}
	related := map[string]map[string][]string{}
	for _, d := range domains {
		var r map[string][]string
		if err := a.s.wsDecode(map[string]any{"type": "search/related", "item_type": "integration", "item_id": d}, &r, "search/related"); err != nil {
			a.gap("affected_automations", d+": "+err.Error())
			continue
		}
		related[d] = r
	}
	for i := range changes {
		c := &changes[i]
		for _, d := range c.Domains {
			for _, id := range related[d]["automation"] {
				c.Automations = appendName(c.Automations, a.x.name(id))
			}
			for _, id := range related[d]["script"] {
				c.Scripts = appendName(c.Scripts, a.x.name(id))
			}
		}
	}
}

func appendName(names []string, name string) []string {
	if slices.Contains(names, name) {
		return names
	}
	return append(names, name)
}

// repairsBreaking: Home Assistant's own deprecation repairs for this home
// that break at or before the Core offered (offline, deterministic).
func (a *updateAdvisor) repairsBreaking() []map[string]string {
	out := []map[string]string{}
	issues, err := a.repairs()
	if err != nil {
		return out // checkRepairs lists the gap
	}
	for _, issue := range issues {
		if later, ok := haVersionLess(a.coreTo, issue.BreaksIn); ok && !later {
			out = append(out, map[string]string{"integration": issue.Domain, "issue_id": issue.IssueID, "breaks_in": issue.BreaksIn})
		}
	}
	return out
}
