# AsterRouter 随机失败与隔离策略

## 1. 判定

满足任一条件即标记为 suspected flaky，不得直接重跑后忽略：

- 相同 commit、环境和数据下，首次失败而后续通过；
- 失败证据指向时间、端口、并发顺序、未清理数据、网络或进程生命周期；
- 最近 20 次运行中至少出现 2 次无代码相关性的失败。

确认 flaky 前至少保留首次失败日志、JUnit、trace/screenshot、环境版本、test run ID 和最短复现命令。

## 2. 处理时限

| 级别 | 范围 | 动作 | 修复时限 |
| --- | --- | --- | --- |
| P0 | auth、RBAC、billing、gateway evidence、migration、backup/restore、plugin trust | 禁止隔离，保持阻断并立即修复 | 24 小时 |
| P1 | 核心浏览器旅程、PostgreSQL、发布/容器 | 可临时从非发布矩阵隔离，但发布仍阻断 | 3 个工作日 |
| P2 | 非关键 UI、浏览器兼容、性能噪声 | 可 quarantine，必须有 owner 和 issue | 5 个工作日 |

超过时限仍未修复时，测试 owner 必须删除隔离或将对应功能标记为不可发布；不能无限续期。

## 3. Quarantine 记录

每个隔离项必须在 issue 和 CI 输出中包含：

- 测试全名、文件、owner、首次失败 commit；
- 影响的 Surface/角色/环境；
- 失败率、最近失败链接和最短复现命令；
- 根因假设、修复 PR、到期日期；
- 为什么隔离不会掩盖 P0/P1 发布路径。

测试内 `skip` 只允许静态环境差异，例如 desktop-only API 旅程。动态失败不得自动转换为 skip。

## 4. Retry 规则

- Go、Vitest、PostgreSQL、migration、backup/restore 默认 0 retry。
- Playwright PR smoke 默认 0 retry；nightly 可对已确认的浏览器启动/进程边界最多 retry 1 次。
- 业务断言、授权、数据一致性和配额失败禁止 retry。
- retry 后通过仍计为 flaky，不得把 job 标记为完全健康。

## 5. 解除隔离

修复后必须满足：

1. 原始最短复现用例已稳定通过；
2. 相同环境连续运行至少 20 次无失败；
3. 相邻包或旅程全量通过；
4. issue 附修复原因、验证命令和证据链接；
5. 从 quarantine 列表和 CI 特殊配置中删除。

当前 quarantine 清单：无。
