package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/itchyny/gojq"
)

// jqResult holds the output text and the last emitted value (for -e flag).
type jqResult struct {
	output    string
	lastValue interface{}
}

// applyJQFilter runs a jq filter on input bytes.
func applyJQFilter(filter string, input []byte, raw bool) (jqResult, error) {
	query, err := gojq.Parse(filter)
	if err != nil {
		return jqResult{}, fmt.Errorf("jq parse error: %w", err)
	}

	code, err := gojq.Compile(query)
	if err != nil {
		return jqResult{}, fmt.Errorf("jq compile error: %w", err)
	}

	var inputVal interface{}
	if err := json.Unmarshal(input, &inputVal); err != nil {
		return jqResult{}, fmt.Errorf("invalid JSON input: %w", err)
	}

	var out strings.Builder
	var lastVal interface{}
	iter := code.Run(inputVal)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			return jqResult{}, err
		}
		lastVal = v
		if raw {
			if s, ok := v.(string); ok {
				fmt.Fprintln(&out, s)
				continue
			}
		}
		b, err := json.Marshal(v)
		if err != nil {
			return jqResult{}, err
		}
		fmt.Fprintln(&out, string(b))
	}
	return jqResult{output: out.String(), lastValue: lastVal}, nil
}

func runJQ(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: relay jq [-r] [-e] '<filter>'")
		os.Exit(1)
	}

	raw := false
	exitStatus := false // -e: exit 1 if last output is false or null
	remaining := args
	for len(remaining) > 0 && strings.HasPrefix(remaining[0], "-") {
		switch remaining[0] {
		case "-r":
			raw = true
		case "-e":
			exitStatus = true
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", remaining[0])
			os.Exit(1)
		}
		remaining = remaining[1:]
	}
	if len(remaining) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: relay jq [-r] [-e] '<filter>'")
		os.Exit(1)
	}
	filter := remaining[0]

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading stdin: %s\n", err)
		os.Exit(1)
	}

	res, err := applyJQFilter(filter, input, raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	fmt.Print(res.output)

	// -e flag: exit 1 if last output value is false or null
	if exitStatus {
		if res.lastValue == nil || res.lastValue == false {
			os.Exit(1)
		}
	}
}
