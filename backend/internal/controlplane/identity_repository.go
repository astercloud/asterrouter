package controlplane

import (
	"context"
	"database/sql"
	"sort"
)

func (r *MemoryRepository) ListWorkspaceUsers(context.Context) ([]WorkspaceUser, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]WorkspaceUser, 0, len(r.workspaceUsers))
	for _, user := range r.workspaceUsers {
		user.ProjectCount = r.projectCountForUser(user.ID)
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspaceUsers[user.ID] = user
	return nil
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

func (r *MemoryRepository) projectCountForUser(userID string) int {
	projects := map[string]struct{}{}
	for _, binding := range r.roleBindings {
		if binding.UserID == userID && binding.ScopeType == RoleScopeProject && binding.ScopeID != "" {
			projects[binding.ScopeID] = struct{}{}
		}
	}
	return len(projects)
}

func (r *PostgresRepository) ListWorkspaceUsers(ctx context.Context) ([]WorkspaceUser, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT u.id, u.email, u.display_name, u.status, u.role,
       COUNT(DISTINCT rb.scope_id) FILTER (WHERE rb.scope_type = 'project' AND rb.scope_id <> '') AS project_count,
       u.created_at, u.updated_at
FROM workspace_users u
LEFT JOIN role_bindings rb ON rb.user_id = u.id
GROUP BY u.id, u.email, u.display_name, u.status, u.role, u.created_at, u.updated_at
ORDER BY u.status ASC, u.email ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceUser
	for rows.Next() {
		var user WorkspaceUser
		if err := rows.Scan(&user.ID, &user.Email, &user.DisplayName, &user.Status, &user.Role, &user.ProjectCount, &user.CreatedAt, &user.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) SaveWorkspaceUser(ctx context.Context, user WorkspaceUser) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO workspace_users(id, email, display_name, status, role, created_at, updated_at)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(id) DO UPDATE SET
  email = EXCLUDED.email,
  display_name = EXCLUDED.display_name,
  status = EXCLUDED.status,
  role = EXCLUDED.role,
  updated_at = EXCLUDED.updated_at
`, user.ID, user.Email, user.DisplayName, user.Status, user.Role, user.CreatedAt, user.UpdatedAt)
	return err
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
	var out []RoleBinding
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
