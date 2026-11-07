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

// A YAML write_file replaces the whole file, and the Relay keeps only one
// .bak, so a plan that overwrites an existing YAML file needs a backups save
// of exactly its current content first (category yaml, data {path,
// content}; skills/yaml-config step 3), like a scene save (sceneKept). YAML
// text has no item identity, so nothing is diffed: every overwrite that
// changes the content counts. The content's hash enters the plan's basis,
// so an edit since the preview is drift. ha-nova relay files keeps its own
// flow (the skill captures the copy and checks drift by hand).

// rawYAMLWrite: the path a files write_file of a YAML file writes; "" for
// any other request.
func rawYAMLWrite(endpoint string, payload map[string]any) string {
	file, _ := payload["path"].(string)
	ext := strings.ToLower(path.Ext(path.Clean(file)))
	if endpoint != "files" || payload["action"] != "write_file" || ext != ".yaml" && ext != ".yml" {
		return ""
	}
	return file
}

// readYAMLFile: {path, content} of the file, live, as its copy holds it;
// nil when there is no such file.
func readYAMLFile(s *relaySession, file string) (map[string]any, error) {
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

// fileKept: a YAML write that changes an existing file needs a backups save
// (category yaml) of exactly its live {path, content} earlier in the plan,
// the config check's rollback too (rawYAMLCheck): a .bak may be older than
// the file. Keeps the file's
// basis. A file that can't be read is an error: nothing is written over
// unseen content.
func (n *rawNamer) fileKept(earlier []rawRequest, payload map[string]any) (refusal, err error) {
	file := rawYAMLWrite("files", payload)
	key := path.Clean(file)
	if _, again := n.files[key]; again {
		return rawDrop{message: "a plan writes the file " + file + " once", hint: "Put every change to the file into one write of the whole file."}, nil
	}
	live, err := readYAMLFile(n.s, file)
	if err != nil {
		return nil, fmt.Errorf("cannot read the file %s, so nothing was planned or sent: %w", file, err)
	}
	if n.files == nil {
		n.files = map[string]any{}
	}
	n.files[key] = fileBasis(live)
	if live == nil || live["content"] == payload["content"] {
		return nil, nil
	}
	removal := fmt.Sprintf("writing the file %s would replace its current content", file)
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
	return rawDrop{
		message: removal + ", with no copy of it saved first",
		hint: fmt.Sprintf(`A write replaces the whole file, and the Relay's .bak keeps only one step. Plan it with a backups save of the file first (ha-nova plan raw): {"endpoint":"backups","payload":{"action":"save","category":"yaml","name":%s,"data":{"path":%s,"content":<its content from files read_file, unchanged>}}}; load the yaml-config skill for the flow.`,
			jsonText(autoName(key)), jsonText(file)),
	}, nil
}

// fileUnchanged runs right before a YAML write is sent: the file must still
// be the one the plan's basis holds (sceneUnchanged).
func fileUnchanged(s *relaySession, endpoint string, payload, basis map[string]any) (refusal, err error) {
	file := rawYAMLWrite(endpoint, payload)
	if file == "" {
		return nil, nil
	}
	live, err := readYAMLFile(s, file)
	if err != nil {
		return nil, fmt.Errorf("cannot read the file %s again before its write: %w", file, err)
	}
	if files, _ := basis["files"].(map[string]any); files[path.Clean(file)] != fileBasis(live) {
		return fmt.Errorf("the file %s changed since the preview, so it was not written", file), nil
	}
	return nil, nil
}
