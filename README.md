# AsterRouter

AsterRouter is an AI Gateway Control Plane. This repository is moving from validation into a productized control-plane implementation for local, private, and service-center connected deployments.

## Product Build

The current product build provides:

- Single-origin routes for `/setup`, `/admin`, `/portal`, `/api/v1/*`, and `/v1/*`.
- Basic settings APIs using a key/value settings model.
- Provider Connection, Project/Application, API Key, and Audit Log control-plane APIs.
- Admin settings UI for site, profile, OIDC, data governance, service-center mode, and system update operations.
- Admin Console pages for overview, providers, projects/apps, API keys, plugin center, audit logs, and system settings.
- Built-in plugin registry with free core, profile bundle, and paid add-on entitlement gates.
- Employee Portal workspace summary backed by the same control-plane data.
- Gateway API key authentication for `/v1/models` and `/v1/chat/completions`.
- OpenAI-compatible local fallback response with model allowlist validation and audit logging.
- OpenAI-compatible provider forwarding when a matching Provider Connection has an encrypted secret configured.
- Setup wizard for choosing `personal`, `relay_operator`, or `enterprise`.
- English-first i18n with Simplified Chinese as the second locale.

## Development

Install frontend dependencies once:

```bash
cd frontend
npm install
```

Run backend and frontend together for local UI development:

```bash
bash scripts/dev.sh
```

The development frontend listens on `http://localhost:5173` and proxies `/api/*` and `/v1/*` to the backend on `http://localhost:8080`.

Backend:

```bash
cd backend
go test ./...
go run ./cmd/asterrouter
```

Frontend:

```bash
cd frontend
npm install
npm run build
npm run dev
```

Single-origin preview:

```bash
cd frontend
npm run build
cd ../backend
go run ./cmd/asterrouter
```

Then open `http://localhost:8080/setup`, `http://localhost:8080/admin/settings`, or `http://localhost:8080/portal`.

Docker single-service deployment:

```bash
docker compose up --build
```

The container builds the frontend and serves it from the Go backend, so one route origin is enough for private deployments.

Environment:

```bash
export DATABASE_URL="postgres://asterrouter:asterrouter@localhost:5432/asterrouter?sslmode=disable"
export ASTER_ADMIN_TOKEN="change-me"
export ASTER_ADMIN_USERNAME="admin"
export ASTER_ADMIN_PASSWORD="change-me"
export ASTER_PROFILE="enterprise"
export ASTER_SECRET_KEY="replace-with-a-stable-random-secret"
export ASTER_BUILD_TYPE="source"
export ASTER_UPDATE_MANIFEST_URL=""
export ASTER_ALLOW_RESTART="false"
```

If `DATABASE_URL` is not set, the backend uses an in-memory settings repository for local development preview. PostgreSQL remains the intended persistent store.
Use a stable `ASTER_SECRET_KEY` before adding Provider secrets; changing it prevents existing encrypted Provider secrets from being decrypted.
The local login page uses `ASTER_ADMIN_USERNAME` and `ASTER_ADMIN_PASSWORD`. If `ASTER_ADMIN_PASSWORD` is empty, it falls back to `ASTER_ADMIN_TOKEN`; if both are empty, the local development default is `admin/admin`.

## Linux Release Deployment

AsterRouter ships Linux-only GitHub Release assets for `amd64` and `arm64`.

```bash
curl -sSL https://raw.githubusercontent.com/astercloud/asterrouter/main/deploy/install.sh | sudo bash
```

The installer deploys to `/opt/asterrouter`, installs the `asterrouter` command wrapper, and creates `/etc/asterrouter/asterrouter.env` when missing. Production release builds refuse to start until `DATABASE_URL`, a stable `ASTER_SECRET_KEY`, and an admin password or token are configured.

Common operations:

```bash
asterrouter status
asterrouter logs -n 200
asterrouter upgrade
asterrouter upgrade -v v0.1.0
asterrouter rollback v0.1.0
```

System update:

- `ASTER_BUILD_TYPE=source` disables in-place binary replacement and reports manual update guidance.
- `ASTER_BUILD_TYPE=release` enables one-click update when `ASTER_UPDATE_MANIFEST_URL` points to a trusted JSON manifest with a matching `os`/`arch` asset and `sha256`.
- `ASTER_ALLOW_RESTART=true` allows the Admin Console restart action to terminate the process so an external supervisor can restart it.
