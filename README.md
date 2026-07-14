<div align="center">

# AsterRouter

**Spend less on AI. Keep every request under control.**

One gateway for multiple AI providers, automatic cost optimization, enterprise governance, and managed delivery.

[English](./README.md) · [简体中文](./README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/astercloud/asterrouter?style=flat-square)](https://github.com/astercloud/asterrouter/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/astercloud/asterrouter/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/astercloud/asterrouter/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/astercloud/asterrouter?style=flat-square)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square)](./backend/go.mod)

</div>

## One gateway. Better AI economics.

AsterRouter gives your team one controlled AI gateway today and is evolving into a single access layer for text, image, audio, and video AI. Connect the providers you already use, issue secure access to teams and products, and see where every token and dollar goes. The multimodal roadmap extends the same controls to media tasks and artifacts.

The next stage goes beyond cost reporting: pricing plugins keep supplier rates current, and cost-aware routing chooses the lowest-cost route that still meets your policy, health, and capacity requirements.

| | What you get |
| --- | --- |
| Lower cost | Compare eligible provider prices and route each request toward the best available economics. |
| One integration | Keep applications on stable APIs for streaming, media, and long-running jobs while providers and routes change behind the gateway. |
| Reliable traffic | Fail over around unhealthy, rate-limited, or capacity-constrained routes. |
| Enterprise control | Manage team access, model permissions, budgets, alerts, audit trails, and data retention. |
| Flexible delivery | Self-host on your infrastructure or use private deployment and managed operations. |

## How AsterRouter saves money

1. **Connect your providers.** Keep using authorized provider accounts and standard APIs.
2. **Keep prices current.** Price-source plugins sync supplier rates from supported APIs, signed feeds, imports, or the optional browser extension.
3. **Route with constraints.** AsterRouter first protects policy, availability, and capacity, then chooses the lowest expected-cost eligible route.
4. **Prove the result.** Usage and route evidence show the selected price, alternatives, actual cost, and measurable savings.

> Cost-aware quote selection, third-party price plugins, the savings ledger, and the browser extension are roadmap capabilities. The routing, governance, usage, cost allocation, audit, and plugin foundations are available today.

## Built for real organizations

**Engineering teams** get one endpoint and one key workflow across providers.

**Finance and operations** get cost allocation by user, department, group, key, and model, plus budgets and alerts.

**Security teams** get private deployment, encrypted secrets, scoped access, audit evidence, retention controls, and offline-capable operations.

**Platform owners** get route health, fallback, capacity controls, backups, upgrades, diagnostics, and a plugin system for provider-specific extensions.

## Deliver AI to your team or your customers

AsterRouter can power AI access behind internal tools and customer-facing products without forcing every business service to become an AI gateway.

| Scenario | How AsterRouter helps |
| --- | --- |
| Internal AI platform | Give employees, departments, services, and automation scoped AI access with shared governance and cost controls. |
| SaaS and desktop products | Keep your existing customer login while AsterRouter enforces AI policy, quota, routing, and usage behind the product. |
| Content and media products | Support immediate image/audio responses and native streams alongside queued long-running jobs, without rebuilding provider-capacity scheduling, artifact delivery, and media billing in every product. |
| AI API and developer platforms | Issue AsterRouter API Keys for a branded, multi-model endpoint without exposing upstream credentials. |
| Partner and OEM delivery | Isolate tenants, apply partner-specific policy, and report usage back to the system that owns the commercial relationship. |

```text
Customer access
  ├─ AsterRouter API Key
  └─ Existing platform login and delegated credential
                       ↓
            AsterRouter Gateway
                       ↓
                AI providers
```

Choose the access model that fits the product. For an AI API, relay, agent platform, or trusted server integration, AsterRouter can issue and govern Bearer API Keys directly. For a SaaS or OEM product with its own users, login sessions, subscriptions, and entitlements, the product remains the source of truth and delegates only the AI access context. Both paths enter the same policy, account scheduling, streaming, usage, cost, and audit pipeline. Neither exposes provider credentials to customers.

AsterRouter API Keys, policy, routing, metering, and isolation are available today. AI Platform supports both a short-lived HMAC-signed delegated context for a trusted product backend and an RS256 JWT/JWKS delegated credential for a product that keeps its own login system. Both bind one Platform Tenant and non-human Gateway Principal, then can only narrow model, QPS, and monthly-token ceilings. Platform operators can also deliver signed, metering-only usage events to an integration-bound HTTPS endpoint: usage and the delivery event commit together, while leased background retries, dead letters, and manual requeue keep callback failures out of the gateway request. OIDC introspection, revocation events, platform-auth plugins, key-level modality scopes, and distributed concurrency limits remain roadmap capabilities.

## One trusted Core, extended for each scenario

Different customers need different workflows, but they should not create different gateways. AsterRouter keeps security-critical decisions in one Core and adds optional integrations through plugins.

| Trusted Core | Scenario plugins |
| --- | --- |
| API Key lifecycle, canonical auth context, and tenant isolation | Platform authentication, JWT/JWKS, introspection, directory, and SSO adapters |
| Model policy, quota, budget, and risk controls | SaaS plans, subscriptions, and entitlement sync |
| Provider routing, fallback, and cost metering | Supplier pricing and browser-assisted collection |
| Direct execution for sync/streaming requests, fair queues for explicit async jobs, artifact policy, usage, and billing state | Provider protocols for GPT Image, Gemini, Midjourney-compatible services, Jimeng, and others |
| Integration-bound usage event delivery, signing, retry, dead letter, and requeue | Customer billing, entitlement, ERP, CRM, and data-warehouse mappings |
| One Core from a single server to Kubernetes | Replaceable queue and artifact adapters for Redis, S3-compatible storage, Cloudflare R2, Alibaba Cloud OSS, and future brokers |
| Usage, trace, audit, and data governance | Notifications, exports, branding, and customer workflows |

Every credential source converges on one gateway pipeline: protocol normalization, authorization, quota hold, candidate planning, provider-account scheduling, capacity control, protocol execution, usage settlement, and audit. Plugins contribute provider or business facts through controlled interfaces; they cannot bypass that pipeline, issue unrestricted credentials, read unrelated secrets, or choose routes outside the trusted scheduler.

## Available today

- OpenAI-compatible model listing and Chat Completions, including streaming.
- Multi-provider routing with priority, weight, capacity, RPM/TPM limits, cooldown, circuit breaking, sticky routing, and fallback.
- Workspace Keys, model allowlists, rate limits, token quotas, budgets, and overage controls.
- Usage analytics, cost allocation, alerts, route traces, policy evidence, audit logs, and exports.
- Enterprise sign-in and access governance with OIDC, Feishu/Lark, DingTalk, departments, groups, and scoped roles.
- Admin Console, employee Portal, plugin center, backup and restore, diagnostics, and verified release updates.
- Four mutually exclusive deployment roles with English and Simplified Chinese interfaces: Personal, Relay Operator, Enterprise, and AI Platform. The AI Platform provides its own control surface, Platform Tenant, Gateway Principal, tenant-bound workspace/service API Keys, HMAC and RS256 JWT/JWKS delegated-access integrations, plus reliable signed HTTPS usage delivery with retries, dead letters, and requeue. It does not create an external product's users, sessions, subscriptions, orders, or balances. OIDC introspection, entitlement adapters, media operations, and asynchronous jobs remain V4 roadmap capabilities. Each new production instance chooses one immutable deployment role; deploy a separate instance for another business model.

The current gateway exposes OpenAI-compatible Models and Chat Completions. Responses, Embeddings, Anthropic Messages, Gemini, realtime sessions, image generation and editing, audio, video, media uploads, asynchronous jobs, and artifact delivery are roadmap capabilities and are not presented as available today.

The multimodal roadmap separates immediate and queued work: synchronous JSON, native image/audio streams, and realtime sessions execute only when capacity is available, while explicit asynchronous jobs enter a durable fair queue. PostgreSQL and Redis form the minimum production infrastructure, with no mandatory external message broker. Start with the same Core on one server, then split API, scheduler, modality workers, reconciliation, and artifact delivery into Kubernetes workloads when traffic becomes bursty. Worker pods and optional burst nodes can scale down during quiet periods, while provider capacity, tenant budgets, and cost ceilings remain hard limits. Media can stay local, use S3-compatible storage such as AWS S3, Cloudflare R2, or MinIO, use Alibaba Cloud OSS, or be delivered to customer-owned storage.

## Quick start

### Choose a deployment role

Choose one business deployment role during installation. This controls the initial console, business objects, roles, and default extensions; it is not a cosmetic home-page choice or a set of composable feature flags. Choose by the owner of the business relationship, commercial settlement, and identity facts, not by whether an integration needs API Keys, streaming, or a particular model.

| Choose this deployment role when | Business and identity source of truth | Initial console | It deliberately does not include |
| --- | --- | --- | --- |
| **Personal**: one person or a small team needs a focused gateway | Your own Workspace and collaboration | `/console` | Enterprise organization management, relay billing, and external-product integration |
| **Relay Operator**: you operate an existing customer, balance, plan, and risk workflow | You own the customer, plan, balance, and risk workflow | `/operator` and `/customer` | Enterprise employee management and external-product tenancy |
| **Enterprise**: you govern employee and service access inside one organization | Your organization owns its employee, directory, and access facts | `/admin` and `/portal` | Relay resale objects and external end-user identity or subscription management |
| **AI Platform**: you operate developer APIs or add AI to SaaS/OEM products | You own the gateway tenant, caller, and integration boundary; the connected product owns end users | `/platform` | Enterprise HR objects, relay balances/plans, and external end-user accounts or sessions |

AI Platform is separate from relay operations. Both can issue or accept API credentials, but a relay operator owns customer balances, plans, and risk workflows. An AI platform owns the gateway boundary for developer API Keys or delegated product access; the connected product remains the source of truth for its users, sessions, subscriptions, and orders. Enterprise owns employee and department governance; Personal owns only its workspace. These are different business roots, not four pages of one customer model.

Linux Release installation requires `--deployment` (or `ASTERROUTER_DEPLOYMENT_ROLE`); Docker and source development use `/setup`, which requires an explicit choice and does not preselect a role. The selection is persisted in PostgreSQL on first start and fixed for that instance. At later starts, a configured role must match the persisted role or startup fails; an environment variable can never switch Enterprise to Platform, or any other role. This protects Provider, credential, usage, and audit data from being mixed across business models. Run another instance when a second model is needed. Existing multi-profile installations remain compatible but their profile configuration is frozen. Multi-profile operation for new deployments is a future migration capability, gated on end-to-end `profile_scope`, explicit Provider Sharing Binding, and tenant isolation. The [deployment-role guide](./docs/roadmap/v4/profile-bundles-and-installation.md) explains the boundary.

### Linux release

```bash
curl -sSL https://raw.githubusercontent.com/astercloud/asterrouter/main/deploy/install.sh | sudo bash -s -- install --deployment enterprise
```

Replace `enterprise` with `personal`, `relay_operator`, or `platform` as appropriate. The installer deploys AsterRouter to `/opt/asterrouter` and creates the service configuration under `/etc/asterrouter`. Production startup requires PostgreSQL, a stable encryption key, and an administrator password or token. New Linux installations without a role are rejected before download or configuration changes.

### Docker

```bash
docker compose up --build
```

Open `http://localhost:8080/setup` to choose one deployment role and review its included and excluded business boundaries before completing setup. For non-interactive deployment, set `ASTER_DEPLOYMENT_ROLE=platform`; the older matching `ASTER_PROFILES=platform` and `ASTER_DEFAULT_PROFILE=platform` pair remains compatible. The choice is persisted on first boot and cannot be changed. Deploy another instance for another role.

## Private deployment and managed delivery

AsterRouter supports three delivery models:

- **Self-managed:** deploy and operate AsterRouter in your own environment.
- **Private delivery:** deploy into a customer-controlled network with installation, migration, and acceptance support.
- **Managed operations:** ongoing upgrades, backups, health checks, diagnostics, and operational support while the customer retains control of data and provider credentials.

Every delivery model can start with a low-cost single-server deployment for predictable traffic, then move the same Core and data model to role-based Kubernetes workloads. Kubernetes scales AsterRouter pods and optional worker nodes around bursts; it never treats upstream provider concurrency as unlimited capacity.

The Core remains usable when official online services are unavailable. Prompts, responses, Provider Secrets, Workspace Keys, detailed gateway usage, and browser-captured supplier sessions are not uploaded by the official Feed synchronization path.

## Project links

- [Releases](https://github.com/astercloud/asterrouter/releases)
- [Build status](https://github.com/astercloud/asterrouter/actions)
- [Deployment environment template](./deploy/asterrouter.env.example)
- [简体中文说明](./README.zh-CN.md)

<details>
<summary><strong>Local development</strong></summary>

Install frontend dependencies and start both services:

```bash
cd frontend
npm install
cd ..
bash scripts/dev.sh
```

The frontend runs at `http://localhost:5173` and proxies API traffic to the backend at `http://localhost:8080`.

Run backend tests:

```bash
cd backend
go test ./...
```

Build the frontend:

```bash
cd frontend
npm run build
```

See the [deployment environment template](./deploy/asterrouter.env.example).

</details>

## License

AsterRouter is licensed under the [Apache License 2.0](./LICENSE).
