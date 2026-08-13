---
name: code-review-testing
description: Author and run risk-based tests for AsterRouter changes across the Go backend and Vue frontend. Use when adding or reviewing behavior, fixing a regression, changing API routes, repositories, policies, authentication, gateway routing, billing, plugins, UI state, or CI test coverage.
---

# AsterRouter Test Authoring

Read `docs/test/v1/README.md` and `docs/test/v1/scenario-registry.json` before broad or cross-surface changes. Treat them as the source of truth for proof levels, priorities, environments, and delivery gates.

## Select the test layer

1. Identify the changed user-visible behavior, trust boundary, persistence effect, and failure mode.
2. Search adjacent tests and reuse their fixtures before adding helpers.
3. Add the narrowest test that proves the contract, then run broader checks.
4. Prefer integration tests at stable boundaries over tests coupled to implementation details.

Use these project conventions:

- Put Go tests in sibling `*_test.go` files. Use table-driven unit tests for pure validation and policy rules.
- Use `httptest` for HTTP routes, authentication middleware, gateway forwarding, streaming, and upstream failures.
- Use memory repositories for fast domain tests; use PostgreSQL for SQL, transactions, constraints, migration compatibility, and restart persistence.
- Add frontend unit or component tests for deterministic state and rendering. Use browser tests for routing, authentication, forms, responsive behavior, and multi-step workflows.
- Give every Playwright test one unique `@e2e-*` scenario ID and register its owner, fixture, proof level, claim boundary, routes, and delivery gates. New user-visible routes must have both a surface contract and a vertical journey.
- Do not add production-only test hooks. Extract a real interface only when it improves production design.
- Mock official services, identity providers, mail, object storage, plugins, and model upstreams by default. Never send secrets or test traffic to production.

## Required assertions

Verify observable outcomes, not only status codes:

- response status, schema, error category, and relevant headers;
- repository state, transaction atomicity, and restart persistence where applicable;
- audit log, usage record, gateway trace, alert, or notification side effects;
- authorization scope and non-disclosure across users, departments, profiles, and surfaces;
- secret masking, one-time secret return, signature validation, and fail-closed behavior;
- streaming termination, cancellation, retry/failover, and idempotency when relevant.

Every bug fix must include a regression test that fails for the original defect. Every change to auth, RBAC, gateway policy, billing, migrations, backup/restore, or plugin trust must include at least one negative-path test.

## Run checks from narrow to broad

Backend:

```bash
cd backend
go test ./internal/<package> -run '<TestName>' -count=1
go test ./...
```

Run `go test -race ./...` for concurrency, schedulers, shared caches, rate limits, streaming, or repository changes.

Frontend baseline:

```bash
cd frontend
npm run typecheck
npm run build
npm run check:enterprise-surface
npm run check:e2e-coverage
```

Run `test:e2e:pr`, `test:e2e:full`, or `test:e2e:release` according to the gate declared by the registry. Do not select release scenarios with hand-maintained grep lists, and do not claim frontend regression coverage based on typecheck or build alone.

## Report evidence

List the exact commands run, their result, skipped environment-dependent tests, and any untested risk. Do not hide unrelated failures or broaden the change to fix them without user authorization.
