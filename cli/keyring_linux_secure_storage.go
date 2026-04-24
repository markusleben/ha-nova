//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	dbus "github.com/godbus/dbus/v5"
)

const (
	secretCollectionDBusInterface        = "org.freedesktop.Secret.Collection"
	dbusIntrospectableInterface          = "org.freedesktop.DBus.Introspectable"
	secretServicePropertiesInterface     = "org.freedesktop.DBus.Properties"
	secretServiceDefaultCollectionAlias  = "default"
	secretServiceDefaultCollectionLabel  = "Login"
	secretServiceContentType             = "text/plain; charset=utf8"
	gnomeKeyringInternalDBusInterface    = "org.gnome.keyring.InternalUnsupportedGuiltRiddenInterface"
	gnomeKeyringCreateWithPasswordMethod = gnomeKeyringInternalDBusInterface + ".CreateWithMasterPassword"
	gnomeKeyringUnlockWithPasswordMethod = gnomeKeyringInternalDBusInterface + ".UnlockWithMasterPassword"
)

type linuxSecureStorageStateKind string

const (
	linuxSecureStorageStateWritable  linuxSecureStorageStateKind = "writable"
	linuxSecureStorageStateLocked    linuxSecureStorageStateKind = "locked"
	linuxSecureStorageStateNeedsInit linuxSecureStorageStateKind = "needs-init"
)

type linuxSecureStorageState struct {
	kind              linuxSecureStorageStateKind
	defaultCollection dbus.ObjectPath
}

type secretServiceOwnerProcess struct {
	comm    string
	exePath string
}

type secretServiceSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string `dbus:"content_type"`
}

var secureStorageRecoveryReadFile = os.ReadFile
var secureStorageRecoveryReadLink = os.Readlink
var secureStorageRecoveryStat = os.Stat
var secureStorageRecoverySupportsGNOMEMethods = secretServiceSupportsGNOMERecoveryMethods

func inspectLinuxSecureStorageState() (linuxSecureStorageState, error) {
	conn, err := relayAuthTokenPreflightSessionBusWithTimeout(relayAuthTokenPreflightTimeout)
	if err != nil {
		if isDesktopKeyringSessionUnavailableError(err) {
			return linuxSecureStorageState{}, desktopKeyringSessionUnavailableError(err.Error())
		}
		if errorsIsContextDeadlineExceeded(err) {
			return linuxSecureStorageState{}, desktopKeyringUnavailableError("Secret Service preflight timed out")
		}
		return linuxSecureStorageState{}, err
	}
	return inspectLinuxSecureStorageStateWithConn(conn)
}

func inspectLinuxSecureStorageStateWithConn(conn *dbus.Conn) (linuxSecureStorageState, error) {
	defaultCollection, err := readSecretServiceDefaultCollection(conn)
	if err != nil {
		return linuxSecureStorageState{}, err
	}
	if strings.TrimSpace(string(defaultCollection)) == "" || defaultCollection == dbus.ObjectPath("/") {
		return linuxSecureStorageState{kind: linuxSecureStorageStateNeedsInit}, nil
	}

	locked, err := secretServiceCollectionLocked(conn, defaultCollection)
	if err != nil {
		if isDesktopKeyringInitializationRequiredError(err) {
			return linuxSecureStorageState{kind: linuxSecureStorageStateNeedsInit}, nil
		}
		return linuxSecureStorageState{}, err
	}
	if locked {
		return linuxSecureStorageState{
			kind:              linuxSecureStorageStateLocked,
			defaultCollection: defaultCollection,
		}, nil
	}

	return linuxSecureStorageState{
		kind:              linuxSecureStorageStateWritable,
		defaultCollection: defaultCollection,
	}, nil
}

func readSecretServiceDefaultCollection(conn *dbus.Conn) (dbus.ObjectPath, error) {
	service := conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath))
	ctx, cancel := context.WithTimeout(context.Background(), relayAuthTokenPreflightTimeout)
	defer cancel()

	var defaultCollection dbus.ObjectPath
	if err := service.CallWithContext(ctx, secretServiceDBusInterface+".ReadAlias", 0, secretServiceDefaultCollectionAlias).Store(&defaultCollection); err != nil {
		return "", normalizeLinuxKeyringErrorWithoutAmbiguousClassification(err)
	}
	return defaultCollection, nil
}

func secretServiceCollectionLocked(conn *dbus.Conn, collection dbus.ObjectPath) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), relayAuthTokenPreflightTimeout)
	defer cancel()

	var locked dbus.Variant
	err := conn.Object(secretServiceDBusName, collection).CallWithContext(
		ctx,
		secretServicePropertiesInterface+".Get",
		0,
		secretCollectionDBusInterface,
		"Locked",
	).Store(&locked)
	if err != nil {
		return false, normalizeLinuxKeyringErrorWithoutAmbiguousClassification(err)
	}

	value, ok := locked.Value().(bool)
	if !ok {
		return false, fmt.Errorf("Secret Service returned invalid collection lock state")
	}
	return value, nil
}

func openSecretServiceSession(conn *dbus.Conn) (dbus.ObjectPath, func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), relayAuthTokenPreflightTimeout)
	var output dbus.Variant
	var session dbus.ObjectPath
	err := conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath)).
		CallWithContext(ctx, secretServiceDBusInterface+".OpenSession", 0, "plain", dbus.MakeVariant("")).
		Store(&output, &session)
	cancel()
	if err != nil {
		return "", func() {}, normalizeLinuxKeyringErrorWithoutAmbiguousClassification(err)
	}

	closeSession := func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), relayAuthTokenPreflightTimeout)
		defer closeCancel()
		_ = conn.Object(secretServiceDBusName, session).CallWithContext(closeCtx, "org.freedesktop.Secret.Session.Close", 0).Err
	}
	return session, closeSession, nil
}

func newSecretServiceSecret(session dbus.ObjectPath, secret []byte) secretServiceSecret {
	return secretServiceSecret{
		Session:     session,
		Parameters:  []byte{},
		Value:       append([]byte(nil), secret...),
		ContentType: secretServiceContentType,
	}
}

func initializeLinuxSecureStorage(conn *dbus.Conn, secret []byte) error {
	session, closeSession, err := openSecretServiceSession(conn)
	if err != nil {
		return err
	}
	defer closeSession()
	payload := newSecretServiceSecret(session, secret)
	defer zeroSecretBytes(payload.Value)

	properties := map[string]dbus.Variant{
		secretCollectionDBusInterface + ".Label": dbus.MakeVariant(secretServiceDefaultCollectionLabel),
	}

	ctx, cancel := context.WithTimeout(context.Background(), secureStorageUnlockTimeout)
	defer cancel()

	var collection dbus.ObjectPath
	err = conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath)).
		CallWithContext(ctx, gnomeKeyringCreateWithPasswordMethod, 0, properties, payload).
		Store(&collection)
	if err != nil {
		return normalizeLinuxKeyringErrorWithoutAmbiguousClassification(err)
	}
	if strings.TrimSpace(string(collection)) == "" || collection == dbus.ObjectPath("/") {
		return desktopKeyringInitializationRequiredError("GNOME Keyring did not create a default collection")
	}

	if err := conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath)).
		CallWithContext(ctx, secretServiceDBusInterface+".SetAlias", 0, secretServiceDefaultCollectionAlias, collection).Err; err != nil {
		return normalizeLinuxKeyringErrorWithoutAmbiguousClassification(err)
	}
	return nil
}

func unlockLinuxSecureStorage(conn *dbus.Conn, collection dbus.ObjectPath, secret []byte) error {
	session, closeSession, err := openSecretServiceSession(conn)
	if err != nil {
		return err
	}
	defer closeSession()
	payload := newSecretServiceSecret(session, secret)
	defer zeroSecretBytes(payload.Value)

	ctx, cancel := context.WithTimeout(context.Background(), secureStorageUnlockTimeout)
	defer cancel()

	err = conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath)).
		CallWithContext(ctx, gnomeKeyringUnlockWithPasswordMethod, 0, collection, payload).Err
	if err != nil {
		if isGNOMEKeyringInvalidPasswordError(err) {
			return localSecureStoragePasswordRejectedError()
		}
		return normalizeLinuxKeyringErrorWithoutAmbiguousClassification(err)
	}
	return nil
}

func isGNOMEKeyringInvalidPasswordError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "password was invalid")
}

func detectSecretServiceOwnerProcessForRecovery() (secretServiceOwnerProcess, error) {
	conn, err := relayAuthTokenPreflightSessionBusWithTimeout(relayAuthTokenPreflightTimeout)
	if err != nil {
		if isDesktopKeyringSessionUnavailableError(err) {
			return secretServiceOwnerProcess{}, desktopKeyringSessionUnavailableError(err.Error())
		}
		if errorsIsContextDeadlineExceeded(err) {
			return secretServiceOwnerProcess{}, desktopKeyringUnavailableError("Secret Service preflight timed out")
		}
		return secretServiceOwnerProcess{}, err
	}
	return secretServiceOwnerInfo(conn)
}

func secretServiceOwnerInfo(conn *dbus.Conn) (secretServiceOwnerProcess, error) {
	bus := conn.Object(dbusServiceName, dbus.ObjectPath(dbusServicePath))

	var owner string
	ownerCtx, ownerCancel := context.WithTimeout(context.Background(), relayAuthTokenPreflightTimeout)
	if err := bus.CallWithContext(ownerCtx, dbusServiceInterface+".GetNameOwner", 0, secretServiceDBusName).Store(&owner); err != nil {
		ownerCancel()
		return secretServiceOwnerProcess{}, normalizeLinuxKeyringError(err)
	}
	ownerCancel()
	if owner == "" {
		return secretServiceOwnerProcess{}, desktopKeyringUnavailableError("Secret Service owner unavailable")
	}

	var pid uint32
	pidCtx, pidCancel := context.WithTimeout(context.Background(), relayAuthTokenPreflightTimeout)
	if err := bus.CallWithContext(pidCtx, dbusServiceInterface+".GetConnectionUnixProcessID", 0, owner).Store(&pid); err != nil {
		pidCancel()
		return secretServiceOwnerProcess{}, normalizeLinuxKeyringError(err)
	}
	pidCancel()
	if pid == 0 {
		return secretServiceOwnerProcess{}, fmt.Errorf("Secret Service owner pid unavailable")
	}

	process := secretServiceOwnerProcess{}
	if comm, err := secureStorageRecoveryReadFile(filepath.Join("/proc", fmt.Sprintf("%d", pid), "comm")); err == nil {
		process.comm = strings.TrimSpace(string(comm))
	}
	if exePath, err := secureStorageRecoveryReadLink(filepath.Join("/proc", fmt.Sprintf("%d", pid), "exe")); err == nil {
		process.exePath = exePath
	}
	if process.comm == "" && process.exePath == "" {
		return secretServiceOwnerProcess{}, fmt.Errorf("Secret Service owner process details unavailable")
	}
	return process, nil
}

func (process secretServiceOwnerProcess) supportsGNOMEKeyringRecovery() bool {
	if !isGNOMEKeyringProcessName(process.comm) && !isGNOMEKeyringProcessName(filepath.Base(process.exePath)) {
		return false
	}
	if !filepath.IsAbs(process.exePath) {
		return false
	}
	info, err := secureStorageRecoveryStat(process.exePath)
	if err != nil {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return stat.Uid == 0
}

func secretServiceSupportsGNOMERecoveryMethods(conn *dbus.Conn) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), relayAuthTokenPreflightTimeout)
	defer cancel()

	var xml string
	err := conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath)).
		CallWithContext(ctx, dbusIntrospectableInterface+".Introspect", 0).
		Store(&xml)
	if err != nil {
		return false, normalizeLinuxKeyringError(err)
	}

	return strings.Contains(xml, gnomeKeyringInternalDBusInterface) &&
		strings.Contains(xml, "CreateWithMasterPassword") &&
		strings.Contains(xml, "UnlockWithMasterPassword"), nil
}

func isGNOMEKeyringProcessName(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	return name == gnomeKeyringComm || strings.HasPrefix(name, "gnome-keyring")
}
