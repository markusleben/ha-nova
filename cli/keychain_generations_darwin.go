//go:build darwin

package main

import (
	"bytes"
	"crypto/subtle"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// Each macOS slot keeps up to two generations, "<slot>#1" and "<slot>#2", and
// each value starts with its sequence number and a newline. A write adds the
// next generation under the other name and only then removes the older one,
// so a crash or a failed add leaves the previous value readable, and no write
// ever modifies an item another program could have created.
const darwinKeychainGenerationHeaderMax = 21

type darwinKeychainGeneration struct {
	service string
	seq     uint64
	raw     []byte
	value   []byte
}

func darwinKeychainGenerations(
	slot, account string,
) ([2]darwinKeychainGeneration, darwinOAuthOSStatus) {
	var gens [2]darwinKeychainGeneration
	for i := range gens {
		gens[i].service = slot + "#" + strconv.Itoa(i+1)
		raw, found, status := darwinKeychainFind(gens[i].service, account)
		if status != darwinOAuthSuccess {
			zeroDarwinKeychainGenerations(gens)
			return gens, status
		}
		if !found {
			continue
		}
		gens[i].raw = raw
		head, value, ok := bytes.Cut(raw, []byte{'\n'})
		seq, err := strconv.ParseUint(string(head), 10, 64)
		if !ok || err != nil || seq == 0 || len(value) == 0 {
			zeroDarwinKeychainGenerations(gens)
			return gens, darwinOAuthAuthFailed
		}
		gens[i].seq, gens[i].value = seq, value
	}
	if gens[0].seq != 0 && gens[0].seq == gens[1].seq {
		zeroDarwinKeychainGenerations(gens)
		return gens, darwinOAuthAuthFailed
	}
	return gens, darwinOAuthSuccess
}

// darwinKeychainLockPath is per login user, not per HOME: a test or lane
// that moves HOME still writes the same login Keychain.
var darwinKeychainLockPath = func() (string, error) {
	account, err := user.Current()
	if err != nil || account.HomeDir == "" {
		return "", errors.New("no home directory for the Keychain write lock")
	}
	return filepath.Join(account.HomeDir, "Library", "Caches", "ha-nova", "keychain.lock"), nil
}

// withDarwinKeychainLock serializes access across processes: writers
// (LOCK_EX) could otherwise remove each other's new generation, and a reader
// (LOCK_SH) between a writer's add and cleanup could see neither generation.
func withDarwinKeychainLock(how int, access func() darwinOAuthOSStatus) darwinOAuthOSStatus {
	path, err := darwinKeychainLockPath()
	if err != nil || os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return darwinOAuthNotAvailable
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return darwinOAuthNotAvailable
	}
	defer file.Close()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		err = unix.Flock(int(file.Fd()), how|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) || time.Now().After(deadline) {
			return darwinOAuthNotAvailable
		}
	}
	defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
	return access()
}

func newestDarwinKeychainGeneration(gens [2]darwinKeychainGeneration) int {
	switch {
	case gens[0].seq == 0 && gens[1].seq == 0:
		return -1
	case gens[0].seq > gens[1].seq:
		return 0
	}
	return 1
}

func zeroDarwinKeychainGenerations(gens [2]darwinKeychainGeneration) {
	for _, gen := range gens {
		zeroSecretBytes(gen.raw)
	}
}

func darwinOAuthGet(
	service, account string,
) ([]byte, bool, darwinOAuthOSStatus) {
	slot := darwinKeychainSlot(service, account)
	if slot == "" {
		return nil, false, darwinOAuthNotAvailable
	}
	var value []byte
	status := withDarwinKeychainLock(unix.LOCK_SH, func() darwinOAuthOSStatus {
		gens, status := darwinKeychainGenerations(slot, account)
		if status != darwinOAuthSuccess {
			return status
		}
		defer zeroDarwinKeychainGenerations(gens)
		if newest := newestDarwinKeychainGeneration(gens); newest >= 0 {
			value = append([]byte(nil), gens[newest].value...)
		}
		return darwinOAuthSuccess
	})
	if status != darwinOAuthSuccess {
		zeroSecretBytes(value)
		return nil, false, status
	}
	return value, value != nil, darwinOAuthSuccess
}

func darwinOAuthSet(
	service, account string,
	value []byte,
) darwinOAuthOSStatus {
	slot := darwinKeychainSlot(service, account)
	if slot == "" {
		return darwinOAuthNotAvailable
	}
	return withDarwinKeychainLock(unix.LOCK_EX, func() darwinOAuthOSStatus {
		return writeDarwinKeychainGeneration(slot, account, value)
	})
}

func writeDarwinKeychainGeneration(slot, account string, value []byte) darwinOAuthOSStatus {
	gens, status := darwinKeychainGenerations(slot, account)
	if status != darwinOAuthSuccess {
		return status
	}
	zeroDarwinKeychainGenerations(gens)
	newest := newestDarwinKeychainGeneration(gens)
	target, seq := 0, uint64(1)
	if newest >= 0 {
		target, seq = 1-newest, gens[newest].seq+1
	}
	if gens[target].seq != 0 {
		if status := darwinKeychainRemove(gens[target].service, account); status != darwinOAuthSuccess {
			return status
		}
	}
	payload := append(strconv.AppendUint(nil, seq, 10), '\n')
	payload = append(payload, value...)
	defer zeroSecretBytes(payload)
	if status := darwinKeychainAdd(gens[target].service, account, payload); status != darwinOAuthSuccess {
		return status
	}
	if newest >= 0 {
		// The newer generation wins every read, so a failed removal only
		// leaves the older value behind until the next write or delete.
		_ = darwinKeychainRemove(gens[newest].service, account)
	}
	return darwinOAuthSuccess
}

func darwinOAuthDelete(service, account string) darwinOAuthOSStatus {
	slot := darwinKeychainSlot(service, account)
	if slot == "" {
		return darwinOAuthNotAvailable
	}
	return withDarwinKeychainLock(unix.LOCK_EX, func() darwinOAuthOSStatus {
		return removeDarwinKeychainGenerations(slot, account)
	})
}

// darwinOAuthDeleteExact removes the slot only while its newest value is
// still expected, comparing and removing under one lock so a write landing
// in between is never removed in its place.
func darwinOAuthDeleteExact(service, account string, expected []byte) (darwinOAuthOSStatus, bool) {
	slot := darwinKeychainSlot(service, account)
	if slot == "" {
		return darwinOAuthNotAvailable, false
	}
	conflict := false
	status := withDarwinKeychainLock(unix.LOCK_EX, func() darwinOAuthOSStatus {
		gens, status := darwinKeychainGenerations(slot, account)
		if status != darwinOAuthSuccess {
			return status
		}
		defer zeroDarwinKeychainGenerations(gens)
		newest := newestDarwinKeychainGeneration(gens)
		if newest < 0 {
			return darwinOAuthSuccess
		}
		if subtle.ConstantTimeCompare(gens[newest].value, expected) != 1 {
			conflict = true
			return darwinOAuthSuccess
		}
		if status := removeDarwinKeychainGenerations(slot, account); status != darwinOAuthItemNotFound {
			return status
		}
		return darwinOAuthSuccess
	})
	return status, conflict
}

func removeDarwinKeychainGenerations(slot, account string) darwinOAuthOSStatus {
	result := darwinOAuthItemNotFound
	for i := 1; i <= 2; i++ {
		switch status := darwinKeychainRemove(slot+"#"+strconv.Itoa(i), account); status {
		case darwinOAuthItemNotFound:
		case darwinOAuthSuccess:
			if result == darwinOAuthItemNotFound {
				result = darwinOAuthSuccess
			}
		default:
			return status
		}
	}
	return result
}
