# 注册登录安全加固（参考 sub2api）

## Stage 1: 邮箱别名归一化去重
**Goal**: 单个真实收件箱无法通过 `+别名` / Gmail 点号 / FQDN 根点派生多个账号
**Success Criteria**: `a+1@gmail.com`、`a.b@gmail.com`、`ab@googlemail.com` 与 `ab@gmail.com` 互相冲突；非 Gmail 域名点号仍有效
**Tests**: `email_alias_test.go` 覆盖归一化规则；注册重复检测集成测试
**Status**: Complete

## Stage 2: 限流与信息泄漏加固
**Goal**: 注册/重置/重发/验证各入口独立限流；密码重置不泄漏用户是否存在
**Success Criteria**: 各入口超限返回 429；reset-password 失败返回通用文案
**Tests**: 限流器多桶测试；reset-password 错误响应测试
**Status**: Complete

## Stage 3: token 比较与密码策略收口
**Goal**: 所有 token/hash 比较为常量时间；密码强度在 service 层无条件校验；重发验证邮件有冷却
**Success Criteria**: 无 `==` 比较 hash；任意注册路径密码 <10 位均被拒
**Tests**: 密码强度测试；重发冷却测试
**Status**: Complete

## Stage 4: 查询路径优化
**Goal**: 登录/验证/重置不再全表扫描
**Success Criteria**: 新增按邮箱与按 token hash 的定向查询方法
**Tests**: 定向查询的 Memory/Postgres 一致性测试
**Status**: Complete
