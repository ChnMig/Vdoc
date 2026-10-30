# 项目提交与发布 API（0.4 beta）

新的文档仓库以项目为单位保存完整文件树。每次保存都产生不可变提交；同一分支可以连续提交，不再依赖可覆盖的草稿。提交 ID、文件内容哈希、新文档 ID、发布 ID 和发布版本名都由服务端生成。

入口：`/api/v1/private/projects/:project_id/repository`。认证、错误码和响应仍使用现有 `HTTP 200 + code/status/detail` 契约；详细入参与 DTO 见同目录 `openapi.yaml`。

## 保存与历史

1. `GET /branches` 取得分支的固定 `head_commit_id` 和 `generation`。
2. 从所需提交创建工作分支：`POST /branches`。`main` 默认受保护。
3. `POST /branches/:branch_id/commits` 提交 `changes`、`expected_head_commit_id`、`expected_generation` 和 `idempotency_key`。无需也不能传入 `version_name`；新文件可省略 `document_id`，由服务器生成。
4. 所有 upsert/delete 在同一事务中改变完整树。HEAD 或 generation 变化则返回 `REF_CONFLICT`，调用方保留自己的编辑并重新读取、对比和合并；服务端不覆盖新 HEAD。
5. 相同 key 和相同请求重试返回原提交，即使分支已经继续前进。相同 key 对应不同请求会被拒绝。

`GET /commits?branch_id=…&page=true` 返回 `{commits,next_cursor}`。每页最多返回 50 个提交、扫描 2,000 个 first-parent 节点；按文档过滤时可能返回空页，但仍有 `next_cursor`。一直使用服务器给出的游标，直到为空。兼容的默认数组响应也会通过 `X-Next-Cursor` 标头给出下一页。

`GET /commits/:commit_id` 返回完整文件元数据；`GET /commits/:commit_id/documents/:document_id/content` 返回经过 SHA-256 校验的原始 UTF-8 内容。CRLF、末尾换行和空内容保持原样，不经过文档格式化器。存储对象键不出现在对外 DTO 中。

## 对比、审核、合并和撤销

`POST /comparisons` 接受两个固定提交与可选的 `document_id`，提供原文差异和 OpenAPI 语义差异。无效 OpenAPI 可以保存和查看原文，语义状态为 `invalid`，审核或发布会拒绝它。Beta 每次对比最多 100 个有变化的文件、累计 8 MiB 的旧/新原文；超过时明确返回 `COMPARISON_LIMIT`，不截断结果。

`POST /merge-requests` 保存来源与目标分支的 HEAD、generation、已证明的公共祖先及精确的候选树 `result_files`。可以合并不同文件和同一文件中不相交的行修改；重叠修改、相互冲突的元信息或重名路径要求解决冲突。存在多个最低公共祖先的 criss-cross 历史会明确拒绝，不推测历史。

审核界面必须查看候选树。`GET /merge-requests/:request_id/documents/:document_id/content` 读取 `result_files` 中的精确候选内容；自动合并后，它可能同时不同于来源和目标的原文。旧审核轮次仍可读取，但只有两个 HEAD 和 generation 仍一致时才能批准及合并。

`POST /merge-requests/:request_id/resolve` 接受完整结果文件列表、审核 generation、两侧 HEAD/generation 和重试 key。列表缺席的文件表示删除；每个保留项都是带文档 ID 的完整 upsert。系统在来源工作分支创建双亲提交，将旧审核标为 `superseded`，返回新的审核 ID。重试返回同一个新轮次；旧轮次不能批准新内容。

只有人类项目管理员可以调用 `…/approve` 和 `…/merge`，MCP 没有这些操作且服务层拒绝带 MCP 身份的调用。批准后任何一侧分支移动都会使审批过期。合并可以 fast-forward，否则创建拥有两个父提交的新提交。

`POST /branches/:branch_id/reverts` 把逆向改动保存为新提交，保留后续无关修改；冲突返回 `REVERT_CONFLICT`，不移动或删除历史。合并提交通过 `parent_index` 选择 mainline parent。支持 HEAD/generation 校验和幂等重试。

## 发布、分享和自动处理

人类管理员通过 `POST /releases` 指定固定分支、提交和 generation。服务端验证 OpenAPI 后生成发布 ID 和版本名，发布永远指向该提交。分支后续保存不改变已发布内容；MCP 不能发布。

`/releases/:release_id/documents/:document_id/shares` 管理固定发布文件的分享链接。新分享走现有公开 document-shares 路由，公开的 `version_id` 是固定 release ID，不是可移动的分支指针。分享口令、撤销和密钥轮转沿用 capability 机制。

`GET /events?after_sequence=…` 提供事务内追加的事件，最多 100 条，用最后一个 sequence 继续。AI 分析、webhook 订阅、重试与投递账本的接口见 `openapi.yaml` 的 Repository Automation。AI 结果和推送固定到提交/事件，不授予 AI 审核或发布权限。

## 旧数据导入

当 `/branches` 返回 `import_required=true` 时，需要管理员明确调用 `POST /bootstrap`。仅有旧文档元数据、没有快照的项目无需导入。导入之前不能通过创建空分支、保存新提交或发布空树绕过历史迁移。

导入为每份仍存在的旧正式版本和当前未发布草稿创建受保护的 `legacy/…` 引用，记录来源和可证明的历史路径。初始化 main 的树明确标记为“各文档最近一次旧发布的组合”；旧数据没有原子项目提交，不会伪造这类历史，已经被旧草稿更新覆盖的内容也无法恢复。

旧表、版本 ID 和分享链接保持可读。导入或首次新仓库保存成功后，旧草稿内容写入在事务内被拒绝。旧写入和新提交采用相同的权限、令牌及仓库锁顺序，防止迁移与旧更新交错覆盖。

## 存储与权限约束

- 迁移 018 的 `repository_records` 独立保存提交图及可移动引用，避免把提交历史放进旧 Store 的全量快照。历史 kind 的 SQL UPDATE/DELETE 由数据库触发器阻止；内存实现遵守相同约束。
- 所有仓库查询限定项目。提交使用项目事务锁和 generation compare-and-swap；权限、项目存续状态和认证时的全部 MCP scopes 在数据库事务内再次校验。
- 文件数最多 5,000；名称最多 200 字节、说明 4,000 字节、相对路径 4,096 字节；内容大小沿用现有对象存储上限。路径不能绝对化、穿越目录或重名。
- 上传使用每次尝试独占的对象路径。可确定回滚的上传会清理；事务提交结果未知时保留对象，避免删除已经被历史引用的内容。
