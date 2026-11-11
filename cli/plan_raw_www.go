package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
)

// Images and videos in /config/www (docs/work/2026-11-10-www-media-delete-spec.md):
// the Relay keeps a copy of each file it deletes there and puts only that
// copy back (restore_file), so a plan asks for no backups save and reads no
// text. What the plan checks before anyone confirms: a media name outside
// community, an App that keeps copies (list_deleted), and for a delete the
// file is listed, small enough, and the copies have room; for a restore its
// copy. The Relay checks all of it again when the request is sent.

var wwwMediaExtensions = []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".mp4"}

// wwwCopyOverhead: what the Relay counts per copy besides its bytes (files-www.ts).
const wwwCopyOverhead = 4096

// wwwFile: the file lies under /config/www by its logical path.
func wwwFile(file string) bool {
	return strings.HasPrefix(path.Clean(file), "/config/www/")
}

type wwwCopies struct {
	Copies []struct {
		Copy string `json:"copy"`
		Path string `json:"path"`
	} `json:"copies"`
	UsedBytes      int64 `json:"used_bytes"`
	LimitBytes     int64 `json:"limit_bytes"`
	FileLimitBytes int64 `json:"file_limit_bytes"`
}

// mediaKept: fileKept for a delete in www or a restore.
func (n *rawNamer) mediaKept(payload map[string]any) (refusal, err error) {
	raw, _ := payload["path"].(string)
	file := path.Clean(raw)
	segments := strings.Split(file, "/") // "", "config", "www", …
	switch {
	case !wwwFile(file):
		return rawDrop{message: "HA NOVA puts back only images and videos it deleted from /config/www, not " + file,
			hint: "List what it keeps with ha-nova relay files and {\"action\":\"list_deleted\"}."}, nil
	case len(segments) > 4 && strings.EqualFold(segments[3], "community"):
		return rawDrop{message: "HA NOVA never changes /config/www/community: HACS keeps its dashboard cards there",
			hint: "Tell the user to remove a card through HACS, never by its files."}, nil
	case !slices.Contains(wwwMediaExtensions, strings.ToLower(path.Ext(file))):
		return rawDrop{message: fmt.Sprintf("in /config/www HA NOVA deletes only images and videos (%s), not %s", strings.Join(wwwMediaExtensions, ", "), file),
			hint: "Tell the user HA NOVA doesn't change other files there: browsers run them. They can remove it themselves, for example in Home Assistant's File editor app."}, nil
	}
	data, err := n.s.sendWithin("files", []byte(`{"action":"list_deleted"}`), relaySessionRequestTimeout)
	var call *relayCallError
	if errors.As(err, &call) && call.code == "VALIDATION_ERROR" {
		return rawDrop{message: "this NOVA App doesn't keep copies of what it deletes in /config/www, so HA NOVA deletes nothing there",
			hint: "Tell the user to update the NOVA App (Settings > Apps > NOVA Relay), then plan again."}, nil
	}
	var copies wwwCopies
	if err == nil {
		err = json.Unmarshal(data, &copies)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the copies the Relay keeps, so nothing was planned or sent: %w", err)
	}
	if payload["action"] == "restore_file" {
		id, _ := payload["copy"].(string)
		for _, kept := range copies.Copies {
			if kept.Copy == id && kept.Path == file {
				return nil, nil
			}
		}
		return rawDrop{message: fmt.Sprintf("HA NOVA keeps no copy %q of %s", id, file),
			hint: "List the copies with ha-nova relay files and {\"action\":\"list_deleted\"}. Copies are kept for 30 days; an older one is gone, though a Home Assistant backup from before the delete still holds the file."}, nil
	}
	return n.mediaRoom(file, copies)
}

// wwwRestores: the restore item of each image or video a done delete took
// out of www, from the copy id the Relay answered.
func wwwRestores(items []rawRequest, result rawApplyResult) []string {
	var restores []string
	for _, out := range result.Items {
		item := items[out.Item-1]
		payload, _ := decodeObject(item.Payload)
		file, _ := payload["path"].(string)
		var kept struct {
			Copy string `json:"copy"`
		}
		if out.Status != "done" || item.Endpoint != "files" || payload["action"] != "delete_file" || !wwwFile(file) || json.Unmarshal(out.Response, &kept) != nil || kept.Copy == "" {
			continue
		}
		restore, _ := json.Marshal(map[string]any{"endpoint": "files", "payload": map[string]any{"action": "restore_file", "path": path.Clean(file), "copy": kept.Copy}})
		restores = append(restores, string(restore))
	}
	return restores
}

// mediaRoom: the file is listed in its folder as a plain file the copies
// have room for. A listing cut at 500 entries can't say: the Relay checks.
func (n *rawNamer) mediaRoom(file string, copies wwwCopies) (refusal, err error) {
	body, _ := json.Marshal(map[string]any{"action": "list_dir", "path": path.Dir(file)})
	data, err := n.s.sendWithin("files", body, relaySessionRequestTimeout)
	var call *relayCallError
	if errors.As(err, &call) && (call.status == http.StatusForbidden || call.status == http.StatusNotFound) {
		return rawDrop{message: fmt.Sprintf("HA NOVA can't list the folder of %s (%s: %s)", file, call.code, call.message),
			hint: "Tell the user why, in plain words; nothing was planned."}, nil
	}
	var listing struct {
		Entries []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Size *int64 `json:"size"`
		} `json:"entries"`
		Truncated bool `json:"truncated"`
	}
	if err == nil {
		err = json.Unmarshal(data, &listing)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list the folder of %s, so nothing was planned or sent: %w", file, err)
	}
	for _, entry := range listing.Entries {
		if entry.Name != path.Base(file) {
			continue
		}
		switch {
		case entry.Type != "file" || entry.Size == nil:
			return rawDrop{message: file + " is not a plain file, so HA NOVA doesn't delete it", hint: "HA NOVA deletes single images and videos, never folders or links."}, nil
		case *entry.Size > copies.FileLimitBytes:
			return rawDrop{message: fmt.Sprintf("%s is larger than the %d MiB HA NOVA keeps a copy of, so it doesn't delete it", file, copies.FileLimitBytes>>20),
				hint: "Tell the user they can delete it themselves, for example in Home Assistant's File editor app; it can't be undone there."}, nil
		case copies.UsedBytes+n.wwwPlanned+*entry.Size+wwwCopyOverhead > copies.LimitBytes:
			return rawDrop{message: fmt.Sprintf("the copies HA NOVA keeps of deleted files are full (%d of %d MiB), so it deletes nothing more for now", copies.UsedBytes>>20, copies.LimitBytes>>20),
				hint: "Tell the user: copies expire 30 days after their delete (list_deleted shows when), or one can be put back first; or they delete the file themselves in Home Assistant's File editor app, without undo. Fewer files in one plan may fit."}, nil
		}
		n.wwwPlanned += *entry.Size + wwwCopyOverhead
		return nil, nil
	}
	if listing.Truncated {
		return nil, nil
	}
	return rawDrop{message: "there is no file " + file, hint: "List its folder again (ha-nova relay files, list_dir) and use the exact name."}, nil
}
