//go:build darwin

package main

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type darwinOAuthOSStatus int32

const (
	darwinOAuthSuccess               darwinOAuthOSStatus = 0
	darwinOAuthUserCanceled          darwinOAuthOSStatus = -128
	darwinOAuthNotAvailable          darwinOAuthOSStatus = -25291
	darwinOAuthAuthFailed            darwinOAuthOSStatus = -25293
	darwinOAuthNoSuchKeychain        darwinOAuthOSStatus = -25294
	darwinOAuthItemNotFound          darwinOAuthOSStatus = -25300
	darwinOAuthInteractionNotAllowed darwinOAuthOSStatus = -25308
)

var darwinOAuthSecurity struct {
	sync.Once
	err error

	getInteraction func(*uint8) darwinOAuthOSStatus
	setInteraction func(uint8) darwinOAuthOSStatus
	findGeneric    func(
		uintptr,
		uint32, *byte,
		uint32, *byte,
		*uint32, *unsafe.Pointer,
		*uintptr,
	) darwinOAuthOSStatus
	addGeneric func(
		uintptr,
		uint32, *byte,
		uint32, *byte,
		uint32, *byte,
		*uintptr,
	) darwinOAuthOSStatus
	freeContent func(uintptr, unsafe.Pointer) darwinOAuthOSStatus
	modifyItem  func(uintptr, uintptr, uint32, *byte) darwinOAuthOSStatus
	deleteItem  func(uintptr) darwinOAuthOSStatus

	cfRelease func(uintptr)
}

func loadDarwinOAuthSecurity() error {
	darwinOAuthSecurity.Do(func() {
		security, err := purego.Dlopen(
			"/System/Library/Frameworks/Security.framework/Security",
			purego.RTLD_LAZY|purego.RTLD_LOCAL,
		)
		if err != nil {
			darwinOAuthSecurity.err = err
			return
		}
		coreFoundation, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			purego.RTLD_LAZY|purego.RTLD_LOCAL,
		)
		if err != nil {
			darwinOAuthSecurity.err = err
			return
		}
		bindings := []struct {
			target any
			handle uintptr
			name   string
		}{
			{&darwinOAuthSecurity.getInteraction, security, "SecKeychainGetUserInteractionAllowed"},
			{&darwinOAuthSecurity.setInteraction, security, "SecKeychainSetUserInteractionAllowed"},
			{&darwinOAuthSecurity.findGeneric, security, "SecKeychainFindGenericPassword"},
			{&darwinOAuthSecurity.addGeneric, security, "SecKeychainAddGenericPassword"},
			{&darwinOAuthSecurity.freeContent, security, "SecKeychainItemFreeContent"},
			{&darwinOAuthSecurity.modifyItem, security, "SecKeychainItemModifyAttributesAndData"},
			{&darwinOAuthSecurity.deleteItem, security, "SecKeychainItemDelete"},
			{&darwinOAuthSecurity.cfRelease, coreFoundation, "CFRelease"},
		}
		for _, binding := range bindings {
			if err := bindDarwinOAuthSymbol(
				binding.target,
				binding.handle,
				binding.name,
			); err != nil {
				darwinOAuthSecurity.err = err
				return
			}
		}
	})
	return darwinOAuthSecurity.err
}

func bindDarwinOAuthSymbol(target any, handle uintptr, name string) error {
	address, err := purego.Dlsym(handle, name)
	if err != nil || address == 0 {
		return fmt.Errorf("resolve macOS Keychain symbol %s", name)
	}
	purego.RegisterFunc(target, address)
	return nil
}

func setDarwinOAuthInteraction(ui SecretStoreUIPolicy) (uint8, error) {
	var previous uint8
	if status := darwinOAuthSecurity.getInteraction(&previous); status != 0 {
		return 0, fmt.Errorf("read macOS Keychain interaction policy: %d", status)
	}
	desired := uint8(0)
	if ui == SecretStoreAllowUI {
		desired = 1
	}
	if status := darwinOAuthSecurity.setInteraction(desired); status != 0 {
		return 0, fmt.Errorf("set macOS Keychain interaction policy: %d", status)
	}
	return previous, nil
}

func darwinOAuthGet(
	service, account string,
) ([]byte, bool, darwinOAuthOSStatus) {
	serviceBytes, accountBytes := []byte(service), []byte(account)
	var length uint32
	var data unsafe.Pointer
	var item uintptr
	status := darwinOAuthSecurity.findGeneric(
		0,
		uint32(len(serviceBytes)), bytePointer(serviceBytes),
		uint32(len(accountBytes)), bytePointer(accountBytes),
		&length, &data, &item,
	)
	runtime.KeepAlive(serviceBytes)
	runtime.KeepAlive(accountBytes)
	if item != 0 {
		defer darwinOAuthSecurity.cfRelease(item)
	}
	if status == darwinOAuthItemNotFound {
		return nil, false, darwinOAuthSuccess
	}
	if status != darwinOAuthSuccess {
		return nil, false, status
	}
	if data != nil {
		defer darwinOAuthSecurity.freeContent(0, data)
	}
	if length == 0 || length > oauthSecretMaxEncodedSize || data == nil {
		return nil, false, darwinOAuthAuthFailed
	}
	value := append([]byte(nil), unsafe.Slice((*byte)(data), int(length))...)
	return value, true, darwinOAuthSuccess
}

func darwinOAuthSet(
	service, account string,
	value []byte,
) darwinOAuthOSStatus {
	serviceBytes, accountBytes := []byte(service), []byte(account)
	var item uintptr
	status := darwinOAuthSecurity.findGeneric(
		0,
		uint32(len(serviceBytes)), bytePointer(serviceBytes),
		uint32(len(accountBytes)), bytePointer(accountBytes),
		nil, nil, &item,
	)
	runtime.KeepAlive(serviceBytes)
	runtime.KeepAlive(accountBytes)
	if status == darwinOAuthSuccess {
		defer darwinOAuthSecurity.cfRelease(item)
		status = darwinOAuthSecurity.modifyItem(
			item,
			0,
			uint32(len(value)),
			bytePointer(value),
		)
		runtime.KeepAlive(value)
		return status
	}
	if status != darwinOAuthItemNotFound {
		return status
	}
	// Do not replace the default Keychain ACL. SecKeychainAddGenericPassword
	// restricts the new item to the caller; broad application lists would let
	// unrelated processes read the OAuth refresh token.
	status = darwinOAuthSecurity.addGeneric(
		0,
		uint32(len(serviceBytes)), bytePointer(serviceBytes),
		uint32(len(accountBytes)), bytePointer(accountBytes),
		uint32(len(value)), bytePointer(value),
		&item,
	)
	runtime.KeepAlive(serviceBytes)
	runtime.KeepAlive(accountBytes)
	runtime.KeepAlive(value)
	if status != darwinOAuthSuccess {
		return status
	}
	defer darwinOAuthSecurity.cfRelease(item)
	return darwinOAuthSuccess
}

func darwinOAuthDelete(service, account string) darwinOAuthOSStatus {
	serviceBytes, accountBytes := []byte(service), []byte(account)
	var item uintptr
	status := darwinOAuthSecurity.findGeneric(
		0,
		uint32(len(serviceBytes)), bytePointer(serviceBytes),
		uint32(len(accountBytes)), bytePointer(accountBytes),
		nil, nil, &item,
	)
	runtime.KeepAlive(serviceBytes)
	runtime.KeepAlive(accountBytes)
	if status != darwinOAuthSuccess {
		return status
	}
	defer darwinOAuthSecurity.cfRelease(item)
	return darwinOAuthSecurity.deleteItem(item)
}

func darwinOAuthErrorCode(
	status darwinOAuthOSStatus,
	ui SecretStoreUIPolicy,
) CloudErrorCode {
	switch status {
	case darwinOAuthUserCanceled:
		return CloudErrSecretPromptCanceled
	case darwinOAuthInteractionNotAllowed:
		if ui == SecretStoreForbidUI {
			return CloudErrSecretUIForbidden
		}
		return CloudErrSecretStoreLocked
	case darwinOAuthAuthFailed:
		return CloudErrSecretStoreLocked
	case darwinOAuthNotAvailable, darwinOAuthNoSuchKeychain:
		return CloudErrSecretStore
	default:
		return CloudErrSecretStore
	}
}

func bytePointer(value []byte) *byte {
	if len(value) == 0 {
		return nil
	}
	return &value[0]
}
