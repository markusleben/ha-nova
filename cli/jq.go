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

type jqProgram struct {
	code *gojq.Code
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

func compileJQFilter(filter string) (*jqProgram, error) {
	filterBytes, err := normalizeUTF8Bytes([]byte(filter), "jq filter")
	if err != nil {
		return nil, err
	}
	query, err := parseJQFilter(string(filterBytes))
	if err != nil {
		return nil, fmt.Errorf("jq parse error: %w", err)
	}

	code, err := gojq.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("jq compile error: %w", err)
	}
	return &jqProgram{code: code}, nil
}

func applyCompiledJQFilter(program *jqProgram, input []byte, raw bool) (jqResult, error) {
	input, err := strictJSONBytes(input, "jq input")
	if err != nil {
		return jqResult{}, err
	}
	var inputVal interface{}
	if err := json.Unmarshal(input, &inputVal); err != nil {
		return jqResult{}, fmt.Errorf("invalid JSON input: %w", err)
	}

	var out strings.Builder
	var lastVal interface{}
	iter := program.code.Run(inputVal)
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

// applyJQFilter runs a jq filter on input bytes.
func applyJQFilter(filter string, input []byte, raw bool) (jqResult, error) {
	program, err := compileJQFilter(filter)
	if err != nil {
		return jqResult{}, err
	}
	return applyCompiledJQFilter(program, input, raw)
}

func runJQ(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: relay jq [-r] [-e] [-c] [--jq-file <filter-file>] '<filter>'")
		return 1
	}

	raw := false
	exitStatus := false // -e: exit 1 if last output is false or null
	inputFile := ""
	filterFile := ""
	inputFileSet := false
	filterFileSet := false
	remaining := args
	for len(remaining) > 0 && strings.HasPrefix(remaining[0], "-") {
		switch remaining[0] {
		case "--help", "-h":
			fmt.Println("Usage: ha-nova relay jq [-r] [-e] [-c] [--file <input-file>] [--jq-file <filter-file>] '<filter>'")
			fmt.Println("\nFlags:\n  -r\traw string output\n  -e\texit 1 when the last output is false or null\n  -c\tcompact output (accepted as a no-op; output is already compact)\n  --file <input-file>\tread the JSON input from a file instead of stdin\n  --jq-file <filter-file>\tread the jq filter from a file")
			return 0
		case "-r":
			raw = true
		case "-e":
			exitStatus = true
		case "-c":
			// JSON output is already compact; accept jq muscle memory as a no-op.
		case "--file":
			if len(remaining) < 2 {
				fmt.Fprintln(os.Stderr, "missing value for --file")
				return 1
			}
			inputFile = remaining[1]
			inputFileSet = true
			remaining = remaining[1:]
		case "--jq-file":
			if len(remaining) < 2 {
				fmt.Fprintln(os.Stderr, "missing value for --jq-file")
				return 1
			}
			filterFile = remaining[1]
			filterFileSet = true
			remaining = remaining[1:]
		default:
			fmt.Fprintf(os.Stderr, "unknown flag: %s\n", remaining[0])
			return 1
		}
		remaining = remaining[1:]
	}
	if filterFileSet && len(remaining) > 0 {
		fmt.Fprintln(os.Stderr, "use either an inline jq filter or --jq-file, not both")
		return 1
	}
	if !filterFileSet && len(remaining) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: relay jq [-r] [-e] [-c] [--jq-file <filter-file>] '<filter>'")
		return 1
	}
	filter := ""
	if filterFileSet {
		if strings.TrimSpace(filterFile) == "" {
			fmt.Fprintln(os.Stderr, "--jq-file requires a non-empty path")
			return 1
		}
		filterBytes, err := os.ReadFile(filepath.Clean(filterFile))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading jq filter: %s\n", err)
			return 1
		}
		filterBytes, err = normalizeUTF8Bytes(filterBytes, fmt.Sprintf("jq filter file %q", filterFile))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		filter = strings.TrimSpace(string(filterBytes))
		if filter == "" {
			fmt.Fprintf(os.Stderr, "jq filter file %q is empty\n", filterFile)
			return 1
		}
	} else {
		filter = remaining[0]
	}
	program, err := compileJQFilter(filter)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	var input []byte
	if inputFileSet {
		if strings.TrimSpace(inputFile) == "" {
			fmt.Fprintln(os.Stderr, "--file requires a non-empty path")
			return 1
		}
		input, err = os.ReadFile(filepath.Clean(inputFile))
	} else {
		input, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading jq input: %s\n", err)
		return 1
	}
	inputSource := "jq stdin"
	if inputFileSet {
		inputSource = fmt.Sprintf("jq input file %q", inputFile)
	}
	input, err = strictJSONBytes(input, inputSource)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	res, err := applyCompiledJQFilter(program, input, raw)
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
