//go:build darwin

package main

import (
	"crypto/subtle"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	darwinCodeSigningStatusOperation = 0
	darwinCodeDirectoryHashOperation = 5
	darwinCodeDirectoryHashSize      = 20
	darwinCodeSigningValidFlag       = 0x1
	darwinCodeSigningHardFlag        = 0x100
	darwinCodeSigningKillFlag        = 0x200
	darwinCodeSigningRequireLVFlag   = 0x2000
	darwinCodeSigningRuntimeFlag     = 0x10000
	darwinCodeSigningDebuggedFlag    = 0x10000000
)

func platformNativeSecretWorkerParentVerified() bool {
	parentPID := os.Getppid()
	if parentPID <= 1 {
		return false
	}
	if !darwinProcessWorkerEligible(parentPID) ||
		!darwinProcessWorkerEligible(os.Getpid()) {
		return false
	}
	parentHash, err := darwinProcessCodeDirectoryHash(parentPID)
	if err != nil {
		return false
	}
	selfHash, err := darwinProcessCodeDirectoryHash(os.Getpid())
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(parentHash[:], selfHash[:]) == 1
}

func platformCloudRemoteSecureStorageBoundaryAvailable() bool {
	return darwinProcessWorkerEligible(os.Getpid())
}

func darwinProcessWorkerEligible(pid int) bool {
	var status uint32
	_, _, errno := unix.Syscall6(
		unix.SYS_CSOPS,
		uintptr(pid),
		darwinCodeSigningStatusOperation,
		uintptr(unsafe.Pointer(&status)),
		uintptr(unsafe.Sizeof(status)),
		0,
		0,
	)
	runtime.KeepAlive(&status)
	required := uint32(
		darwinCodeSigningValidFlag |
			darwinCodeSigningHardFlag |
			darwinCodeSigningKillFlag |
			darwinCodeSigningRequireLVFlag |
			darwinCodeSigningRuntimeFlag,
	)
	return errno == 0 &&
		status&required == required &&
		status&darwinCodeSigningDebuggedFlag == 0
}

func darwinProcessCodeDirectoryHash(
	pid int,
) ([darwinCodeDirectoryHashSize]byte, error) {
	var hash [darwinCodeDirectoryHashSize]byte
	_, _, errno := unix.Syscall6(
		unix.SYS_CSOPS,
		uintptr(pid),
		darwinCodeDirectoryHashOperation,
		uintptr(unsafe.Pointer(&hash[0])),
		uintptr(len(hash)),
		0,
		0,
	)
	runtime.KeepAlive(&hash)
	if errno != 0 {
		return [darwinCodeDirectoryHashSize]byte{}, errno
	}
	return hash, nil
}

func platformRunNativeSecretWorker(
	request nativeSecretWorkerRequest,
) nativeSecretWorkerResponse {
	response := nativeSecretWorkerResponse{
		SchemaVersion: nativeSecretWorkerSchema,
	}
	if err := validateNativeSecretWorkerRequest(request); err != nil {
		response.ErrorCode = CloudErrSecretStore
		return response
	}
	if request.UI == SecretStoreAllowUI &&
		!platformNativeSecretPromptContextAvailable() {
		response.ErrorCode = CloudErrSecretUIForbidden
		return response
	}
	if err := loadDarwinOAuthSecurity(); err != nil {
		response.ErrorCode = CloudErrSecretStore
		return response
	}
	previous, err := setDarwinOAuthInteraction(request.UI)
	if err != nil {
		response.ErrorCode = CloudErrSecretStore
		return response
	}
	defer darwinOAuthSecurity.setInteraction(previous)

	switch request.Operation {
	case nativeSecretGet:
		value, found, status := darwinOAuthGet(
			request.Service,
			request.Account,
		)
		if status != darwinOAuthSuccess {
			zeroSecretBytes(value)
			response.ErrorCode = darwinOAuthErrorCode(status, request.UI)
			return response
		}
		response.Found = found
		response.Value = value
	case nativeSecretSet:
		status := darwinOAuthSet(
			request.Service,
			request.Account,
			request.Value,
		)
		if status != darwinOAuthSuccess {
			response.ErrorCode = darwinOAuthErrorCode(status, request.UI)
		}
	case nativeSecretDelete:
		status := darwinOAuthDelete(request.Service, request.Account)
		if status != darwinOAuthSuccess &&
			status != darwinOAuthItemNotFound {
			response.ErrorCode = darwinOAuthErrorCode(status, request.UI)
		}
	default:
		response.ErrorCode = CloudErrSecretStore
	}
	return response
}
