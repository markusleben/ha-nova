package main

import "context"

func (b *memoryOAuthSecretBackend) DeleteExact(
	_ context.Context,
	service, account, expected string,
	ui SecretStoreUIPolicy,
) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.policies = append(b.policies, ui)
	b.operations = append(b.operations, "delete_exact")
	if b.beforeDeleteExact != nil {
		b.beforeDeleteExact(service, account)
	}
	if b.fail != nil {
		if err := b.fail("delete", service); err != nil {
			return err
		}
	}
	key := service + "\x00" + account
	actual, exists := b.values[key]
	if !exists {
		return nil
	}
	if actual != expected {
		return newCloudError(
			CloudErrSecretConflict,
			"delete exact OAuth secret",
			nil,
		)
	}
	delete(b.values, key)
	return nil
}
