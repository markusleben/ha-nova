//go:build windows

package main

import (
	"crypto/subtle"
	"errors"
	"os"

	"github.com/zalando/go-keyring"
	"golang.org/x/sys/windows"
)

func platformCloudRemoteSecureStorageBoundaryAvailable() bool {
	return true
}

func platformNativeSecretWorkerParentVerified() bool {
	parentPID := os.Getppid()
	if parentPID <= 0 {
		return false
	}
	process, err := windows.OpenProcess(
		windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(parentPID),
	)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(process)

	parentPath := make([]uint16, 32768)
	size := uint32(len(parentPath))
	if err := windows.QueryFullProcessImageName(
		process,
		0,
		&parentPath[0],
		&size,
	); err != nil || size == 0 || int(size) > len(parentPath) {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	parentInfo, err := os.Stat(windows.UTF16ToString(parentPath[:size]))
	if err != nil {
		return false
	}
	selfInfo, err := os.Stat(executable)
	return err == nil && os.SameFile(parentInfo, selfInfo)
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
	switch request.Operation {
	case nativeSecretGet:
		value, err := keyring.Get(request.Service, request.Account)
		if errors.Is(err, keyring.ErrNotFound) {
			return response
		}
		if err != nil {
			response.ErrorCode = nativeSecretWorkerErrorCode(err)
			return response
		}
		response.Found = true
		response.Value = []byte(value)
	case nativeSecretSet:
		if err := keyring.Set(
			request.Service,
			request.Account,
			string(request.Value),
		); err != nil {
			response.ErrorCode = nativeSecretWorkerErrorCode(err)
		}
	case nativeSecretDelete:
		err := keyring.Delete(request.Service, request.Account)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			response.ErrorCode = nativeSecretWorkerErrorCode(err)
		}
	case nativeSecretDeleteExact:
		value, err := keyring.Get(request.Service, request.Account)
		if errors.Is(err, keyring.ErrNotFound) {
			return response
		}
		if err != nil {
			response.ErrorCode = nativeSecretWorkerErrorCode(err)
			return response
		}
		raw := []byte(value)
		matches := subtle.ConstantTimeCompare(
			raw,
			request.Value,
		) == 1
		zeroSecretBytes(raw)
		if !matches {
			response.ErrorCode = CloudErrSecretConflict
			return response
		}
		err = keyring.Delete(request.Service, request.Account)
		if err != nil && !errors.Is(err, keyring.ErrNotFound) {
			response.ErrorCode = nativeSecretWorkerErrorCode(err)
		}
	default:
		response.ErrorCode = CloudErrSecretStore
	}
	return response
}

func nativeSecretWorkerErrorCode(err error) CloudErrorCode {
	classified := classifyOAuthNativeStoreError(err)
	var cloudErr *CloudError
	if errors.As(classified, &cloudErr) {
		return cloudErr.Code
	}
	return CloudErrSecretStore
}
