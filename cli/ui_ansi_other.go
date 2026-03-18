//go:build !windows

package main

import (
	"io"
	"os"
)

func writerSupportsANSI(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	return writerSupportsTTY(file)
}
