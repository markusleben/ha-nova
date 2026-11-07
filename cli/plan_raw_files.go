package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

// A write_file replaces the whole file and the Relay keeps only one .bak; a
// delete_file keeps none. So a plan that overwrites or deletes an existing
// config file needs a backups save of exactly its current content first
// (category yaml, data {path, content}; skills/yaml-config step 3), like a
// scene save (sceneKept). Every writable extension counts: the Relay
// follows a symlink to its target, so a .txt name can be a YAML file. File
// text has no item identity, so nothing is diffed: every change counts. The
// content's hash enters the plan's basis, so an edit since the preview is
// drift. ha-nova relay files keeps its own flow (the skill captures the copy
// and checks drift by hand).

// rawFileChange: the path a files write_file or delete_file changes; "" for
// any other request.
func rawFileChange(endpoint string, payload map[string]any) string {
	file, _ := payload["path"].(string)
	if endpoint != "files" || payload["action"] != "write_file" && payload["action"] != "delete_file" {
		return ""
	}
	return file
}

// readLiveFile: {path, content} of the file, live, as its copy holds it;
// nil when there is no such file.
func readLiveFile(s *relaySession, file string) (map[string]any, error) {
	body, err := json.Marshal(map[string]any{"action": "read_file", "path": file})
	var data json.RawMessage
	if err == nil {
		data, err = s.sendWithin("files", body, relaySessionRequestTimeout) // only reads: the Relay decides on file access
	}
	var call *relayCallError
	if errors.As(err, &call) && call.code == "FILE_NOT_FOUND" {
		return nil, nil
	}
	var read struct {
		Content *string `json:"content"`
	}
	if err == nil {
		err = json.Unmarshal(data, &read)
	}
	if err == nil && read.Content == nil {
		err = errors.New("the Relay answered without the file's content")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": file, "content": *read.Content}, nil
}

// fileBasis: what the basis holds for a file: its content's hash, or
// "absent".
func fileBasis(live map[string]any) string {
	if live == nil {
		return "absent"
	}
	sum := sha256.Sum256([]byte(live["content"].(string)))
	return hex.EncodeToString(sum[:])
}

// fileKept: a write that changes an existing file, or its delete, needs a
// backups save (category yaml) of exactly its live {path, content} earlier
// in the plan, the config check's rollback too (rawYAMLCheck): a .bak may be
// older than the file. Keeps the file's basis. A file the Relay can't read
// is refused (no copy is possible) or an error: nothing changes unseen
// content.
func (n *rawNamer) fileKept(earlier []rawRequest, payload map[string]any) (refusal, err error) {
	file := rawFileChange("files", payload)
	key := path.Clean(file)
	if _, again := n.files[key]; again {
		return rawDrop{message: "a plan changes the file " + file + " once", hint: "Plan one change per file: one write of the whole file, or its delete."}, nil
	}
	live, err := readLiveFile(n.s, file)
	var call *relayCallError
	if errors.As(err, &call) && slices.Contains([]string{"FILE_TOO_LARGE", "FILE_NOT_TEXT", "FILE_NOT_REGULAR", "FILE_IS_DIRECTORY", "FILE_PATH_DENIED", "FILE_PATH_INVALID"}, call.code) {
		return rawDrop{message: fmt.Sprintf("HA NOVA can't read the file %s (%s), so it can't keep a copy of it", file, call.code),
			hint: "Tell the user HA NOVA can't change this file. They can edit or remove it themselves, for example in Home Assistant's File editor app."}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read the file %s, so nothing was planned or sent: %w", file, err)
	}
	if n.files == nil {
		n.files = map[string]any{}
	}
	n.files[key] = fileBasis(live)
	deletes := payload["action"] == "delete_file"
	if live == nil || !deletes && live["content"] == payload["content"] {
		return nil, nil
	}
	removal := fmt.Sprintf("writing the file %s would replace its current content", file)
	if deletes {
		removal = fmt.Sprintf("deleting the file %s would remove its content", file)
	}
	if hidden := hiddenRemoval(live, removal); hidden != nil {
		return hidden, nil
	}
	yamlCopies := slices.DeleteFunc(slices.Clone(earlier), func(item rawRequest) bool { // a restore lists the yaml category
		saved, _ := decodeObject(item.Payload)
		return item.Endpoint == "backups" && saved["action"] == "save" && saved["category"] != "yaml"
	})
	if copySaved(yamlCopies, live) {
		return nil, nil
	}
	type data struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	type save struct {
		Action   string `json:"action"`
		Category string `json:"category"`
		Name     string `json:"name"`
		Data     data   `json:"data"`
	}
	saveItem, _ := json.Marshal(struct {
		Endpoint string `json:"endpoint"`
		Payload  save   `json:"payload"`
	}{"backups", save{"save", "yaml", autoName(key), data{file, "CONTENT"}}})
	return rawDrop{
		message: removal + ", with no copy of it saved first",
		hint: map[bool]string{false: "A write replaces the whole file, and the Relay's .bak keeps only one step.", true: "A delete keeps no .bak."}[deletes] +
			" Plan it with a backups save of the file first (ha-nova plan raw): " +
			strings.Replace(string(saveItem), `"CONTENT"`, "<its content from files read_file, unchanged>", 1) + "; load the yaml-config skill for the flow.",
	}, nil
}

// fileUnchanged runs right before a file's write or delete is sent: the file must still
// be the one the plan's basis holds (sceneUnchanged).
func fileUnchanged(s *relaySession, endpoint string, payload, basis map[string]any) (refusal, err error) {
	file := rawFileChange(endpoint, payload)
	if file == "" {
		return nil, nil
	}
	live, err := readLiveFile(s, file)
	if err != nil {
		return nil, fmt.Errorf("cannot read the file %s again before its write: %w", file, err)
	}
	if files, _ := basis["files"].(map[string]any); files[path.Clean(file)] != fileBasis(live) {
		return fmt.Errorf("the file %s changed since the preview, so it was not changed", file), nil
	}
	return nil, nil
}
