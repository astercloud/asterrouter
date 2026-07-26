package controlplane

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/lib/pq"
)

const workspaceUserColumns = `id, email, email_normalized, display_name, avatar_data_url, status, role, balance_micros, concurrency_limit, rpm_limit, external_issuer, external_subject, department_id, totp_enabled, totp_secret_ciphertext, totp_recovery_hashes, password_hash, email_verified, email_verify_hash, email_verify_expires_at, email_verify_sent_at, password_reset_hash, password_reset_expires_at, password_reset_sent_at, session_version, created_at, updated_at`

const workspaceUserSelect = `SELECT ` + workspaceUserColumns + ` FROM workspace_users `

func (r *MemoryRepository) ListWorkspaceUsers(context.Context) ([]WorkspaceUser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WorkspaceUser, 0, len(r.workspaceUsers))
	for _, user := range r.workspaceUsers {
		out = append(out, user)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status == out[j].Status {
			return out[i].Email < out[j].Email
		}
		return out[i].Status < out[j].Status
	})
	return out, nil
}

func (r *MemoryRepository) SaveWorkspaceUser(_ context.Context, user WorkspaceUser) error {
	withNormalizedEmail(&user)
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, current := range r.workspaceUsers {
		if id == user.ID {
			continue
		}
		if current.Email == user.Email || (user.EmailNormalized != "" && current.EmailNormalized == user.EmailNormalized) {
			return ErrUserEmailExists
		}
	}
	r.workspaceUsers[user.ID] = user
	return nil
}

func (r *MemoryRepository) FindWorkspaceUserByEmail(_ context.Context, email string) (WorkspaceUser, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, user := range r.workspaceUsers {
		if user.Email == email {
			return user, true, nil
		}
	}
	return WorkspaceUser{}, false, nil
}

func (r *MemoryRepository) FindWorkspaceUserByEmailNormalized(_ context.Context, normalized string) (WorkspaceUser, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, user := range r.workspaceUsers {
		if user.EmailNormalized == normalized {
			return user, true, nil
		}
	}
	return WorkspaceUser{}, false, nil
}

func (r *MemoryRepository) FindWorkspaceUserByEmailVerifyHash(_ context.Context, hash string) (WorkspaceUser, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, user := range r.workspaceUsers {
		if user.EmailVerifyHash != "" && constantTimeHashEqual(user.EmailVerifyHash, hash) {
			return user, true, nil
		}
	}
	return WorkspaceUser{}, false, nil
}

func (r *MemoryRepository) FindWorkspaceUserByPasswordResetHash(_ context.Context, hash string) (WorkspaceUser, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, user := range r.workspaceUsers {
		if user.PasswordResetHash != "" && constantTimeHashEqual(user.PasswordResetHash, hash) {
			return user, true, nil
		}
	}
	return WorkspaceUser{}, false, nil
}

func (r *MemoryRepository) IssueWorkspaceUserEmailVerification(_ context.Context, email, hash string, now, expiresAt time.Time, cooldown time.Duration) (WorkspaceUser, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, user := range r.workspaceUsers {
		if user.Email != email || user.Status != WorkspaceUserStatusActive || user.EmailVerified {
			continue
		}
		if user.EmailVerifySentAt != nil && now.Sub(*user.EmailVerifySentAt) < cooldown {
			return WorkspaceUser{}, false, nil
		}
		user.EmailVerifyHash = hash
		user.EmailVerifyExpiresAt = &expiresAt
		user.EmailVerifySentAt = &now
		user.UpdatedAt = now
		r.workspaceUsers[id] = user
		return user, true, nil
	}
	return WorkspaceUser{}, false, nil
}

func (r *MemoryRepository) ConsumeWorkspaceUserEmailVerification(_ context.Context, hash string, now time.Time) (WorkspaceUser, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, user := range r.workspaceUsers {
		if user.EmailVerifyHash == "" || !constantTimeHashEqual(user.EmailVerifyHash, hash) || user.EmailVerified || user.EmailVerifyExpiresAt == nil || !now.Before(*user.EmailVerifyExpiresAt) {
			continue
		}
		user.EmailVerified = true
		user.EmailVerifyHash = ""
		user.EmailVerifyExpiresAt = nil
		user.UpdatedAt = now
		r.workspaceUsers[id] = user
		return user, true, nil
	}
	return WorkspaceUser{}, false, nil
}

func (r *MemoryRepository) IssueWorkspaceUserPasswordReset(_ context.Context, email, hash string, now, expiresAt time.Time, cooldown time.Duration) (WorkspaceUser, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, user := range r.workspaceUsers {
		if user.Email != email || user.Status != WorkspaceUserStatusActive {
			continue
		}
		if user.PasswordResetSentAt != nil && now.Sub(*user.PasswordResetSentAt) < cooldown {
			return WorkspaceUser{}, false, nil
		}
		user.PasswordResetHash = hash
		user.PasswordResetExpiresAt = &expiresAt
		user.PasswordResetSentAt = &now
		user.UpdatedAt = now
		r.workspaceUsers[id] = user
		return user, true, nil
	}
	return WorkspaceUser{}, false, nil
}

func (r *MemoryRepository) ConsumeWorkspaceUserPasswordReset(_ context.Context, hash, passwordHash string, now time.Time) (WorkspaceUser, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, user := range r.workspaceUsers {
		if user.PasswordResetHash == "" || !constantTimeHashEqual(user.PasswordResetHash, hash) || user.PasswordResetExpiresAt == nil || !now.Before(*user.PasswordResetExpiresAt) {
			continue
		}
		user.PasswordHash = passwordHash
		user.PasswordResetHash = ""
		user.PasswordResetExpiresAt = nil
		user.SessionVersion++
		user.UpdatedAt = now
		r.workspaceUsers[id] = user
		return user, true, nil
	}
	return WorkspaceUser{}, false, nil
}

func (r *MemoryRepository) ConsumeWorkspaceUserTOTPRecoveryCode(_ context.Context, userID, hash string, now time.Time) (WorkspaceUser, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	user, found := r.workspaceUsers[userID]
	if !found || user.Status != WorkspaceUserStatusActive || !user.TOTPEnabled {
		return WorkspaceUser{}, false, nil
	}
	for index, stored := range user.TOTPRecoveryHashes {
		if !constantTimeHashEqual(stored, hash) {
			continue
		}
		user.TOTPRecoveryHashes = append(user.TOTPRecoveryHashes[:index:index], user.TOTPRecoveryHashes[index+1:]...)
		user.UpdatedAt = now
		r.workspaceUsers[userID] = user
		return user, true, nil
	}
	return WorkspaceUser{}, false, nil
}

// withNormalizedEmail 在写库前统一填充 EmailNormalized，让别名去重的不变量
// 不依赖每个调用方记得赋值。邮箱为空（不该发生）时留空，唯一索引的部分条件
// 会跳过空值行。
func withNormalizedEmail(user *WorkspaceUser) {
	if user == nil {
		return
	}
	user.EmailNormalized = NormalizeEmailForAliasDedup(user.Email)
	if user.Email == "" {
		user.EmailNormalized = ""
	}
}

func (r *MemoryRepository) ListAuthIdentities(_ context.Context, userID string) ([]AuthIdentity, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AuthIdentity, 0)
	for _, identity := range r.authIdentities {
		if identity.UserID == userID {
			out = append(out, identity)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Issuer < out[j].Issuer })
	return out, nil
}

func (r *MemoryRepository) FindAuthIdentity(_ context.Context, issuer, subject string) (AuthIdentity, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	identity, ok := r.authIdentities[issuer+"\x00"+subject]
	return identity, ok, nil
}

func (r *MemoryRepository) SaveAuthIdentity(_ context.Context, identity AuthIdentity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := identity.Issuer + "\x00" + identity.Subject
	if current, exists := r.authIdentities[key]; exists && current.UserID != identity.UserID {
		return errors.New("external identity is already bound to another user")
	}
	for currentKey, current := range r.authIdentities {
		if current.UserID == identity.UserID && current.Issuer == identity.Issuer && currentKey != key {
			return errors.New("user already has an identity for this issuer")
		}
	}
	r.authIdentities[key] = identity
	return nil
}

func (r *MemoryRepository) DeleteAuthIdentity(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, identity := range r.authIdentities {
		if identity.ID == id {
			delete(r.authIdentities, key)
			return nil
		}
	}
	return sql.ErrNoRows
}

func (r *MemoryRepository) ListRoleBindings(context.Context) ([]RoleBinding, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RoleBinding, 0, len(r.roleBindings))
	for _, binding := range r.roleBindings {
		out = append(out, binding)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UserID == out[j].UserID {
			if out[i].ScopeType == out[j].ScopeType {
				return out[i].ScopeID < out[j].ScopeID
			}
			return out[i].ScopeType < out[j].ScopeType
		}
		return out[i].UserID < out[j].UserID
	})
	return out, nil
}

func (r *MemoryRepository) SaveRoleBinding(_ context.Context, binding RoleBinding) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roleBindings[binding.ID] = binding
	return nil
}

func (r *MemoryRepository) DeleteRoleBinding(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.roleBindings, id)
	return nil
}

func (r *PostgresRepository) ListWorkspaceUsers(ctx context.Context) ([]WorkspaceUser, error) {
	rows, err := r.db.QueryContext(ctx, workspaceUserSelect+`ORDER BY status ASC, email ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]WorkspaceUser, 0)
	for rows.Next() {
		user, err := scanWorkspaceUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

type workspaceUserRowScanner interface {
	Scan(dest ...any) error
}

func scanWorkspaceUser(scanner workspaceUserRowScanner) (WorkspaceUser, error) {
	var user WorkspaceUser
	var recovery string
	if err := scanner.Scan(&user.ID, &user.Email, &user.EmailNormalized, &user.DisplayName, &user.AvatarDataURL, &user.Status, &user.Role, &user.BalanceMicros, &user.ConcurrencyLimit, &user.RPMLimit, &user.ExternalIssuer, &user.ExternalSubject, &user.DepartmentID, &user.TOTPEnabled, &user.TOTPSecretCiphertext, &recovery, &user.PasswordHash, &user.EmailVerified, &user.EmailVerifyHash, &user.EmailVerifyExpiresAt, &user.EmailVerifySentAt, &user.PasswordResetHash, &user.PasswordResetExpiresAt, &user.PasswordResetSentAt, &user.SessionVersion, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return WorkspaceUser{}, err
	}
	user.TOTPRecoveryHashes = parseStringList(recovery)
	return user, nil
}

func (r *PostgresRepository) FindWorkspaceUserByEmail(ctx context.Context, email string) (WorkspaceUser, bool, error) {
	return r.findWorkspaceUser(ctx, workspaceUserSelect+`WHERE email = $1`, email)
}

func (r *PostgresRepository) FindWorkspaceUserByEmailNormalized(ctx context.Context, normalized string) (WorkspaceUser, bool, error) {
	return r.findWorkspaceUser(ctx, workspaceUserSelect+`WHERE email_normalized = $1 AND email_normalized <> ''`, normalized)
}

func (r *PostgresRepository) FindWorkspaceUserByEmailVerifyHash(ctx context.Context, hash string) (WorkspaceUser, bool, error) {
	return r.findWorkspaceUser(ctx, workspaceUserSelect+`WHERE email_verify_hash = $1 AND email_verify_hash <> ''`, hash)
}

func (r *PostgresRepository) FindWorkspaceUserByPasswordResetHash(ctx context.Context, hash string) (WorkspaceUser, bool, error) {
	return r.findWorkspaceUser(ctx, workspaceUserSelect+`WHERE password_reset_hash = $1 AND password_reset_hash <> ''`, hash)
}

func (r *PostgresRepository) IssueWorkspaceUserEmailVerification(ctx context.Context, email, hash string, now, expiresAt time.Time, cooldown time.Duration) (WorkspaceUser, bool, error) {
	query := `UPDATE workspace_users
SET email_verify_hash=$2, email_verify_expires_at=$3, email_verify_sent_at=$4, updated_at=$4
WHERE email=$1 AND status='active' AND email_verified=FALSE
  AND (email_verify_sent_at IS NULL OR email_verify_sent_at <= $5)
RETURNING ` + workspaceUserColumns
	return scanOptionalWorkspaceUser(r.db.QueryRowContext(ctx, query, email, hash, expiresAt, now, now.Add(-cooldown)))
}

func (r *PostgresRepository) ConsumeWorkspaceUserEmailVerification(ctx context.Context, hash string, now time.Time) (WorkspaceUser, bool, error) {
	query := `UPDATE workspace_users
SET email_verified=TRUE, email_verify_hash='', email_verify_expires_at=NULL, updated_at=$2
WHERE email_verify_hash=$1 AND email_verify_hash<>'' AND email_verified=FALSE AND email_verify_expires_at>$2
RETURNING ` + workspaceUserColumns
	return scanOptionalWorkspaceUser(r.db.QueryRowContext(ctx, query, hash, now))
}

func (r *PostgresRepository) IssueWorkspaceUserPasswordReset(ctx context.Context, email, hash string, now, expiresAt time.Time, cooldown time.Duration) (WorkspaceUser, bool, error) {
	query := `UPDATE workspace_users
SET password_reset_hash=$2, password_reset_expires_at=$3, password_reset_sent_at=$4, updated_at=$4
WHERE email=$1 AND status='active'
  AND (password_reset_sent_at IS NULL OR password_reset_sent_at <= $5)
RETURNING ` + workspaceUserColumns
	return scanOptionalWorkspaceUser(r.db.QueryRowContext(ctx, query, email, hash, expiresAt, now, now.Add(-cooldown)))
}

func (r *PostgresRepository) ConsumeWorkspaceUserPasswordReset(ctx context.Context, hash, passwordHash string, now time.Time) (WorkspaceUser, bool, error) {
	query := `UPDATE workspace_users
SET password_hash=$2, password_reset_hash='', password_reset_expires_at=NULL, session_version=session_version+1, updated_at=$3
WHERE password_reset_hash=$1 AND password_reset_hash<>'' AND password_reset_expires_at>$3
RETURNING ` + workspaceUserColumns
	return scanOptionalWorkspaceUser(r.db.QueryRowContext(ctx, query, hash, passwordHash, now))
}

func (r *PostgresRepository) ConsumeWorkspaceUserTOTPRecoveryCode(ctx context.Context, userID, hash string, now time.Time) (WorkspaceUser, bool, error) {
	query := `UPDATE workspace_users
SET totp_recovery_hashes = COALESCE((
  SELECT jsonb_agg(code)::text
  FROM jsonb_array_elements_text(totp_recovery_hashes::jsonb) AS recovery(code)
  WHERE code <> $2
), '[]'), updated_at=$3
WHERE id=$1 AND status='active' AND totp_enabled=TRUE
  AND EXISTS (
    SELECT 1 FROM jsonb_array_elements_text(totp_recovery_hashes::jsonb) AS recovery(code)
    WHERE code=$2
  )
RETURNING ` + workspaceUserColumns
	return scanOptionalWorkspaceUser(r.db.QueryRowContext(ctx, query, userID, hash, now))
}

func scanOptionalWorkspaceUser(row workspaceUserRowScanner) (WorkspaceUser, bool, error) {
	user, err := scanWorkspaceUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceUser{}, false, nil
	}
	if err != nil {
		return WorkspaceUser{}, false, err
	}
	return user, true, nil
}

func (r *PostgresRepository) findWorkspaceUser(ctx context.Context, query, value string) (WorkspaceUser, bool, error) {
	user, err := scanWorkspaceUser(r.db.QueryRowContext(ctx, query, value))
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceUser{}, false, nil
	}
	if err != nil {
		return WorkspaceUser{}, false, err
	}
	return user, true, nil
}

func (r *PostgresRepository) SaveWorkspaceUser(ctx context.Context, user WorkspaceUser) error {
	withNormalizedEmail(&user)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO workspace_users(id, email, email_normalized, display_name, avatar_data_url, status, role, balance_micros, concurrency_limit, rpm_limit, external_issuer, external_subject, department_id, totp_enabled, totp_secret_ciphertext, totp_recovery_hashes, password_hash, email_verified, email_verify_hash, email_verify_expires_at, email_verify_sent_at, password_reset_hash, password_reset_expires_at, password_reset_sent_at, session_version, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
ON CONFLICT(id) DO UPDATE SET
  email = EXCLUDED.email,
  email_normalized = EXCLUDED.email_normalized,
	  display_name = EXCLUDED.display_name,
	  avatar_data_url = EXCLUDED.avatar_data_url,
  status = EXCLUDED.status,
  role = EXCLUDED.role,
  balance_micros = EXCLUDED.balance_micros,
  concurrency_limit = EXCLUDED.concurrency_limit,
  rpm_limit = EXCLUDED.rpm_limit,
  external_issuer = EXCLUDED.external_issuer,
  external_subject = EXCLUDED.external_subject,
  department_id = EXCLUDED.department_id,
  totp_enabled = EXCLUDED.totp_enabled,
  totp_secret_ciphertext = EXCLUDED.totp_secret_ciphertext,
  totp_recovery_hashes = EXCLUDED.totp_recovery_hashes,
  password_hash = EXCLUDED.password_hash,
  email_verified = EXCLUDED.email_verified,
  email_verify_hash = EXCLUDED.email_verify_hash,
  email_verify_expires_at = EXCLUDED.email_verify_expires_at,
  email_verify_sent_at = EXCLUDED.email_verify_sent_at,
  password_reset_hash = EXCLUDED.password_reset_hash,
  password_reset_expires_at = EXCLUDED.password_reset_expires_at,
	  password_reset_sent_at = EXCLUDED.password_reset_sent_at,
	 session_version = EXCLUDED.session_version,
  updated_at = EXCLUDED.updated_at
		`, user.ID, user.Email, user.EmailNormalized, user.DisplayName, user.AvatarDataURL, user.Status, user.Role, user.BalanceMicros, user.ConcurrencyLimit, user.RPMLimit, user.ExternalIssuer, user.ExternalSubject, user.DepartmentID, user.TOTPEnabled, user.TOTPSecretCiphertext, marshalStringList(user.TOTPRecoveryHashes), user.PasswordHash, user.EmailVerified, user.EmailVerifyHash, user.EmailVerifyExpiresAt, user.EmailVerifySentAt, user.PasswordResetHash, user.PasswordResetExpiresAt, user.PasswordResetSentAt, user.SessionVersion, user.CreatedAt, user.UpdatedAt)
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" && (pqErr.Constraint == "workspace_users_email_key" || pqErr.Constraint == "workspace_users_email_normalized_unique") {
		return ErrUserEmailExists
	}
	return err
}

func (r *PostgresRepository) ListAuthIdentities(ctx context.Context, userID string) ([]AuthIdentity, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,user_id,issuer,subject,email,created_at,updated_at FROM auth_identities WHERE user_id=$1 ORDER BY issuer`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AuthIdentity, 0)
	for rows.Next() {
		var identity AuthIdentity
		if err := rows.Scan(&identity.ID, &identity.UserID, &identity.Issuer, &identity.Subject, &identity.Email, &identity.CreatedAt, &identity.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, identity)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) FindAuthIdentity(ctx context.Context, issuer, subject string) (AuthIdentity, bool, error) {
	var identity AuthIdentity
	err := r.db.QueryRowContext(ctx, `SELECT id,user_id,issuer,subject,email,created_at,updated_at FROM auth_identities WHERE issuer=$1 AND subject=$2`, issuer, subject).Scan(&identity.ID, &identity.UserID, &identity.Issuer, &identity.Subject, &identity.Email, &identity.CreatedAt, &identity.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthIdentity{}, false, nil
	}
	return identity, err == nil, err
}

func (r *PostgresRepository) SaveAuthIdentity(ctx context.Context, identity AuthIdentity) error {
	result, err := r.db.ExecContext(ctx, `INSERT INTO auth_identities(id,user_id,issuer,subject,email,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(issuer,subject) DO UPDATE SET email=EXCLUDED.email,updated_at=EXCLUDED.updated_at WHERE auth_identities.user_id=EXCLUDED.user_id`, identity.ID, identity.UserID, identity.Issuer, identity.Subject, identity.Email, identity.CreatedAt, identity.UpdatedAt)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return errors.New("external identity is already bound to another user")
	}
	return nil
}

func (r *PostgresRepository) DeleteAuthIdentity(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM auth_identities WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *PostgresRepository) ListRoleBindings(ctx context.Context) ([]RoleBinding, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, user_id, role, scope_type, scope_id, created_at, updated_at
FROM role_bindings
ORDER BY user_id ASC, scope_type ASC, scope_id ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RoleBinding, 0)
	for rows.Next() {
		var binding RoleBinding
		if err := rows.Scan(&binding.ID, &binding.UserID, &binding.Role, &binding.ScopeType, &binding.ScopeID, &binding.CreatedAt, &binding.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, binding)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveRoleBinding(ctx context.Context, binding RoleBinding) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO role_bindings(id, user_id, role, scope_type, scope_id, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(id) DO UPDATE SET
  user_id = EXCLUDED.user_id,
  role = EXCLUDED.role,
  scope_type = EXCLUDED.scope_type,
  scope_id = EXCLUDED.scope_id,
  updated_at = EXCLUDED.updated_at
`, binding.ID, binding.UserID, binding.Role, binding.ScopeType, binding.ScopeID, binding.CreatedAt, binding.UpdatedAt)
	return err
}

func (r *PostgresRepository) DeleteRoleBinding(ctx context.Context, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM role_bindings WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
