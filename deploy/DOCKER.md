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
