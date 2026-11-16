//go:build darwin

package main

import (
	"bytes"
	"strconv"
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
	gens, status := darwinKeychainGenerations(slot, account)
	if status != darwinOAuthSuccess {
		return nil, false, status
	}
	defer zeroDarwinKeychainGenerations(gens)
	newest := newestDarwinKeychainGeneration(gens)
	if newest < 0 {
		return nil, false, darwinOAuthSuccess
	}
	return append([]byte(nil), gens[newest].value...), true, darwinOAuthSuccess
}

func darwinOAuthSet(
	service, account string,
	value []byte,
) darwinOAuthOSStatus {
	slot := darwinKeychainSlot(service, account)
	if slot == "" {
		return darwinOAuthNotAvailable
	}
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
