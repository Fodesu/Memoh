# Session Runtime Decision 与 Abort 竞态分析

## 结论

当前问题不是单纯的前端显示问题，也不是把一个 `if` 放到 Abort 处理器里就能可靠解决的问题。

代码中确实存在一条可触发的竞态：Decision response 和 Abort 是两个独立的异步操作，而 Decision response 在某些路径上还会启动一个新的 continuation run。Abort 可能已经被用户发出，但 continuation 仍然完成 admission，随后继续产生提问或 streaming 输出。

不过，目前没有一份真实的 WebSocket frame trace 能证明每一次用户复现都由这一条竞态导致。已有代码和 Manager contract test 证明了“两个操作没有统一顺序”这一缺陷；要把线上某次现象归因到具体窗口，还需要同时记录 response、abort、runtime snapshot/delta 和 continuation generation。

本次未提交的修复方向能够覆盖这个窗口，但引入了较多局部状态和同步层：前端 intent map、handler 的 pending response registry、continuation admission 标记、精确 RunHandle 路由，以及 Manager 的 event barrier。它是一个不改变 wire shape 的防守性修复，不是最简洁的长期模型。因此本分支暂不保留这批未提交修改，先把问题和后续设计边界记录下来。

## 当前执行路径

### 正常的 active runtime

```text
Agent 发出 pending Decision
  -> Manager 把 Decision 写入 runtime snapshot
  -> 浏览器发送 tool_approval_response / user_input_response
  -> handler 将 response 路由到当前 run owner
  -> owner 执行 command handler
  -> Decision 持久化为 terminal
  -> agent waiter 被唤醒
  -> 同一个 run 继续执行
```

这里 response 和 Abort 都会修改同一个 `runControl`。如果它们分别进入不同 goroutine，而没有共同的顺序屏障，就会出现：

```text
response 开始处理
Abort 开始处理
response 唤醒 waiter
Abort 才把 run 标记为 aborting
```

最终表现可能是 Abort 已经返回成功，但 agent 已经读取了 response，并继续发出后续事件。

### deferred continuation

当原来的 runtime 不再由当前 handler 直接处理时，response 会走 deferred 路径：

```text
response
  -> Prepare response target
  -> 创建 continuation stream
  -> StartRun / admission builder
  -> Commit response
  -> ContinueCommitted...()
```

这条路径多了一个窗口：

```text
response 已进入 handler
  -> continuation 尚未完成 admission
  -> Abort 到达
  -> continuation 仍可能完成 admission
  -> 后续 tool / text 事件继续产生
```

所以只在已有 active stream 上调用 `AbortRun` 不足以覆盖 deferred continuation。

## 当前未提交方案做了什么

这批修改由几个互相配合的部分组成：

1. **前端保留 Abort intent**

   Decision response 使用独立的 response stream。用户在 continuation generation 尚未出现时点击 Abort，前端暂存 intent，并在 generation 出现后再次发送针对该 generation 的 Abort。

2. **handler 记录 pending response**

   `wsStreamRegistry` 增加 `decisionResponses`。Abort 如果早于 response admission 到达，不立即丢弃，而是挂在同一个 `(session_id, stream_id)` 上。

3. **continuation admission handoff**

   continuation 注册时接收 pending Abort 标记；在 admission 完成前再次检查。若 Abort 已经赢得这个窗口，就终止新 run，不启动 runner。

4. **精确绑定 parent run**

   `DispatchActiveCommandWithHandle` 返回实际接受 Decision 的 `RunHandle`，避免只用 response stream id 猜测要 Abort 的 run。

5. **Manager event barrier**

   Agent event、Decision command 和 Abort 共用一个 per-run 顺序屏障，避免 Decision projection 与 Abort 状态更新交错。

6. **有界 finalization**

   终止路径不再使用无限期的 `context.WithoutCancel`，避免 response/Abort 的收尾 goroutine 无限等待。

这些部分分别解决不同时间窗口，所以不是完全重复；但它们共同维护了两套状态：runtime 的 run 状态，以及 handler/frontend 对 response admission 的临时状态。这正是实现显得复杂的主要原因。

## 复杂度是否合理

### 必然存在的复杂度

以下事实决定了问题不会被完全压缩成一个前端修补：

- Decision response 和 Abort 是不同的命令，网络可以乱序、重复或延迟。
- active runtime 和 deferred continuation 是两个不同的执行路径。
- 多 server 部署时，收到 WebSocket 的 server 可能不是 run owner。
- response commit 和 agent continuation 不是同一个数据库事务。
- continuation 在获得 generation 前没有可直接放入 Abort frame 的 generation。

因此，必须有某种“尚未完成的操作”状态，以及一个原子或串行的交接点。完全不保存状态是不可能可靠解决的。

### 当前方案中可以避免的重复

当前方案把同一件事分散在多个位置：

- 前端保存一次 intent；
- handler 保存一次 pending response；
- `activeWSStream` 再保存一次 admission/abort 标记；
- Manager 还维护一套 abort phase；
- response 路由另外返回一个 `RunHandle`。

这种设计可以在不增加 wire 字段的前提下补住窗口，但长期维护成本偏高，尤其容易出现某个入口忘记转发 intent、清理状态或携带 generation 的问题。

## 是否可以用更简单的设计

可以，但要接受不同的边界和改动范围。

### 方案 A：前端在 response 期间禁用 Abort

不推荐。

这只能减少发起页面的一个竞态，无法处理另一个浏览器、重连后的重复命令或 server 内部的 Abort。它还会让用户在最需要停止时暂时失去控制。

### 方案 B：在 WebSocket read loop 中串行处理所有消息

只能部分解决。

如果 response 和 Abort 来自同一条连接，按收到顺序执行可以消除一部分 goroutine 乱序；但 handler 仍会把命令转发到 owner，多个连接和多个 server 仍然需要统一顺序。为了等待 continuation admission 而阻塞 read loop 也会带来 head-of-line blocking。因此它不是分布式语义的完整解决方案。

### 方案 C：每个 run 一个 operation coordinator

这是更清晰的长期方向。

把 Decision response、Agent event、Abort 和 continuation admission 都提交给同一个 per-run coordinator：

```text
coordinator(run generation)
  -> resolve decision
  -> mark decision projection
  -> accept or reject continuation
  -> apply abort
  -> publish terminal/runtime state
```

coordinator 只保留一个明确状态，例如：

```text
running
waiting_decision
resuming_decision
abort_requested
aborted
completed
```

Decision continuation 只有在 coordinator 原子地从 `waiting_decision` 转为 `resuming_decision` 后才能启动；Abort 如果先到，则把状态转为 `abort_requested`，后续 continuation 直接被拒绝。这样 handler 不需要自己维护一套 admission 状态。

这个方案仍需要在分布式 backend 中持久化或原子更新关键状态，不能只靠 Go 内存锁。它是更好的边界，但会触及 runtime command、decision service 和 deferred continuation 的接口，不属于低风险小修复。

### 方案 D：使用 backend 的 compare-and-set 状态转换

也可以把关键交接收敛为 backend 原子操作：

```text
ResolveDecisionIfRunGenerationIsActive
AbortRunIfGenerationMatches
StartContinuationIfDecisionWasResolvedAndNotAborted
```

这对多 server 最可靠，但需要定义持久化的 operation/decision 状态和失败语义。若现有 snapshot 只保存 run 和 UI projection，就必须扩展 runtime contract 或持久化 command intent。

## 判断：这是不是“真正的问题”

可以分成两层：

### 已确认

- response 路径和 Abort 路径在原实现中没有统一的 per-run 顺序保证。
- deferred continuation 在 admission 完成前存在可被 Abort 穿过的窗口。
- Manager contract test 可以稳定复现“Decision handler 未完成时 Abort 不应越过它”的问题。
- 仅凭前端收到一个 Abort 成功回调，不能证明 agent continuation 已经停止。

### 尚未确认

- 用户某一次看到的“后续问题/streaming”是否来自这个窗口，还是来自 agent/tool 本身没有检查 abort channel。
- 被动页面的 UI 延迟是否由 runtime snapshot/delta 顺序造成，还是 reducer 没有及时投影终态。
- 多 server 场景中是否存在 owner 路由失败后 fallback 到本地 continuation 的额外路径。

要完成归因，测试或生产诊断至少要记录：

```text
decision response received
decision command accepted
decision commit completed
abort received
abort run generation
runtime status: running -> aborting -> aborted
continuation generation created
first post-abort agent event
```

如果 continuation generation 在 Abort 之后创建，问题是 admission handoff；如果 generation 早已创建但仍产生事件，问题是 run cancellation/agent abort；如果 runtime 已是 aborted 而 UI 仍显示后续内容，问题在前端 reducer/projection。

## 后续建议

本次先不保留未提交的 808 行防守性修改。后续若再次处理，建议先选定一个明确的长期边界：

1. 以 `run generation` 为 key 建立唯一 operation coordinator；或
2. 把 Decision resolve、Abort、continuation admission 设计成 backend 可验证的原子状态转换。

不建议继续在 frontend、handler 和 Manager 各自增加一个新的临时标记。无论选择哪一种，都应先补一条端到端 contract：

```text
pending decision
-> response and abort in both orders
-> exactly one terminal run
-> no continuation event after abort wins
```

在没有这条 contract 之前，继续增加局部 retry、timer 或 map 只能提高覆盖率，不能证明状态转换是正确的。
