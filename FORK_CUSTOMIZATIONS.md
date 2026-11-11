# Fork：个人使用记录扩展

## 功能与范围

- 管理员侧栏将「我的账号」及其原有子菜单置顶，原管理菜单归入「系统」。保留上游权限、功能开关及自用模式规则。
- 我的账号 → 使用记录增加可选择的「会话名称」「缓存命中率」列，沿用列设置的本地保存机制。检出掉缓存时，在命中率数字旁显示红色「掉缓存」标签，不再单列显示。
- 我的账号使用记录及管理端明细页的筛选条件使用全宽自适应网格；刷新、重置、列设置和导出等按钮独立一行靠右，避免挤占筛选空间。
- `usage_logs.session_id` 已由上游持久化；继续复用，不新增另一套 ID，也不以提示词内容猜测 ID。列表不提供 ID 列；响应数据及 CSV 导出保留 ID。
- 名称从服务运行机器的 Codex/Claude 本地标题记录按 UUID 精确查找。没有标题或没有 ID 显示 `—`；不回退到用户消息。名称随本地重命名刷新，缓存 10 秒，不写入使用日志数据库。
- 命中率 = 缓存读取 /（普通输入 + 缓存读取 + 缓存写入），保留两位小数。颜色与 `codex_quota` 一致：≥98% 深绿加粗，≥95% 绿，≥90% 黄，其余红；无输入用量显示 `—`。

## 掉缓存检测

与 `codex_quota/backend/gateway/monitoring.py` 相同的核心条件：同一用户及会话、同一上游账号和实际模型的相邻请求，间隔大于 0 且不超过 300 秒，命中率下降至少 20 个百分点，缓存读取 token 数减少。

通过数据库读取当前页记录的直接前序请求，不受列表分页、模型、日期、API Key 筛选或排序影响。不跨账号/模型寻找更早的可比请求。同一时间多个请求、前序请求持续至当前请求之后、持续时间未知、压缩及其紧邻下一条请求、空或无效输入用量都不比较。监测状态（无会话 ID、暂不可用、未检出）放在命中率的悬停提示中。

命中率旁的红色「掉缓存」标签的提示包含前后命中率、下降百分点、间隔和前序使用记录编号。它只说明已记录用量变化，不推断切号、TTL 或服务商行为。「未检出」表示未发现符合条件的变化，也可能缺少可比较的前序请求。

与参考项目的时间语义差异：Sub2API 的 `created_at` 是完成转发后记录用量的时间，因此以记录时间判定相邻关系和 5 分钟窗口，并以「本次记录时间 − 本次 duration_ms」估计开始时间，与前序完成时间比较以排除重叠。异步落账可能存在时间偏移；这是一项用量启发式监测，不是网关原始调用时间审计。

## 本地标题读取

只向管理员自己的 `/api/v1/usage` 响应提供本地名称，普通用户不能凭会话 ID 读取运营者本地标题。该扩展定位为单机自用；服务器部署时读取的是服务器文件，而非浏览器所在电脑。

- Codex：`CODEX_HOME`，默认用户目录 `.codex`；只读 `state_5.sqlite` 的 `threads(id,title)`，缺失时回退 `session_index.jsonl`。
- Claude：`CLAUDE_CONFIG_DIR`，默认用户目录 `.claude`；优先 `projects/*/<UUID>/custom-title.json`，回退同名会话 JSONL 中的 `custom-title` 元数据。
- 设置 `PERSONAL_USAGE_LOCAL_TITLES=false` 可完全关闭本地标题查找。
- 不记录文件路径、消息内容、数据库凭据或上游 Key；SQLite 使用只读连接，不创建或修改客户端数据库。

## 后续同步上游

核心逻辑独立在 `backend/internal/pkg/personalusage/`、`backend/internal/{handler,service,repository}/personal_usage*.go` 和 `frontend/src/extensions/personalUsage/`。

上游文件仅保留少量挂接点：使用记录 handler 的响应构建、共享 UsageTable 的两个扩展 slot、个人 UsageView 的列/单元格/CSV、UsageLog 的可选 session_id 类型、侧栏分组顺序及名称，以及个人/管理端筛选工具栏布局。未修改生成的 Ent 代码、调度、计费、鉴权逻辑或依赖锁文件。

新增迁移 `251_fork_personal_usage_session_index_notx.sql` 使用独立索引名 `idx_fork_usage_user_session_created`，为跨页查询建立部分索引。迁移按完整文件名记录；今后上游即使也增加 251 前缀文件，也无需改名。已执行的迁移不要修改。

源码构建后才会生效。后续更新需要备份数据库、替换运行程序并重启 Sub2API 服务；无需重启 Codex。

## 本次构建与验证

已生成并启用 `local-runtime/sub2api-custom.exe`，当前版本 `0.2.21-personal.2`，内嵌本次构建的前端资源。2026-11-11 已将其复制到启动脚本使用的 `local-runtime/sub2api.exe` 并重启服务；运行接口确认版本为 `0.2.21-personal.2`，健康检查正常。以后仍可通过根目录的 `restart-local.cmd` 重启当前定制版。

personal.2 将掉缓存标签合并至命中率列，并拆分个人/管理端的筛选条件与操作按钮行。71 项相关前端测试、TypeScript、ESLint 与生产构建通过；按用户要求，页面由用户自行验收。启用前的数据库和 personal.1 程序备份在 `local-runtime/backups/20261111-personal.2/`；本次没有新增数据库迁移。

验证覆盖前端列开关及其持久化、隐藏 ID 仍保留数据、CSV 导出、共享表格 slot、命中率边界和颜色、告警详情及检测不可用状态；通过 TypeScript、ESLint 和 Vite 生产构建。后端覆盖标题优先级/只读 SQLite/重命名缓存、用户隔离、跨页前序请求、20 个百分点与 300 秒边界、并发和压缩排除；数据库相邻查询已在本机 PostgreSQL 上只读验证。

部署应使用自编译产物；同步上游源码后重新构建，以保留扩展。启用前已备份运行文件和 PostgreSQL 数据库，并在停止旧服务后再生成一份最终数据库备份，均保存在忽略的 `local-runtime/backups/20261111-1723-personal-upgrade/`。备份包含私密配置，仅供本机恢复，不应提交或分享。

本次迁移完成 `249_add_usage_logs_api_key_cost_index_notx.sql`、`250_openai_cache_write_ttl.sql` 和 `251_fork_personal_usage_session_index_notx.sql`，已核对迁移记录、新字段、会话索引有效性及升级前后主要数据计数。数据库已升级，回退旧程序前必须评估数据库兼容性；完整恢复应使用 `stopped/database.dump` 与同目录上级保存的旧运行文件，并考虑备份之后产生的新数据。

## personal.3：上游定价同步（已构建，按用户选择暂不启用）

已实现按 API Key 上游账号配置的 New API/Sub2API 定价同步：分组读取、价格差异预览及应用、定时同步、人工覆盖、版本历史与回滚预览。入口在账号编辑窗口新增的“上游定价”页签。

管理员个人/管理端用量列表增加可选“上游预计扣费”列；个人 CSV 增加估算金额、状态和价格版本。普通用户和受限管理密钥不返回上游成本。此扩展仅计算上游估算，不修改本机扣费和既有账号统计倍率。

新增主体集中在 `backend/internal/pkg/upstreampricing/`、独立 service/repository/handler/route 文件与 `frontend/src/extensions/upstreamPricing/`；接入点是 Wire 装配/清理、用量原始快照与记录后的异步估算、管理路由、账号编辑页和共享用量列表。新增迁移 `252_fork_upstream_pricing.sql` 创建五张独立侧表；不修改 Ent 生成代码、go.mod/go.sum 或 pnpm-lock.yaml。

同步失败保留旧价，账号 Key/地址变化使旧配置失效。价格按既有请求开始时间选版本，支持明确的固定 Token/按次价格与已知缓存写入生命周期；阶梯、峰谷、工具等未适配维度显示未知。尚未接入真实站点进行价格核对，凭证需要用户在本机页面填写。完整操作与限制见 `UPSTREAM_PRICING_PLAN.md`。

已通过 226 项相关前端测试、TypeScript、ESLint、Vite 构建、后端相关回归与 PostgreSQL 临时 schema 的迁移/历史价格/同步互斥/用户隔离验证。构建产物为 `local-runtime/sub2api-personal.3.exe`，当前运行程序仍为 personal.2；直接运行现有重启脚本不会自动切换到新产物。启用前需备份当前程序、运行配置与数据库，再替换程序并重启；首次启动自动执行新增迁移。
