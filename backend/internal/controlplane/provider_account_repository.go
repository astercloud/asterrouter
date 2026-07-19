package controlplane

import "context"

func (r *MemoryRepository) DeleteProviderAccount(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.accounts, id)
	delete(r.accountModels, id)
	delete(r.accountHealthChecks, id)
	return nil
}

func (r *PostgresRepository) DeleteProviderAccount(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM provider_accounts WHERE id = $1`, id)
	return err
}
