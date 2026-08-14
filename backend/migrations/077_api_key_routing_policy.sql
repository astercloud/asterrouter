ALTER TABLE routing_policies
  ADD COLUMN IF NOT EXISTS is_default BOOLEAN NOT NULL DEFAULT false;

UPDATE routing_policies AS policy
SET is_default = true
WHERE policy.id IN (
  SELECT DISTINCT ON (route_group) id
  FROM routing_policies
  WHERE status = 'active'
  ORDER BY route_group, updated_at DESC, id
)
AND NOT EXISTS (
  SELECT 1
  FROM routing_policies AS existing
  WHERE existing.route_group = policy.route_group
    AND existing.status = 'active'
    AND existing.is_default
);

DROP INDEX IF EXISTS routing_policies_one_active_per_group_idx;

CREATE UNIQUE INDEX IF NOT EXISTS routing_policies_one_default_per_group_idx
  ON routing_policies(route_group)
  WHERE status = 'active' AND is_default;

ALTER TABLE api_keys
  ADD COLUMN IF NOT EXISTS routing_policy_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS api_keys_routing_policy_id_idx
  ON api_keys(routing_policy_id)
  WHERE routing_policy_id <> '';
