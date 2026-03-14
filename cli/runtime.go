package main

import (
	"fmt"
	"os"
)

func resolveCommand(argv0 string, args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}

	if args[0] == "relay" {
		return "relay", args[1:]
	}

	return args[0], args[1:]
}

func extractRelayPayload(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-d", "--data":
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing value for %s", args[i])
			}
			return args[i+1], nil
		case "--data-file":
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing value for --data-file")
			}
			data, err := os.ReadFile(args[i+1])
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
	}
	return "", nil
}

func extractPayload(args []string) string {
	payload, _ := extractRelayPayload(args)
	return payload
}
