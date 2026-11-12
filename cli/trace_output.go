package main

import (
	"encoding/json"
	"fmt"
)

func printTraceLatestJSON(call *verbCall, out traceLatestOutput) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		call.errorf("cannot render trace result: %s", err)
		return
	}
	fmt.Fprintln(call.stdout(), string(data))
}

func printTraceListJSON(call *verbCall, out traceListOutput) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		call.errorf("cannot render trace list: %s", err)
		return
	}
	fmt.Fprintln(call.stdout(), string(data))
}

func printTraceGetJSON(call *verbCall, out traceGetOutput) {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		call.errorf("cannot render trace detail: %s", err)
		return
	}
	fmt.Fprintln(call.stdout(), string(data))
}
