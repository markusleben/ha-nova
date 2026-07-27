//go:build linux

package main

import (
	"context"
	"fmt"
	"strings"

	dbus "github.com/godbus/dbus/v5"
)

const (
	secretCollectionDBusInterface       = "org.freedesktop.Secret.Collection"
	secretServicePropertiesInterface    = "org.freedesktop.DBus.Properties"
	secretServiceDefaultCollectionAlias = "default"
	secretServiceDefaultCollectionLabel = "Login"
	secretServiceContentType            = "text/plain; charset=utf8"
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

type secretServiceSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string `dbus:"content_type"`
}

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
