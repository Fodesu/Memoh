# 轮次历史的事件日志方向（设计起点）

状态：方向已确认，设计未开始。本文记录结论与边界，作为 PR #1357 过渡方案的删除条件。

## 背景

2026-09 排查 `session_runtime.history_inconsistent`（Web 端对已结束的 run 点击重试或编辑返回 500）时确认了两层原因。

**直接原因。** 服务端以 `bot_history_messages` 判定"最新可见轮次"，前端以运行时快照投影出的列表尾部判定。run 以失败结束但没有写入任何消息时（无可见输出的失败按"未发送"处理），两边对"这一轮是否存在"得出不同结论。

**结构原因。** 轮次（turn）不是一等数据结构。

| 状态持有者 | 内容 | 谁负责对齐 |
|---|---|---|
| `bot_history_messages` | 消息行；turn 是行上的 7 个列（`turn_id`、`turn_position`、`turn_message_seq`、`turn_visible`、`turn_superseded_*`） | 应用层持久化代码 |
| Session Runtime 快照（Redis / 内存） | run 的实时视图：状态、错误码、消息流 | `FinishRun`、`HandleAgentEvent` |
| `session_runs` ledger | run 的持久终态与 fencing | `finishRun` 的一组签名 |

三处状态靠应用层代码手工对齐。一轮对话只有在至少写入一条消息后才在历史中"存在"，所以记录一次失败的轮次需要写一条空 assistant 消息并在 metadata 放 `error_code`。现有的 `bot_session_events` 只记录入站事件，`chat/event.Hub` 是进程内发布订阅，`chat/timeline` 从消息行向上投影，都不是 agent 轮次的事实来源。

## 目标形态

一个会话一条追加式事件日志，作为轮次状态的唯一事实来源：

- `turn_admitted`（turn id、position 在此分配）
- `request_recorded`
- `step_committed` / assistant 输出
- `turn_failed`（error_code）
- `turn_completed`
- `turn_superseded`（retry / edit）

历史 API、运行时实时视图、重试与编辑的校验读的是同一条日志的不同投影。失败本身是一条事件，不需要空消息占位；"这一轮有没有落库"这个问题不存在，因为事件写入即落库；前后端的 turn id 来自同一条 `turn_admitted`。`session_runs` 的 fencing 与终态并入日志，Redis 只做缓存。

## 需要设计文档回答的问题

1. 事件模型：事件种类、载荷、幂等键、与 fencing token 的关系。
2. 读模型：历史 API 继续返回消息列表，还是改为返回轮次列表；前端 transcript 如何从轮次渲染失败状态。
3. 与现有表的关系：`bot_history_messages` 是否保留为投影表；`turn_*` 列的去向；`session_runs` 是否并入。
4. 迁移：历史数据回填；渠道侧（Telegram 等）读取路径；`botbackup` 导入导出。
5. 分 PR 顺序与每一步的回滚方式。

## 过渡方案（PR #1357）的删除条件

过渡方案在运行时快照上增加 `persisted_turn`（仅含 `turn_id`），由应用层在 round 落库当下记录；前端以 `runHistoryState` 单一谓词判定失败阶段、投影与重试门禁；Session Runtime 在 run 失败结束且无记录时调用审计器，与历史比对并在不一致时打 error 日志。

事件日志落地后，以下内容整体删除：

- `sessionruntime.PersistedTurnView`、`Manager.RecordPersistedTurn`、`SetPersistedTurnAuditor`
- 应用层 `notePersistedTurn` / `publishPersistedTurn` / `auditUnrecordedPersistedTurn` 及三处挂点
- 前端 `RuntimePersistedTurn`、`runHistoryState`、`isTurnUnpersisted`、`RuntimeTranscriptSlice.unpersisted`、`replacement-recovery.ts` 的 `runLeftNoHistory`

`session_runtime.turn_not_latest` / `turn_incomplete` 等错误码与 `catalog_guard_test.go` 是永久改动，不随过渡方案删除。
