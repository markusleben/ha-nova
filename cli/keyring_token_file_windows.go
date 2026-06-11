//go:build windows

package main

import "os"

func validateRelayAuthTokenFileOwner(_ os.FileInfo) error {
	return nil
}
