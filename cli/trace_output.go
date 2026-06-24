package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func printTraceLatestJSON(out traceLatestOutput) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		printErr("cannot render trace result: %s", err)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

func printTraceListJSON(out traceListOutput) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		printErr("cannot render trace list: %s", err)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}

func printTraceGetJSON(out traceGetOutput) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		printErr("cannot render trace detail: %s", err)
		return
	}
	fmt.Fprintln(os.Stdout, string(data))
}
