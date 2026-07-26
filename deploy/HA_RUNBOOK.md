# 双实例运行手册

本文适用于 `deploy/docker-compose.ha.yml`：两个应用实例通过 Nginx 接收流量，共享外部 PostgreSQL。它只描述已经随仓库提供的部署能力；Prometheus 抓取、告警规则和 Grafana 看板提供配置模板，但监控服务、通知渠道和自动扩缩容不包含在该模板中。

## 启动前检查

1. 为两个实例配置同一个 `ASTERROUTER_DATABASE_URL` 和稳定的 `ASTERROUTER_SECRET_KEY`。
2. 确认 PostgreSQL 使用 TLS、备份策略和最小权限的服务账号。
3. 在受控网络中暴露代理端口。模板默认仅绑定 `127.0.0.1`，公网访问必须由外层 TLS 代理负责。
4. 若使用 Durable Job 或跨实例路由亲和，配置 `ASTERROUTER_REDIS_URL`，并将 Job 队列和路由亲和驱动设为 `redis`。
5. 不要把上游账号、密钥或数据库连接串写入 Compose 文件、镜像、日志或工单。

## 启动与验收

```bash
docker compose -f deploy/docker-compose.ha.yml up -d --build
docker compose -f deploy/docker-compose.ha.yml ps
curl --fail http://127.0.0.1:8080/health
curl --fail http://127.0.0.1:8080/ready
```

`/health` 只表示进程存活。`/ready` 会检查必要存储依赖；返回非 2xx 时，负载均衡器和外部探针不得再向该实例派发新请求。

仓库提供隔离的完整拓扑验收，覆盖 PostgreSQL 16、两个非 root 应用容器、Nginx、任一实例退出后的连续请求以及两个实例都停止时的失败语义：

```bash
ASTER_HA_TEST_ARTIFACT_DIR=/tmp/asterrouter-ha-evidence \
bash scripts/test-ha-container.sh
```

该脚本会创建并清理自己命名的测试容器、网络和卷，只能在隔离的 Docker 环境运行，不得指向现有数据库或复用生产 Compose project。成功报告和容器日志写入指定证据目录。

完成启动后，以管理员身份执行一次受控文本请求，并在管理界面的 Trace、Usage 与 Audit 中核对 Request ID、Route、Provider Account 和最终状态。不要在运行手册或监控标签中记录完整 Key、Prompt、Response 或连接串。

## 单实例故障

1. 记录故障实例的容器日志、时间范围和最近的 Request ID。
2. 确认另一实例的 `/ready` 返回成功，再从代理入口发起一条新的只读或测试请求。
3. 重启故障实例前，检查 PostgreSQL 连接、迁移状态和本地卷可写性。
4. 恢复后观察新请求、Usage、Trace 和 Audit 是否只出现一次；不要通过重放已经部分返回的流来“补偿”。

Nginx 只会对尚未向客户端开始响应的新请求做被动故障切换。已经开始输出的流在实例或上游中断时应由客户端按 Request ID 和错误结果处理，不能由代理自动重放。

## 数据库不可用

1. 保持 `/health` 作为进程存活信号，使用 `/ready` 作为流量接收信号。
2. 当任一必要 Repository 检查失败时，`/ready` 返回 503；响应不会包含数据库驱动或连接细节，完整原因仅在实例日志中。
3. 从代理摘除未就绪实例，检查数据库可用性、连接数、存储空间和最近的迁移记录。
4. 数据库恢复后，等待每个实例连续通过 `/ready`，再逐个恢复流量。
5. 对数据库不可用期间的失败请求，按 Operation、Attempt、Usage 和 Billing Hold 记录核对最终状态；不把未知上游结果重记为成功。

## 监控与告警

模板提供 `/health`、`/ready` 和默认关闭的 `/metrics`。设置独立的 `ASTERROUTER_METRICS_TOKEN` 后，Prometheus 使用 Bearer Token 抓取有限基数的 HTTP、Readiness、容量准入和 Provider Account 水位指标。双实例场景必须从内部网络分别抓取两个应用实例，不能只抓会轮询后端的代理入口；`deploy/prometheus/prometheus.example.yml` 提供了 Secret 文件方式的示例。基础规则位于 `deploy/prometheus/alerts.yml`，容量看板位于 `deploy/grafana/asterrouter-capacity-dashboard.json`，部署方仍需加载规则、绑定 Prometheus 数据源并配置通知渠道。接入外部监控时，至少建立以下探针和人工响应约定：

- 代理和每个实例的 `/ready` 连续失败：立即停止向故障实例派发新请求。
- PostgreSQL 连接、磁盘、WAL 或备份失败：按最高优先级处置，并冻结风险变更。
- Provider Account 连续错误、熔断或容量拒绝：检查路由、凭据、余额和替代候选，保留 Audit。
- Usage、Billing Hold 或 Trace 出现无法终态化的记录：暂停相关配置变更，按 Operation ID 进行对账。

接入指标系统时，容量准入计数只使用固定的 Scope、Result 和 Reason 枚举；Provider 水位只使用受配置控制的 Provider 与 Provider Account ID。不得把 Tenant、Application、Credential、邮箱、完整 Key、Prompt、Response、对象 URL 或原始请求 ID 放入指标标签。

## 容量告警演练

1. 在隔离或预生产环境加载 `deploy/prometheus/alerts.yml`，确认 `asterrouter_provider_capacity_snapshot_status` 为 1，两个实例均被独立抓取。
2. 为测试 Provider Account 设置低于测试流量的并发、RPM 或 TPM 上限，使用合成请求持续五分钟；不得使用生产凭据或真实业务内容。
3. 核对 `asterrouter_capacity_admissions_total` 的 `rejected` 计数增长、Provider 水位达到 90%，并确认持续拒绝和高水位告警进入 firing。
4. 恢复原限额并释放测试请求，确认水位下降、告警恢复；在 Trace、Usage 和 Attempt 中核对拒绝原因，不把策略拒绝误判为供应不足。
5. CI 使用 `deploy/prometheus/alerts_test.yml` 模拟持续 Tenant 拒绝和 Provider 并发高水位。修改阈值、标签或告警文案时必须同步更新规则测试。

`asterrouter_provider_capacity_snapshot_status` 为 0 时，容量看板属于陈旧证据，不能据此扩容或缩容。先检查 PostgreSQL/容量仓储和实例 Readiness，恢复后再观察至少一个完整业务窗口。

## 持续负载证据

仓库中的 Gateway 持续负载测试会在存在 `ASTER_TEST_DATABASE_URL` 时交替使用两个独立 Service，并在结束时核对 Operation、Attempt、Usage 和 Billing Hold 终态。30 分钟运行必须显式提高 Go 测试超时：

```bash
cd backend
ASTER_TEST_DATABASE_URL='postgres://user:password@db.example.com:5432/asterrouter?sslmode=require' \
ASTER_GATEWAY_SOAK=1 \
go test -timeout=40m -count=1 -run '^TestGatewayNormalAndStreamingSoak$' ./internal/server
```

不要在生产数据库上执行该测试。CI 已提供短时门禁和手动/定时的隔离 PostgreSQL 长测工作流；长测输出应与构建版本一起保存。

## 备份、升级与回滚

在升级前完成一次 PostgreSQL 备份，并确认备份可以在隔离环境恢复。应用的备份与诊断目录位于各实例独立卷中；若这些目录包含需要保留的插件或本地产物，必须与数据库备份一起纳入恢复演练。

升级采用逐实例方式：先确认另一实例就绪，再替换一台实例，验证 `/ready` 和受控请求后处理另一台。迁移由共享数据库协调，禁止同时手工运行多个迁移进程。发生回滚时，使用已验证的应用镜像和与目标版本兼容的数据库恢复路径；不要通过删除卷或手工修改生产表恢复服务。
