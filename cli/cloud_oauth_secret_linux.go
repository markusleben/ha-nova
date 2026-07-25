//go:build linux

package main

import (
	"context"
	"errors"
	"time"

	dbus "github.com/godbus/dbus/v5"
)

const (
	oauthSecretCollectionInterface = "org.freedesktop.Secret.Collection"
	oauthSecretItemInterface       = "org.freedesktop.Secret.Item"
	oauthSecretPromptInterface     = "org.freedesktop.Secret.Prompt"
	oauthSecretSessionInterface    = "org.freedesktop.Secret.Session"
)

type linuxOAuthSecretBackend struct {
	itemLabel string
}

func newNativeOAuthSecretBackend() (OAuthSecretBackend, error) {
	return &linuxOAuthSecretBackend{
		itemLabel: "HA NOVA Home Assistant Cloud authorization",
	}, nil
}

func newNativeCredentialSecretBackend() (OAuthSecretBackend, error) {
	return &linuxOAuthSecretBackend{
		itemLabel: "HA NOVA secure credential",
	}, nil
}

func (b *linuxOAuthSecretBackend) Get(ctx context.Context, service, account string, ui SecretStoreUIPolicy) (string, bool, error) {
	if err := validateLinuxOAuthSecretUI(ui); err != nil {
		return "", false, err
	}
	ctx = linuxSecretServiceSinglePromptContext(ctx, ui)
	conn, collection, err := linuxOAuthSecretCollection(ctx, ui)
	if err != nil {
		return "", false, err
	}
	item, exists, err := linuxOAuthSecretFindItem(ctx, conn, collection, service, account, ui)
	if err != nil || !exists {
		return "", exists, err
	}
	session, closeSession, err := linuxOAuthSecretOpenSession(ctx, conn)
	if err != nil {
		return "", false, err
	}
	defer closeSession()

	var secret secretServiceSecret
	if err := conn.Object(secretServiceDBusName, item).
		CallWithContext(ctx, oauthSecretItemInterface+".GetSecret", 0, session).
		Store(&secret); err != nil {
		return "", false, classifyOAuthNativeStoreError(normalizeLinuxKeyringError(err))
	}
	defer zeroSecretBytes(secret.Value)
	if secret.Session != session || len(secret.Parameters) != 0 ||
		len(secret.Value) == 0 || len(secret.Value) > oauthSecretMaxEncodedSize {
		return "", false, newCloudError(CloudErrSecretCorrupt, "read OAuth secret", nil)
	}
	return string(secret.Value), true, nil
}

func (b *linuxOAuthSecretBackend) Set(ctx context.Context, service, account, value string, ui SecretStoreUIPolicy) error {
	if err := validateLinuxOAuthSecretUI(ui); err != nil {
		return err
	}
	ctx = linuxSecretServiceSinglePromptContext(ctx, ui)
	conn, collection, err := linuxOAuthSecretCollection(ctx, ui)
	if err != nil {
		return err
	}
	session, closeSession, err := linuxOAuthSecretOpenSession(ctx, conn)
	if err != nil {
		return err
	}
	defer closeSession()

	secret := newSecretServiceSecret(session, []byte(value))
	defer zeroSecretBytes(secret.Value)
	label := b.itemLabel
	if label == "" {
		label = "HA NOVA secure credential"
	}
	properties := map[string]dbus.Variant{
		oauthSecretItemInterface + ".Label": dbus.MakeVariant(label),
		oauthSecretItemInterface + ".Attributes": dbus.MakeVariant(map[string]string{
			"service":  service,
			"username": account,
		}),
	}
	var item, prompt dbus.ObjectPath
	err = conn.Object(secretServiceDBusName, collection).
		CallWithContext(ctx, oauthSecretCollectionInterface+".CreateItem", 0, properties, secret, true).
		Store(&item, &prompt)
	if err != nil {
		return reconcileLinuxOAuthSecretSet(
			ctx,
			b,
			service,
			account,
			secret.Value,
			linuxOAuthSecretMutationUnknown("write OAuth secret", err),
		)
	}
	result, err := linuxSecretServiceHandlePrompt(ctx, conn, prompt, ui)
	if err != nil {
		return reconcileLinuxOAuthSecretSet(
			ctx,
			b,
			service,
			account,
			secret.Value,
			linuxOAuthSecretMutationUnknown("write OAuth secret", err),
		)
	}
	if item == "" || item == dbus.ObjectPath("/") {
		if path, ok := result.Value().(dbus.ObjectPath); ok {
			item = path
		}
	}
	if !stringsHasDBusPathPrefix(item, collection) {
		return reconcileLinuxOAuthSecretSet(
			ctx,
			b,
			service,
			account,
			secret.Value,
			linuxOAuthSecretMutationUnknown(
				"write OAuth secret",
				errors.New("Secret Service returned an invalid item"),
			),
		)
	}
	return nil
}

func (b *linuxOAuthSecretBackend) Delete(ctx context.Context, service, account string, ui SecretStoreUIPolicy) error {
	if err := validateLinuxOAuthSecretUI(ui); err != nil {
		return err
	}
	ctx = linuxSecretServiceSinglePromptContext(ctx, ui)
	conn, collection, err := linuxOAuthSecretCollection(ctx, ui)
	if err != nil {
		return err
	}
	item, exists, err := linuxOAuthSecretFindItem(ctx, conn, collection, service, account, ui)
	if err != nil || !exists {
		return err
	}
	var prompt dbus.ObjectPath
	if err := conn.Object(secretServiceDBusName, item).
		CallWithContext(ctx, oauthSecretItemInterface+".Delete", 0).
		Store(&prompt); err != nil {
		return reconcileLinuxOAuthSecretDelete(
			ctx,
			b,
			service,
			account,
			linuxOAuthSecretMutationUnknown("delete OAuth secret", err),
		)
	}
	if _, err = linuxSecretServiceHandlePrompt(ctx, conn, prompt, ui); err != nil {
		return reconcileLinuxOAuthSecretDelete(
			ctx,
			b,
			service,
			account,
			linuxOAuthSecretMutationUnknown("delete OAuth secret", err),
		)
	}
	return nil
}

func validateLinuxOAuthSecretUI(ui SecretStoreUIPolicy) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	if ui == SecretStoreAllowUI && !platformNativeSecretPromptAvailable() {
		return newCloudError(
			CloudErrSecretUIForbidden,
			"open Secret Service prompt",
			nil,
		)
	}
	return nil
}

func linuxOAuthSecretCollection(ctx context.Context, ui SecretStoreUIPolicy) (*dbus.Conn, dbus.ObjectPath, error) {
	if err := validateSecretUIPolicy(ui); err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", newCloudError(CloudErrTimeout, "connect to Secret Service", err)
	}
	busTimeout := relayAuthTokenPreflightTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, "", newCloudError(
				CloudErrTimeout,
				"connect to Secret Service",
				context.DeadlineExceeded,
			)
		}
		if remaining < busTimeout {
			busTimeout = remaining
		}
	}
	conn, err := relayAuthTokenPreflightSessionBusWithTimeout(busTimeout)
	if err != nil {
		return nil, "", classifyOAuthNativeStoreError(normalizeLinuxKeyringError(err))
	}
	var collection dbus.ObjectPath
	err = conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath)).
		CallWithContext(ctx, secretServiceDBusInterface+".ReadAlias", 0, secretServiceDefaultCollectionAlias).
		Store(&collection)
	if err != nil {
		return nil, "", classifyOAuthNativeStoreError(normalizeLinuxKeyringError(err))
	}
	if collection == "" || collection == dbus.ObjectPath("/") {
		if ui == SecretStoreForbidUI {
			return nil, "", newCloudError(CloudErrSecretUIForbidden, "initialize Secret Service collection", nil)
		}
		collection, err = linuxOAuthSecretCreateCollection(ctx, conn)
		if err != nil {
			return nil, "", err
		}
	}
	if err := linuxOAuthSecretUnlock(ctx, conn, collection, ui); err != nil {
		return nil, "", err
	}
	return conn, collection, nil
}

func linuxOAuthSecretCreateCollection(ctx context.Context, conn *dbus.Conn) (dbus.ObjectPath, error) {
	properties := map[string]dbus.Variant{
		oauthSecretCollectionInterface + ".Label": dbus.MakeVariant(secretServiceDefaultCollectionLabel),
	}
	var collection, prompt dbus.ObjectPath
	err := conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath)).
		CallWithContext(ctx, secretServiceDBusInterface+".CreateCollection", 0, properties, secretServiceDefaultCollectionAlias).
		Store(&collection, &prompt)
	if err != nil {
		return "", classifyOAuthNativeStoreError(normalizeLinuxKeyringError(err))
	}
	result, err := linuxSecretServiceHandlePrompt(ctx, conn, prompt, SecretStoreAllowUI)
	if err != nil {
		return "", err
	}
	if collection == dbus.ObjectPath("/") {
		if path, ok := result.Value().(dbus.ObjectPath); ok {
			collection = path
		}
	}
	if collection == "" || collection == dbus.ObjectPath("/") {
		return "", newCloudError(CloudErrSecretStore, "initialize Secret Service collection", nil)
	}
	return collection, nil
}

func linuxOAuthSecretUnlock(ctx context.Context, conn *dbus.Conn, object dbus.ObjectPath, ui SecretStoreUIPolicy) error {
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	err := conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath)).
		CallWithContext(ctx, secretServiceDBusInterface+".Unlock", 0, []dbus.ObjectPath{object}).
		Store(&unlocked, &prompt)
	if err != nil {
		return classifyOAuthNativeStoreError(normalizeLinuxKeyringError(err))
	}
	if len(unlocked) > 0 {
		return nil
	}
	result, err := linuxSecretServiceHandlePrompt(ctx, conn, prompt, ui)
	if err != nil {
		return err
	}
	if paths, ok := result.Value().([]dbus.ObjectPath); ok {
		for _, path := range paths {
			if path == object {
				return nil
			}
		}
	}
	if prompt == dbus.ObjectPath("/") {
		return newCloudError(CloudErrSecretStoreLocked, "unlock Secret Service item", nil)
	}
	return newCloudError(CloudErrSecretStoreLocked, "unlock Secret Service item", nil)
}

func linuxOAuthSecretFindItem(
	ctx context.Context,
	conn *dbus.Conn,
	collection dbus.ObjectPath,
	service, account string,
	ui SecretStoreUIPolicy,
) (dbus.ObjectPath, bool, error) {
	attributes := map[string]string{"service": service, "username": account}
	var unlocked, locked []dbus.ObjectPath
	err := conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath)).
		CallWithContext(ctx, secretServiceDBusInterface+".SearchItems", 0, attributes).
		Store(&unlocked, &locked)
	if err != nil {
		return "", false, classifyOAuthNativeStoreError(normalizeLinuxKeyringError(err))
	}
	if len(unlocked)+len(locked) > 1 {
		return "", false, newCloudError(CloudErrSecretCorrupt, "locate OAuth secret", nil)
	}
	if len(unlocked) > 0 {
		if !stringsHasDBusPathPrefix(unlocked[0], collection) {
			return "", false, newCloudError(CloudErrSecretStore, "locate OAuth secret", nil)
		}
		return unlocked[0], true, nil
	}
	if len(locked) == 0 {
		return "", false, nil
	}
	// Ensure the item belongs to the already-unlocked target collection. Search
	// is global, while the service/account pair is intentionally install-scoped.
	if !stringsHasDBusPathPrefix(locked[0], collection) {
		return "", false, newCloudError(CloudErrSecretStore, "locate OAuth secret", nil)
	}
	if err := linuxOAuthSecretUnlock(ctx, conn, locked[0], ui); err != nil {
		return "", false, err
	}
	return locked[0], true, nil
}

func linuxOAuthSecretOpenSession(ctx context.Context, conn *dbus.Conn) (dbus.ObjectPath, func(), error) {
	var output dbus.Variant
	var session dbus.ObjectPath
	err := conn.Object(secretServiceDBusName, dbus.ObjectPath(secretServiceDBusPath)).
		CallWithContext(ctx, secretServiceDBusInterface+".OpenSession", 0, "plain", dbus.MakeVariant("")).
		Store(&output, &session)
	if err != nil {
		return "", func() {}, classifyOAuthNativeStoreError(normalizeLinuxKeyringError(err))
	}
	closeSession := func() {
		closeCtx, cancel := context.WithTimeout(ctx, relayAuthTokenPreflightTimeout)
		defer cancel()
		_ = conn.Object(secretServiceDBusName, session).
			CallWithContext(closeCtx, oauthSecretSessionInterface+".Close", 0).
			Err
	}
	return session, closeSession, nil
}

func stringsHasDBusPathPrefix(item, collection dbus.ObjectPath) bool {
	return len(collection) > 1 && len(item) > len(collection) &&
		string(item[:len(collection)]) == string(collection) &&
		item[len(collection)] == '/'
}
