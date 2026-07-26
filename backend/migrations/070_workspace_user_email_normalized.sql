ALTER TABLE workspace_users ADD COLUMN IF NOT EXISTS email_normalized TEXT NOT NULL DEFAULT '';

-- 回填已有账号的归一化邮箱：小写、去 FQDN 根点、剥离 local 的 "+后缀"，
-- Gmail 系额外去掉 local 的点并把域名折叠为 gmail.com。
-- 与 controlplane.NormalizeEmailForAliasDedup 的规则保持一致。
UPDATE workspace_users
SET email_normalized = CASE
  WHEN split_part(rtrim(lower(email), '.'), '@', 2) IN ('gmail.com', 'googlemail.com')
    THEN COALESCE(NULLIF(replace(split_part(split_part(lower(email), '@', 1), '+', 1), '.', ''), ''),
                  split_part(lower(email), '@', 1)) || '@gmail.com'
  ELSE split_part(split_part(lower(email), '@', 1), '+', 1) || '@' || rtrim(split_part(lower(email), '@', 2), '.')
END
WHERE email_normalized = '' AND position('@' in email) > 1;

-- 同一收件箱只允许一个账号。历史冲突必须在迁移前显式清理；不能降级为
-- 普通索引，否则并发注册会绕过应用层查重并创建重复账号。
CREATE UNIQUE INDEX IF NOT EXISTS workspace_users_email_normalized_unique
  ON workspace_users(email_normalized) WHERE email_normalized <> '';
