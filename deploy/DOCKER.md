# Docker 部署

## 内置 PostgreSQL

在仓库根目录执行：

```bash
cp .env.example .env
# 编辑 .env，至少替换数据库密码、管理员密码和 ASTERROUTER_SECRET_KEY
docker compose up -d --build
docker compose ps
docker compose logs -f asterrouter
```

默认只绑定 `127.0.0.1:8080`。通过 `ASTERROUTER_BIND_ADDRESS` 和
`ASTERROUTER_PORT` 修改监听地址或端口。首次打开
`http://localhost:8080/setup`，选择一个部署角色；也可以在 `.env` 中设置
`ASTERROUTER_DEPLOYMENT_ROLE` 进行无人值守初始化。

应用数据和 PostgreSQL 数据分别保存在 `asterrouter_data`、`postgres_data`
命名卷中。升级时使用 `docker compose pull`（使用远程镜像时）或
`docker compose up -d --build`（从源码构建），不要删除这两个卷。

## 外置 PostgreSQL

当 PostgreSQL 由云服务、Kubernetes 或宿主机提供时，使用：

```bash
cp .env.example .env
# 设置 ASTERROUTER_DATABASE_URL，例如：
# ASTERROUTER_DATABASE_URL=postgres://user:password@db.example.com:5432/asterrouter?sslmode=require
docker compose -f deploy/docker-compose.standalone.yml up -d --build
```

外置数据库模式不会启动或暴露 PostgreSQL 容器，但仍将插件、备份、诊断和本地产物保存到 `asterrouter_data` 命名卷。

## 双实例 HA 拓扑

仓库提供一个两实例 Compose 模板，使用外部 PostgreSQL 作为共享事实源，并由 Nginx 将新请求分发到两个应用实例：

```bash
cp .env.example .env
# 在 .env 中设置同一 PostgreSQL URL、稳定的 ASTERROUTER_SECRET_KEY 和管理员凭据
docker compose -f deploy/docker-compose.ha.yml up -d --build
docker compose -f deploy/docker-compose.ha.yml ps
curl --fail http://127.0.0.1:8080/ready
```

模板会为两个实例使用独立的本地产物卷，避免插件缓存、备份和诊断文件互相覆盖；Provider Secret、Key 和业务数据库只通过环境变量与 PostgreSQL 进入实例。Nginx 已关闭请求/响应缓冲，并配置了长连接超时，适用于 SSE 和长时间请求。`/ready` 会等待两个应用实例健康后才让代理容器进入可用状态。

如果需要 Durable Job 和跨实例路由亲和，将 `ASTERROUTER_JOBS_QUEUE_DRIVER` 与 `ASTERROUTER_ROUTING_AFFINITY_DRIVER` 设置为 `redis`，并提供 `ASTERROUTER_REDIS_URL`。仅使用默认的内存队列时，文本网关请求可以通过代理运行，但不应把 Job 队列或实例本地状态当作跨实例共享事实。

代理只对新请求提供被动故障切换。已经开始向客户端发送响应的流不会被 Nginx 伪装成成功或自动重放；客户端应依据 Request ID 和错误结果决定是否重试。

如需 Prometheus 文本指标，设置独立的 `ASTERROUTER_METRICS_TOKEN` 后重启实例，再通过代理执行一次连通性检查：

```bash
curl --fail -H "Authorization: Bearer ${ASTERROUTER_METRICS_TOKEN}" http://127.0.0.1:8080/metrics
```

指标端点默认关闭，Token 为空时返回 404。当前指标包含有限基数的 HTTP Route、Method、Status、耗时、在途请求、Readiness、容量准入结果和 Provider Account 水位，不包含 Key、Tenant、Application、Credential、Prompt、Response 或底层依赖错误。

双实例监控应在内部网络直接抓取 `asterrouter-a:8080` 和 `asterrouter-b:8080`，不要只抓代理入口。示例位于 `deploy/prometheus/prometheus.example.yml`，Token 从 Secret 文件读取。告警规则位于 `deploy/prometheus/alerts.yml`，覆盖实例可用性、容量仓储错误、持续容量拒绝、Provider 高水位和熔断；Grafana 模板位于 `deploy/grafana/asterrouter-capacity-dashboard.json`。部署方应按实际 Prometheus Job 标签和通知渠道加载规则，并在预生产环境执行 `deploy/HA_RUNBOOK.md` 中的容量告警演练。

## GitHub Container Registry

Docker 镜像使用独立的手动 GitHub Actions 工作流发布，不会阻断普通 CI 或 GitHub Release：

1. 先完成正常的 `v*` Git tag 和 GitHub Release；手动工作流会检查 Release 已存在。
2. 打开仓库的 `Actions` 页面，选择 `Docker Release`。
3. 点击 `Run workflow`，输入已经存在且包含 Docker 部署文件的 tag，例如 `v1.2.3`。
4. 仅在需要移动稳定入口时勾选 `publish_latest`。

工作流会构建并发布 amd64/arm64 镜像：

```bash
docker pull ghcr.io/astercloud/asterrouter:1.2.3
ASTERROUTER_IMAGE=ghcr.io/astercloud/asterrouter:1.2.3 docker compose up -d
```

镜像发布前会经过 release container acceptance，发布后还会检查多架构 manifest。GitHub Actions 使用 `GITHUB_TOKEN` 登录 GHCR，不需要额外的长期 Docker 密钥。

首次使用前检查仓库设置：

- 当前仓库默认 `GITHUB_TOKEN` 保持只读即可，不必扩大所有工作流权限；`Docker Release` 自身只申请 `contents: read` 和 `packages: write`。如果组织策略禁止工作流提升 Packages 权限，需要组织管理员单独放开。
- 如果组织限制可用 Actions，需要允许 `actions/checkout`、`actions/upload-artifact` 和 `docker/*` 官方 Actions。
- 首次发布后，在 GitHub Package 设置中选择镜像可见性。公开拉取需要将 Package 设为 Public；保持 Private 时，拉取方需要先执行 `docker login ghcr.io`。
- `Docker Release` 文件必须先进入默认分支，GitHub 才会在 Actions 页面显示手动运行按钮。

## 生产注意事项

- `ASTERROUTER_SECRET_KEY` 必须跨重启保持不变，否则会导致会话和加密数据失效。
- 不要在生产环境启用 `ASTERROUTER_DEMO_MODE`，也不要使用示例密码。
- 需要公网访问时，建议在反向代理后运行并启用 TLS；容器本身只提供 HTTP。
- `docker compose down` 不会删除命名卷；清理数据必须显式执行 `docker compose down -v`。
- 健康检查使用 `/ready`，会同时验证 PostgreSQL 和应用存储。查看状态：
  `docker inspect --format '{{json .State.Health}}' asterrouter`。
