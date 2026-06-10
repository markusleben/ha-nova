//go:build darwin || linux

package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func validateRelayAuthTokenFileOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%w: cannot inspect owner", errRelayAuthTokenFileInvalid)
	}
	current, err := user.Current()
	if err != nil {
		return fmt.Errorf("%w: cannot determine current user", errRelayAuthTokenFileInvalid)
	}
	uid, err := strconv.ParseUint(current.Uid, 10, 32)
	if err != nil {
		return fmt.Errorf("%w: cannot parse current user", errRelayAuthTokenFileInvalid)
	}
	if stat.Uid != uint32(uid) {
		return fmt.Errorf("%w: file must be owned by the current user", errRelayAuthTokenFileInvalid)
	}
	return nil
}
