CREATE UNIQUE INDEX IF NOT EXISTS workspace_users_email_verify_hash_unique
  ON workspace_users(email_verify_hash) WHERE email_verify_hash <> '';

CREATE UNIQUE INDEX IF NOT EXISTS workspace_users_password_reset_hash_unique
  ON workspace_users(password_reset_hash) WHERE password_reset_hash <> '';
