# Ent 双数据库、双缓存重构执行记录

更新时间：2026-10-10。

## 当前结论

**当前是基础设施阶段的实现，不是完整四组合兼容版本，也不满足生产发布条件。**
新增的精确十进制能力尚未替换现有业务金额列；SQLite 基线仍有 REAL 金额列。
配额的 Redis 权威路径、原子令牌轮转、Redis 状态导入和全部任务的结算事务尚未迁移。
不能用初始化成功或单元测试通过，替代四组合真实业务验收。

行为对照为 Step1 前的 `b8dece900`。保留当前 HEAD、暂存区和工作区有效改动，
不整体回退，不引入 `sub2api1` 的 PostgreSQL SQL 自动翻译器。
主流程、端口 8080、外部 TZ/ZONEINFO、HTTP 和权限/计费契约不在本阶段重写。

## 已落地

- [x] `TransactionRunner` 复用 context 中的 Ent 事务或调用方提供的事务 client；只有事务拥有者提交/回滚，panic 时回滚，不自动重放不确定提交。
- [x] 身份仓储移除反射/unsafe 取 driver，必要 SQL 通过 Ent execquery 接口执行；按 PG/SQLite 错误码分类约束、锁竞争和取消错误。
- [x] `dbdecimal.Decimal` Scanner/Valuer、精度量化和范围校验；拒绝从 SQL float64 扫描原有金额。SQLite 精确算术、比较、排序、聚合、SHA256、UTC 时间及命名时区函数已注册并启动自检。
- [x] SQLite 驱动锁定 `modernc.org/sqlite v1.46.2`，配套最低依赖 `modernc.org/libc v1.70.0`；单连接、WAL、外键、FULL 同步和有界 busy timeout。
- [x] SQLite 静态版本基线与迁移账本：未知非空文件只读识别并拒绝；识别 WAL 中已提交的应用标记；校验历史 checksum 和连续前缀；待执行脚本与版本记录整事务提交，不拆触发器。
- [x] SQLite 三类触发器脚本：鉴权哈希失效事件、长上下文计费标志及父子同步、日汇总水位失效。已有 SQLite 行为测试；尚不能宣称逐条 PG 等价验收完成。
- [x] `DurableState` Ent 模型及主库 KV 仓储；memory 模式持久状态借用主库，不再为 PG+memory 创建独立 SQLite 文件。
- [x] PG 迁移 `242_primary_durable_state.sql` 增加主库 KV 和队列表，不修改已有 PG 历史迁移。
- [x] 原字节、TTL、原子一次性消费、条件创建、刷新令牌集合更新及删除参与 Ent 事务；修复冲突无 RETURNING 行、最长 TTL 和不区分大小写前缀匹配的回归。
- [x] `ImageQueueEntry` Ent 模型覆盖现有队列表；重排、续租和确认使用 Ent 条件操作。PG 领取使用真实 `FOR UPDATE SKIP LOCKED`，SQLite 使用条件更新；旧 receipt 不能操作新领取者的队列状态。
- [x] 账号创建与 scheduler outbox 同事务；创建/更新返回触发器修改后的 Extra；修复事务 driver 被错误识别为 PG 的 SQLite 账号更新路径。
- [x] PG+memory 启动前检查旧 persistence_path、WAL 和回滚日志。非空旧源明确拒绝启动，保留原文件，不静默放弃令牌/任务/配额状态。
- [x] 轻量模板 memory 预算 64 MiB、`GOMEMLIMIT=384MiB`，不切换为业务 simple 模式。

## 保留与替换

| 范围 | 处理 |
| --- | --- |
| 既有资源生命周期、实例锁、memory TTL/LRU、容量限制、领取凭证和有效测试 | 保留并继续补强，不整体推倒 |
| 当前 SQLite 普通业务修复 | 保留；后续按模块收敛到 Ent，不能据此认定已解决全部类型/锁语义 |
| `sub2api1/sqlite_pgcompat.go` 的锁剥离、统计近似、容错忽略 DDL | 不引入；方言差异必须局部、显式、可测试 |
| 启动时 Ent 自动推断结构、实验迁移重复列忽略 | SQLite 生产初始化已改为版本化静态脚本；旧开发导出器不是正式升级器 |
| PG+memory 的第二份 SQLite 状态库 | 新安装移除；旧文件保留，离线导入未完成前阻止切换 |
| Redis 配额绝对快照刷回 DB | 仍待替换；不能只关闭 flusher 而遗漏账本事务与旧数据对账 |
| Redis 持久状态 providers | 暂不提前切换，以免导入前丢失原状态 |

## 必须继续完成

- [ ] 审计全部 Ent 字段、关系和补充 DDL，尤其复合键、特殊外键、软删除唯一性、布尔约束及新增实体覆盖。
- [ ] 把金额实际接入 Ent 自定义类型：PG 原 NUMERIC(p,s)，SQLite 规范十进制 TEXT、校验和数值排序索引；替换所有原生金额加减、SUM、比较及排序。
- [ ] 精确原子扣费与幂等凭证契约：原数据库金额不得通过 float64 读写往返，保持历史计费指纹顺序，覆盖超 int64 缩放范围。
- [ ] 完成统一 UTC 微秒编码和 JSON 精确数字/三种空值语义；当前主要业务时间与 JSON 路径尚未全部转换。
- [ ] 按业务规则将结算凭证、余额、额度窗口、必要预留和事件放进主库同一事务；移除 flusher 双权威及相关旧配置的重新启用能力。
- [ ] 令牌创建/轮转/撤销、家族撤销、Passkey/OAuth 一次性状态等领域操作整体原子化。KV 单项原子操作不等于完整令牌流程原子化。
- [ ] 任务状态、租约、重试和业务结果/结算统一持久化；领取凭证当前只约束队列操作，不代表全部 worker 业务写回和收费提交已完成隔离。
- [ ] 完成 Redis 到所选主库的离线导入、绝对过期时间和配额绝对值对账，再切换 Redis 模式的持久状态 providers。旧源不得删除。
- [ ] 鉴权与硬配额版本校验、失效断线重同步、提交后可靠 outbox 扫描；检查全部 Service/中间件直接客户端依赖。
- [ ] PG/SQLite 共用的触发器、金额、JSON、时区/DST、软删除、排序分页和任务契约。
- [ ] 四组合真实安装、登录、模拟上游、扣费、统计、任务、备份和恢复；多实例锁及故障注入。
- [ ] SQLite+memory 在 1 核 1 GiB、无 swap 环境运行规定的 60 分钟验收，记录 RSS、延迟、锁等待和最终对账。

## 验证记录

已执行的定向测试覆盖：Ent 嵌套事务及回滚、持久令牌并发集合、原字节/TTL/消费、
SQLite 整脚本迁移与升级失败回滚、未知库保护、三类触发器写入行为、账号触发器回读、
精确算术/数值排序/窗口聚合、命名时区的 23/25 小时日界、队列并发领取和旧 receipt 拒绝。

- `go test -mod=readonly ./... -count=1 -timeout=300s`：首轮全后端通过；最后两处事务上下文/绝对 TTL 修正后的复跑中，repository、service 及其余包通过，securityaudit 有一个时序测试失败，因此该次全量命令退出码为 1。
- `go test -mod=readonly ./internal/securityaudit -run '^TestGuardEvaluatorFlagSharedDeadlineFailClosedAndContextCancel$' -count=20 -timeout=60s`：连续 20 次通过。失败子项使用 35ms 等待和 70ms 共享截止时间，负载下有可能来不及进入第二个端点；未修改测试或放宽运行时安全策略，此项时序不稳定仍需跟踪。
- PG 备份单测在测试命令的 PATH 中临时加入 Git 自带 `sh` 后通过，没有修改全局环境。
- `git diff --check`：通过。
- `go test -mod=readonly -tags integration ./internal/repository -run '^TestPostgres(Primary|Exact)' -count=1 -v -timeout=180s`：构建通过；TestMain 输出 `docker is not available; skipping integration tests`，没有实际执行 PG 持久状态、队列或金额契约。

PG/Redis 真实服务不在当前测试环境中；本机未发现 Docker，不能把 integration harness
因缺 Docker 跳过的零退出码记为真实集成通过。单元测试内的 Redis 模拟器也不替代真实 Redis。

本次未启动正式服务，未修改生产数据，未生成 Git 提交。当前分支应继续用于开发和验收，
不得直接作为已满足完整计划的生产升级交付。
