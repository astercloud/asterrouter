---
name: remote-tests
description: Validate AsterRouter outside its in-memory fast path using PostgreSQL, the production single-origin build, Docker, Linux release artifacts, or an authorized remote deployment. Use for repository and migration changes, backup/restore, deployment, release, networking, health checks, or environment-specific failures.
---

# AsterRouter Environment Tests

Read the environment matrix and release gates in `docs/test/v1/README.md` before testing. Start with an isolated local environment and move outward only when the narrower environment passes.

## Choose the environment

1. Use memory mode for fast service and route tests that do not claim persistence coverage.
2. Use PostgreSQL 16 for SQL, constraints, transactions, cleanup, exports, migrations, backup, and restart persistence.
3. Use the production single-origin build for frontend asset serving, SPA fallback, API routing, and runtime configuration.
4. Use Docker or Linux artifacts for image/user/architecture, signal handling, packaging, checksum, install, upgrade, and rollback behavior.
5. Use a remote deployment only when the user has authorized the target and the test data is isolated.

## PostgreSQL workflow

- Use a dedicated test database and a unique run identifier. Never point `ASTER_TEST_DATABASE_URL` or `DATABASE_URL` at production.
- Exercise both a clean schema and an upgrade fixture representing the oldest supported release.
- Verify repository initialization twice to prove idempotency.
- Test constraints, transaction rollback, concurrent writes, and process restart persistence.
- Compare runtime-created schema with `backend/migrations`; schema drift is a release blocker.
- Clean only records created by the run. Do not delete volumes or shared databases without explicit confirmation.

Run PostgreSQL-enabled tests with an isolated URL:

```bash
cd backend
ASTER_TEST_DATABASE_URL='<test-postgres-url>' go test ./... -count=1
```

## Production and release workflow

For the single-origin application, build the frontend, start the backend with `ASTER_FRONTEND_DIR=../frontend/dist`, then verify `/health`, `/ready`, SPA deep links, `/api/v1/*`, and `/v1/*`.

For containers and release artifacts, verify:

- non-root runtime and declared architecture;
- required production configuration fails closed;
- health/readiness transitions and graceful `SIGTERM` shutdown;
- frontend assets and API routes share one origin;
- checksums, `--version`, archive contents, install, upgrade, rollback, and restart;
- backup creation, restore into an empty database, and restored critical-record counts.

Use fake upstream model APIs and fake official services. Cover normal JSON, server-sent streaming, timeout, malformed payload, 429, 5xx, and connection interruption without contacting a live provider.

## Remote deployment discipline

1. Record the target, version, commit, configuration class, database version, and test run ID.
2. Start with read-only health/version checks.
3. Create isolated identities, keys, providers, and records for mutation tests.
4. Capture request IDs and relevant logs without storing credentials or model content.
5. Remove only test-owned records and report anything left behind.

Do not skip an incompatible environment silently. State the environment, reason, owner, and follow-up issue. A critical path that only passes in memory mode is not deployment-ready.
