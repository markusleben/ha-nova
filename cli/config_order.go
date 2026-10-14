package main

import (
	"bytes"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// Home Assistant evaluates these maps top-down (a variable may use the one
// before it; script fields render in order), so their key ORDER is content.
// renderConfigChanges compares maps unordered; this adds the order check.
var orderSemanticKeys = map[string]bool{"variables": true, "trigger_variables": true, "fields": true}

// orderedKeyLists maps the path of every object stored under an
// order-semantic key to its keys in document order.
func orderedKeyLists(raw []byte) map[string]string {
	out := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var walk func(path string, semantic bool) bool
	walk = func(path string, semantic bool) bool {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		switch tok {
		case json.Delim('{'):
			var keys []string
			for dec.More() {
				keyTok, err := dec.Token()
				key, isString := keyTok.(string)
				if err != nil || !isString || !walk(path+"/"+key, orderSemanticKeys[key]) {
					return false
				}
				keys = append(keys, key)
			}
			_, _ = dec.Token()
			if semantic {
				out[path] = strings.Join(keys, ", ")
			}
		case json.Delim('['):
			for i := 0; dec.More(); i++ {
				if !walk(path+"/"+strconv.Itoa(i), false) {
					return false
				}
			}
			_, _ = dec.Token()
		}
		return true
	}
	walk("", false)
	return out
}

// orderChanges renders one diff row per order-semantic map whose key order
// differs between before and after (same keys, other order).
func orderChanges(before, after []byte) []string {
	a, b := orderedKeyLists(before), orderedKeyLists(after)
	var rows []string
	for path, order := range b {
		if previous, ok := a[path]; ok && previous != order && sameKeySet(previous, order) {
			rows = append(rows, tableRow(strings.TrimPrefix(path, "/")+" (order)", previous, order))
		}
	}
	sort.Strings(rows)
	return rows
}

func sameKeySet(a, b string) bool {
	x, y := strings.Split(a, ", "), strings.Split(b, ", ")
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, ",") == strings.Join(y, ",")
}
