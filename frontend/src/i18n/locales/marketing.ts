export const marketingZh = {
  metaTitle: 'AsterRouter｜企业 AI 访问与路由基础设施',
  metaDescription: 'AsterRouter 是可私有部署的企业 AI 网关，统一管理应用接入、访问策略、模型供应、智能路由、用量成本与审计证据。',
  brandTagline: '企业 AI 访问与路由基础设施',
  navLabel: '官网主导航', mobileNavLabel: '移动端官网导航', openNav: '打开官网导航', closeNav: '关闭官网导航', enterConsole: '进入企业控制台',
  nav: { product: '产品能力', routing: '路由策略', governance: '企业边界', repository: '开源仓库' },
  hero: {
    overline: 'POLICY-GOVERNED ENTERPRISE AI GATEWAY', category: '企业 AI 访问与路由基础设施',
    description: '在企业应用与已授权 AI 供应商之间建立统一控制面。一次完成身份校验、访问准入、资源选择、成本约束和请求级证据记录。',
    primaryAction: '进入企业控制台', secondaryAction: '查看请求决策', factsLabel: '产品事实',
    factProtocols: '统一接入 OpenAI、Anthropic 与 Gemini 兼容协议', factEvidence: '策略、路由、用量、成本与 Trace 使用同一证据链',
    imageAlt: 'AsterRouter 路由策略工作台实际界面',
    liveDecisionLabel: 'AsterRouter 实时路由决策预览', liveDecision: '实时决策', requestApproved: '请求已进入首选线路', requestApprovedDetail: '策略版本、候选顺序与执行结果同步写入 Trace',
    policyProof: '访问与路由策略已命中', policyProofDetail: '先准入，再选择供应资源', costProof: '成本边界已检查', costProofDetail: '有效价格未超过策略上限', capacityProof: '容量租约已获取', capacityProofDetail: '并发、RPM 与 TPM 均可用'
  },
  decision: {
    overline: 'ONE REQUEST, ONE EXPLAINABLE DECISION', title: '每一次模型请求，都经过同一条企业决策链',
    description: '访问策略先决定请求是否允许，路由策略再从已批准且可调度的资源中选择线路。执行结果回到 Trace、用量与成本事实，不留下黑箱。',
    workbenchLabel: 'AsterRouter 请求决策工作面', requestLabel: '请求标识', policyApplied: '企业策略已应用', application: '应用', model: '网关模型', routeGroup: '路由组',
    candidates: '候选资源', hardRulesFirst: '硬约束先于评分偏好', selected: '已选择', standby: '待命', excluded: '已排除',
    candidatePrimary: '稳定优先 · 本批次首选', candidateSecondary: '健康可调度 · 同批次备用', candidateExcluded: '超过相对最低价上限',
    traceReady: '请求级证据已就绪', traceDetail: '策略版本、候选顺序、排除原因和上游结果写入 Trace',
    stages: { identity: '身份与凭据', access: '访问策略', routing: '路由策略', scheduling: '资源调度', evidence: 'Trace 证据' },
    stageValues: { identity: '应用调用身份', access: '权限与额度', routing: '约束与偏好', scheduling: '容量与健康', evidence: '结果与成本' }
  },
  capabilities: {
    overline: 'ONE ENTERPRISE CONTROL PLANE', title: '围绕企业任务组织，不围绕后端模块堆菜单',
    description: '管理员从应用、模型、策略、用量和组织五个稳定入口完成治理；服务门户只向员工和开发者开放被授权的访问与证据。',
    tableLabel: 'AsterRouter 企业产品能力', domain: '企业任务', scope: '统一管理对象', evidence: '可核对结果',
    applications: { title: '应用接入', scope: '应用、调用身份、Workspace Key 与企业登录集成', evidence: '凭据生命周期、授权上下文与访问范围' },
    supply: { title: '模型服务', scope: '网关模型、Provider、路由资源、模型路由与采购价格', evidence: '显式供应映射、健康状态与可调度容量' },
    policies: { title: '策略治理', scope: '访问策略、路由策略、模型与协议准入、成本护栏', evidence: '策略版本、路由原因、硬约束排除与审计记录' },
    cost: { title: '用量与成本', scope: '请求用量、采购成本、成本分摊、预算、告警与导出', evidence: 'Usage、Trace、成本事实与上游账单对账' },
    governance: { title: '组织与安全', scope: '企业成员、部门、组织组、身份绑定、TOTP 与系统审计', evidence: '最小权限、会话撤销、变更记录与数据边界' }
  },
  routing: {
    overline: 'ROUTING POLICY IS A CONTRACT', title: '策略不是一个权重，而是一份完整路由合同',
    description: '模型和协议范围、价格边界、有序资源批次、粘性路由与首字节前故障切换共同决定候选。偏好只能在硬约束内排序。', action: '打开路由策略工作台', contract: '内置决策偏好', hardBeforeSoft: '硬约束 → 偏好评分 → 有序降级',
    preferences: { cost: '成本优先', speed: '速度优先', stability: '稳定优先', balanced: '综合均衡' },
    preferenceDetails: { cost: '在硬约束与稳定性底线内优先低成本资源', speed: '优先容量余量高、当前负载低的资源', stability: '优先非探测状态和管理员明确的稳定线路', balanced: '综合路由优先级、账号优先级、容量余量和权重' },
    orderedBatches: '有序资源批次不是第五种偏好', orderedBatchesDetail: '同一批次内评分；当前批次全部失败后才进入下一批次，粘性与价格优化不得越级。'
  },
  governance: {
    overline: 'PRIVATE-DEPLOYABLE ENTERPRISE BOUNDARY', title: '企业身份、供应凭据与调用证据留在同一控制边界',
    description: 'AsterRouter 只管理企业 AI 访问与路由，不接管外部 SaaS 的用户、订单、订阅或支付。业务系统保留自己的产品事实，网关保留授权、执行和成本证据。',
    callers: '企业应用与员工', callersDetail: '内部服务、Agent、开发工具与受限服务门户', coreDetail: '身份 → 策略 → 路由 → 证据', providers: '已授权 AI 供应商', providersDetail: '企业批准的 Provider、账号、模型与区域资源',
    privateDeployment: '支持私有部署与单一企业组织边界', enterpriseIdentity: '应用身份、企业登录与最小权限授权', noResale: '面向企业自有业务与内部平台，不承载余额充值或对外转售流程'
  },
  cta: { title: '从一个受策略治理的请求开始', description: '建立企业实例，配置首个应用和模型供应，再用访问策略与路由策略发布一条可解释的生产调用链。', primary: '进入企业控制台', repository: '查看开源仓库' },
  footer: { description: '面向企业应用、团队与内部平台的可私有部署 AI 网关。', productLabel: '产品', resourceLabel: '资源', boundary: '企业专用 · 策略治理 · 全程可追溯' }
}

export const marketingEn = {
  metaTitle: 'AsterRouter | Enterprise AI access and routing infrastructure',
  metaDescription: 'AsterRouter is a private-deployable enterprise AI gateway for application access, policy, model supply, intelligent routing, usage, cost, and audit evidence.',
  brandTagline: 'Enterprise AI access and routing',
  navLabel: 'Website navigation', mobileNavLabel: 'Mobile website navigation', openNav: 'Open website navigation', closeNav: 'Close website navigation', enterConsole: 'Open enterprise console',
  nav: { product: 'Product', routing: 'Routing policy', governance: 'Enterprise boundary', repository: 'Repository' },
  hero: {
    overline: 'POLICY-GOVERNED ENTERPRISE AI GATEWAY', category: 'Enterprise AI access and routing infrastructure',
    description: 'Place one control plane between enterprise applications and authorized AI providers. Enforce identity, access, supply selection, cost boundaries, and request-level evidence on every call.',
    primaryAction: 'Open enterprise console', secondaryAction: 'Inspect the decision flow', factsLabel: 'Product facts',
    factProtocols: 'One gateway for OpenAI, Anthropic, and Gemini-compatible protocols', factEvidence: 'Policy, routing, usage, cost, and traces share one evidence chain',
    imageAlt: 'Actual AsterRouter routing policy workbench',
    liveDecisionLabel: 'AsterRouter live routing decision preview', liveDecision: 'Live decision', requestApproved: 'Request entered the preferred route', requestApprovedDetail: 'Policy version, candidate order, and outcome are written to the trace',
    policyProof: 'Access and routing policy matched', policyProofDetail: 'Admit first, then select supply', costProof: 'Cost boundary checked', costProofDetail: 'Effective price remains inside policy', capacityProof: 'Capacity lease acquired', capacityProofDetail: 'Concurrency, RPM, and TPM are available'
  },
  decision: {
    overline: 'ONE REQUEST, ONE EXPLAINABLE DECISION', title: 'Every model request follows the same enterprise decision chain',
    description: 'Access policy decides whether the request is allowed. Routing policy then selects from approved, schedulable supply. Execution returns to trace, usage, and cost facts instead of disappearing into a black box.',
    workbenchLabel: 'AsterRouter request decision surface', requestLabel: 'Request ID', policyApplied: 'Enterprise policy applied', application: 'Application', model: 'Gateway model', routeGroup: 'Route group',
    candidates: 'Candidate resources', hardRulesFirst: 'Hard constraints precede preference scoring', selected: 'Selected', standby: 'Standby', excluded: 'Excluded',
    candidatePrimary: 'Stability first · first in batch', candidateSecondary: 'Healthy and schedulable · same-batch fallback', candidateExcluded: 'Above relative cheapest-price limit',
    traceReady: 'Request evidence is ready', traceDetail: 'Policy version, candidate order, exclusion reasons, and upstream outcome are written to the trace',
    stages: { identity: 'Identity and key', access: 'Access policy', routing: 'Routing policy', scheduling: 'Supply scheduling', evidence: 'Trace evidence' },
    stageValues: { identity: 'Application principal', access: 'Permissions and limits', routing: 'Constraints and preference', scheduling: 'Capacity and health', evidence: 'Outcome and cost' }
  },
  capabilities: {
    overline: 'ONE ENTERPRISE CONTROL PLANE', title: 'Organized around enterprise work, not backend modules',
    description: 'Administrators govern applications, models, policies, usage, and organization through five stable entry points. The service portal exposes only authorized access and evidence to employees and developers.',
    tableLabel: 'AsterRouter enterprise capabilities', domain: 'Enterprise task', scope: 'Managed objects', evidence: 'Verifiable result',
    applications: { title: 'Application access', scope: 'Applications, calling principals, Workspace Keys, and enterprise sign-in integrations', evidence: 'Credential lifecycle, authorization context, and access scope' },
    supply: { title: 'Model services', scope: 'Gateway models, providers, route resources, model routes, and procurement prices', evidence: 'Explicit supply mappings, health state, and schedulable capacity' },
    policies: { title: 'Policy governance', scope: 'Access and routing policies, model and protocol admission, and cost guardrails', evidence: 'Policy versions, route reasons, hard-filter exclusions, and audit records' },
    cost: { title: 'Usage and cost', scope: 'Request usage, procurement cost, allocation, budgets, alerts, and exports', evidence: 'Usage, traces, cost facts, and upstream billing reconciliation' },
    governance: { title: 'Organization and security', scope: 'Members, departments, organization groups, identity binding, TOTP, and system audit', evidence: 'Least privilege, session revocation, change records, and data boundaries' }
  },
  routing: {
    overline: 'ROUTING POLICY IS A CONTRACT', title: 'A policy is not one weight. It is a complete routing contract.',
    description: 'Model and protocol scope, price boundaries, ordered resource batches, stickiness, and pre-byte failover shape the candidate set. Preferences may only rank resources inside hard constraints.', action: 'Open the routing policy workbench', contract: 'Built-in decision preferences', hardBeforeSoft: 'Hard constraints → scoring → ordered fallback',
    preferences: { cost: 'Cost first', speed: 'Speed first', stability: 'Stability first', balanced: 'Balanced' },
    preferenceDetails: { cost: 'Prefer lower-cost resources inside hard constraints and reliability floors', speed: 'Prefer more capacity headroom and lower current load', stability: 'Prefer non-probe resources and administrator-defined stable routes', balanced: 'Combine route priority, account priority, capacity headroom, and configured weight' },
    orderedBatches: 'Ordered resource batches are not a fifth preference', orderedBatchesDetail: 'Score inside one batch and advance only after it fails. Stickiness and price optimization cannot jump across batches.'
  },
  governance: {
    overline: 'PRIVATE-DEPLOYABLE ENTERPRISE BOUNDARY', title: 'Keep enterprise identity, provider credentials, and request evidence in one control boundary',
    description: 'AsterRouter governs enterprise AI access and routing. It does not take ownership of an external SaaS product’s users, orders, subscriptions, or payments. Business systems keep product facts; the gateway keeps authorization, execution, and cost evidence.',
    callers: 'Enterprise applications and employees', callersDetail: 'Internal services, agents, developer tools, and the restricted service portal', coreDetail: 'Identity → policy → routing → evidence', providers: 'Authorized AI providers', providersDetail: 'Enterprise-approved providers, accounts, models, and regional supply',
    privateDeployment: 'Private deployment with one enterprise organization boundary', enterpriseIdentity: 'Application identity, enterprise sign-in, and least-privilege access', noResale: 'For enterprise-owned applications and internal platforms, without prepaid balance or resale workflows'
  },
  cta: { title: 'Start with one policy-governed request', description: 'Initialize the enterprise instance, configure the first application and model supply, then publish an explainable production call path with access and routing policy.', primary: 'Open enterprise console', repository: 'View repository' },
  footer: { description: 'A private-deployable AI gateway for enterprise applications, teams, and internal platforms.', productLabel: 'Product', resourceLabel: 'Resources', boundary: 'Enterprise only · Policy governed · Fully traceable' }
}
