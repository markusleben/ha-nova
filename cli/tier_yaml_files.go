package main

import (
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

// content reads one file after checking that it and every directory above
// it are plain (no symlink) entries.
func (y *yamlReader) content(file string) (string, error) {
	out, err := y.contents([]string{file})
	if err != nil {
		return "", err
	}
	return out[0], nil
}

func (y *yamlReader) contents(files []string) ([]string, error) {
	for _, file := range files {
		entry, err := y.entry(file, "file")
		if err != nil {
			return nil, err
		}
		y.used[file] = true
		y.files++
		if entry.Size == nil {
			return nil, yamlFail(yamlUnread)
		}
		y.bytes += int(*entry.Size)
	}
	if y.files > yamlMaxFiles || y.bytes > yamlMaxBytes {
		return nil, yamlFail(yamlUnread)
	}
	out := make([]string, len(files))
	errs := make([]error, len(files))
	var wg sync.WaitGroup
	for i, file := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var read struct {
				Content *string `json:"content"`
			}
			if errs[i] = y.call("read_file", file, &read); errs[i] == nil && read.Content == nil {
				errs[i] = yamlFail(yamlUnread)
			} else if errs[i] == nil {
				out[i] = *read.Content
			}
		}()
	}
	wg.Wait()
	var first error
	for _, err := range errs {
		var fail yamlFail
		if err != nil && (first == nil || errors.As(first, &fail) && !errors.As(err, &fail)) {
			first = err // a cut budget stays an error, not a failure code
		}
	}
	return out, first
}

// walk lists a dir include's files as Home Assistant's loader finds them:
// *.yaml, no name starting with ".", no secrets.yaml, recursively. A
// symlinked *.yaml is unknown; a symlinked directory isn't followed.
func (y *yamlReader) walk(dir string) ([]string, error) {
	if dir == "/config" {
		return nil, yamlFail(yamlUnread) // no listing shows /config's own times
	}
	if _, err := y.entry(dir, "dir"); err != nil {
		return nil, err
	}
	list, err := y.list(dir)
	if err != nil {
		return nil, err
	}
	y.used[dir], y.walked[dir] = true, true
	var files, subdirs []string
	for _, e := range list {
		yamlFile, _ := path.Match("*.yaml", e.Name)
		switch {
		case strings.HasPrefix(e.Name, "."):
		case e.Type == "dir":
			subdirs = append(subdirs, path.Join(dir, e.Name))
		case e.Type == "symlink" && yamlFile:
			return nil, yamlFail(yamlUnread)
		case e.Type == "file" && yamlFile && e.Name != "secrets.yaml":
			files = append(files, path.Join(dir, e.Name))
		}
	}
	slices.Sort(files)
	for _, sub := range subdirs {
		more, err := y.walk(sub)
		if err != nil {
			return nil, err
		}
		files = append(files, more...)
	}
	return files, nil
}

// entry: p's row in its directory's listing, of the given type, with every
// directory above it a plain directory.
func (y *yamlReader) entry(p, kind string) (yamlEntry, error) {
	if p == "/config" {
		return yamlEntry{Type: "dir"}, nil
	}
	dir, name := path.Split(p)
	dir = path.Clean(dir)
	if !strings.HasPrefix(p, "/config/") {
		return yamlEntry{}, yamlFail(yamlUnread)
	}
	if _, err := y.entry(dir, "dir"); err != nil {
		return yamlEntry{}, err
	}
	list, err := y.list(dir)
	if err != nil {
		return yamlEntry{}, err
	}
	i := slices.IndexFunc(list, func(e yamlEntry) bool { return e.Name == name })
	if i < 0 || list[i].Type != kind {
		return yamlEntry{}, yamlFail(yamlUnread)
	}
	return list[i], nil
}

func (y *yamlReader) list(dir string) ([]yamlEntry, error) {
	if list, listed := y.dirs[dir]; listed {
		return list, nil
	}
	if len(y.dirs) >= yamlMaxDirs {
		return nil, yamlFail(yamlUnread)
	}
	list, err := y.listing(dir)
	if err != nil {
		return nil, err
	}
	if y.entries += len(list); y.entries > yamlMaxEntries {
		return nil, yamlFail(yamlUnread)
	}
	y.dirs[dir] = list
	return list, nil
}

// listing: one complete list_dir answer.
func (y *yamlReader) listing(dir string) ([]yamlEntry, error) {
	var out struct {
		Entries   *[]yamlEntry `json:"entries"`
		Truncated bool         `json:"truncated"`
	}
	if err := y.call("list_dir", dir, &out); err != nil {
		return nil, err
	}
	if out.Entries == nil || out.Truncated {
		return nil, yamlFail(yamlUnread)
	}
	return *out.Entries, nil
}

func (y *yamlReader) call(action, p string, into any) error {
	if strings.ContainsAny(p, "%\\") || strings.ContainsFunc(p, unicode.IsControl) || p != "/config" && !strings.HasPrefix(p, "/config/") {
		return yamlFail(yamlUnread) // the Relay would decode or refuse it: not the name Home Assistant reads
	}
	body, _ := json.Marshal(map[string]any{"action": action, "path": p})
	data, err := y.s.send("files", body)
	var refused *relayCallError
	switch {
	case errors.As(err, &refused) && refused.code == "FILE_ACCESS_DISABLED":
		return yamlFail(yamlFilesOff)
	case errors.As(err, &refused):
		return yamlFail(yamlUnread)
	case err != nil:
		return err
	case json.Unmarshal(data, into) != nil:
		return yamlFail(yamlUnread)
	}
	return nil
}

// proven: nothing read changed since Home Assistant started, and nothing
// changed while it was read (each directory is listed again). A directory
// on the way to a used file counts too: renaming one keeps its files' ctime.
func (y *yamlReader) proven(start time.Time) error {
	if y.files > yamlMaxFiles {
		return yamlFail(yamlUnread)
	}
	for dir, first := range y.dirs {
		again, err := y.listing(dir)
		if err != nil {
			return err
		}
		for _, e := range first {
			p := path.Join(dir, e.Name)
			_, listed := y.dirs[p]
			i := slices.IndexFunc(again, func(a yamlEntry) bool { return a.Name == e.Name })
			if (y.walked[dir] || y.used[p] || listed) && (i < 0 || !yamlSame(e, again[i])) {
				return yamlFail(yamlChanged)
			}
			changed, fresh := yamlTime(e.Changed)
			modified, known := yamlTime(e.Modified)
			if (y.used[p] || listed) && p != yamlLock && (!fresh || !changed.Before(start) || y.walked[p] && (!known || !modified.Before(start))) {
				return yamlFail(yamlChanged)
			}
		}
		if y.walked[dir] && len(again) != len(first) {
			return yamlFail(yamlChanged)
		}
	}
	return nil
}

func yamlSame(a, b yamlEntry) bool {
	text := func(s *string) string {
		if s == nil {
			return "-"
		}
		return *s
	}
	size := func(n *int64) int64 {
		if n == nil {
			return -1
		}
		return *n
	}
	return a.Type == b.Type && size(a.Size) == size(b.Size) && text(a.Modified) == text(b.Modified) && text(a.Changed) == text(b.Changed)
}

// yamlTime: a plausible timestamp (a filesystem that keeps none reports 0).
func yamlTime(s *string) (time.Time, bool) {
	if s == nil {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339Nano, *s)
	return at, err == nil && at.Year() >= 2000
}
