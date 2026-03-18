package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/itchyny/gojq"
)

// jqResult holds the output text and the last emitted value (for -e flag).
type jqResult struct {
	output    string
	lastValue interface{}
}

func trimWrappedJQFilter(filter string) string {
	trimmed := strings.TrimSpace(filter)
	if len(trimmed) >= 2 {
		if trimmed[0] == '"' && trimmed[0] == trimmed[len(trimmed)-1] {
			var decoded string
			if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
				return decoded
			}
		}
		if (trimmed[0] == '\'' || trimmed[0] == '"') && trimmed[0] == trimmed[len(trimmed)-1] {
			return trimmed[1 : len(trimmed)-1]
		}
	}
	return trimmed
}

func normalizeCommonJQEscapes(filter string) string {
	return strings.ReplaceAll(filter, `\.`, `\\.`)
}

func parseJQFilter(filter string) (*gojq.Query, error) {
	unwrapped := trimWrappedJQFilter(filter)
	candidates := []string{
		unwrapped,
		normalizeCommonJQEscapes(unwrapped),
		filter,
		normalizeCommonJQEscapes(filter),
	}

	var lastErr error
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		query, err := gojq.Parse(candidate)
		if err == nil {
			return query, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// applyJQFilter runs a jq filter on input bytes.
func applyJQFilter(filter string, input []byte, raw bool) (jqResult, error) {
	query, err := parseJQFilter(filter)
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

func runJQ(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: relay jq [-r] [-e] [--jq-file <filter-file>] '<filter>'")
		return 1
	}

	raw := false
	exitStatus := false // -e: exit 1 if last output is false or null
	inputFile := ""
	filterFile := ""
	remaining := args
	for len(remaining) > 0 && strings.HasPrefix(remaining[0], "-") {
		switch remaining[0] {
		case "-r":
			raw = true
		case "-e":
			exitStatus = true
		case "--file":
			if len(remaining) < 2 {
				fmt.Fprintln(os.Stderr, "missing value for --file")
				return 1
			}
			inputFile = remaining[1]
			remaining = remaining[1:]
		case "--jq-file":
			if len(remaining) < 2 {
				fmt.Fprintln(os.Stderr, "missing value for --jq-file")
				return 1
			}
			filterFile = remaining[1]
			remaining = remaining[1:]
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", remaining[0])
			return 1
		}
		remaining = remaining[1:]
	}
	if filterFile != "" && len(remaining) > 0 {
		fmt.Fprintln(os.Stderr, "use either an inline jq filter or --jq-file, not both")
		return 1
	}
	if filterFile == "" && len(remaining) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: relay jq [-r] [-e] [--jq-file <filter-file>] '<filter>'")
		return 1
	}
	filter := ""
	if filterFile != "" {
		filterBytes, err := os.ReadFile(filepath.Clean(filterFile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading jq filter: %s\n", err)
			return 1
		}
		filter = strings.TrimSpace(string(filterBytes))
	} else {
		filter = remaining[0]
	}

	var (
		input []byte
		err   error
	)
	if inputFile != "" {
		input, err = os.ReadFile(filepath.Clean(inputFile))
	} else {
		input, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading jq input: %s\n", err)
		return 1
	}

	res, err := applyJQFilter(filter, input, raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		return 1
	}

	fmt.Print(res.output)

	// -e flag: exit 1 if last output value is false or null
	if exitStatus {
		if res.lastValue == nil || res.lastValue == false {
			return 1
		}
	}
	return 0
}
