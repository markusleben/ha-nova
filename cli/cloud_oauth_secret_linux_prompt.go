//go:build linux

package main

import (
	"context"

	dbus "github.com/godbus/dbus/v5"
)

type linuxSecretPromptBudgetContextKey struct{}

type linuxSecretPromptBudget struct {
	used bool
}

// linuxSecretServiceHandlePrompt is the reusable native Secret Service prompt
// primitive. ForbidUI returns before Prompt is called.
func linuxSecretServiceHandlePrompt(
	ctx context.Context,
	conn *dbus.Conn,
	prompt dbus.ObjectPath,
	ui SecretStoreUIPolicy,
) (dbus.Variant, error) {
	empty := dbus.MakeVariant("")
	if prompt == "" || prompt == dbus.ObjectPath("/") {
		return empty, nil
	}
	if err := linuxSecretServiceConsumePromptBudget(ctx, ui); err != nil {
		return empty, err
	}
	match := []dbus.MatchOption{
		dbus.WithMatchObjectPath(prompt),
		dbus.WithMatchInterface(oauthSecretPromptInterface),
	}
	if err := conn.AddMatchSignalContext(ctx, match...); err != nil {
		return empty, classifyOAuthNativeStoreError(normalizeLinuxKeyringError(err))
	}
	defer linuxSecretServiceRemovePromptMatch(ctx, conn, match)

	signals := make(chan *dbus.Signal, 1)
	conn.Signal(signals)
	defer conn.RemoveSignal(signals)
	if err := conn.Object(secretServiceDBusName, prompt).
		CallWithContext(ctx, oauthSecretPromptInterface+".Prompt", 0, "").Err; err != nil {
		return empty, classifyOAuthNativeStoreError(normalizeLinuxKeyringError(err))
	}
	select {
	case <-ctx.Done():
		// The deadline must remain hard even if Secret Service stopped
		// responding. Dismiss in a separately bounded goroutine; no provider or
		// transport behavior may delay the caller after its deadline.
		go func() {
			dismissCtx, cancel := context.WithTimeout(
				context.Background(),
				relayAuthTokenPreflightTimeout,
			)
			defer cancel()
			call := conn.Object(secretServiceDBusName, prompt).GoWithContext(
				dismissCtx,
				oauthSecretPromptInterface+".Dismiss",
				0,
				make(chan *dbus.Call, 1),
			)
			select {
			case <-call.Done:
			case <-dismissCtx.Done():
			}
		}()
		return empty, newCloudError(
			CloudErrTimeout,
			"wait for Secret Service prompt",
			ctx.Err(),
		)
	case signal := <-signals:
		if err := ctx.Err(); err != nil {
			return empty, newCloudError(
				CloudErrTimeout,
				"wait for Secret Service prompt",
				err,
			)
		}
		if signal == nil ||
			signal.Name != oauthSecretPromptInterface+".Completed" ||
			len(signal.Body) != 2 {
			return empty, newCloudError(
				CloudErrSecretStore,
				"complete Secret Service prompt",
				nil,
			)
		}
		dismissed, ok := signal.Body[0].(bool)
		result, resultOK := signal.Body[1].(dbus.Variant)
		if !ok || !resultOK {
			return empty, newCloudError(
				CloudErrSecretStore,
				"complete Secret Service prompt",
				nil,
			)
		}
		if dismissed {
			return empty, newCloudError(
				CloudErrSecretPromptCanceled,
				"complete Secret Service prompt",
				nil,
			)
		}
		return result, nil
	}
}

func linuxSecretServiceSinglePromptContext(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) context.Context {
	if ui != SecretStoreAllowUI {
		return ctx
	}
	return context.WithValue(
		ctx,
		linuxSecretPromptBudgetContextKey{},
		&linuxSecretPromptBudget{},
	)
}

func linuxSecretServiceConsumePromptBudget(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	if ui == SecretStoreForbidUI {
		return newCloudError(
			CloudErrSecretUIForbidden,
			"open Secret Service prompt",
			nil,
		)
	}
	budget, _ := ctx.Value(linuxSecretPromptBudgetContextKey{}).(*linuxSecretPromptBudget)
	if budget == nil {
		return nil
	}
	if budget.used {
		return newCloudError(
			CloudErrSecretUIForbidden,
			"open additional Secret Service prompt",
			nil,
		)
	}
	budget.used = true
	return nil
}

func linuxSecretServiceRemovePromptMatch(
	ctx context.Context,
	conn *dbus.Conn,
	match []dbus.MatchOption,
) {
	remove := func(cleanupCtx context.Context) {
		_ = conn.RemoveMatchSignalContext(cleanupCtx, match...)
	}
	if ctx.Err() == nil {
		cleanupCtx, cancel := context.WithTimeout(
			ctx,
			relayAuthTokenPreflightTimeout,
		)
		defer cancel()
		remove(cleanupCtx)
		return
	}
	go func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			relayAuthTokenPreflightTimeout,
		)
		defer cancel()
		remove(cleanupCtx)
	}()
}
