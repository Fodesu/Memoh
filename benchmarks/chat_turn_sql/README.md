# Chat Turn SQL Benchmark

This benchmark measures PostgreSQL hot paths for the single-head chat turn model. It uses the current message read model on `bot_history_messages` (`turn_id`, `turn_position`, `turn_message_seq`, `turn_visible`) and does not add migrations or change the app schema. Seeded rows are written to normal app tables and tagged with a benchmark marker in `metadata`.

The runner is a closed-loop benchmark: each worker waits for one request/query to finish before sending the next. The default runner is `sqlc`, which calls generated Postgres sqlc methods and includes pgx scan/decode plus Go allocation cost. The optional `sql` runner is DB-only and row-drain-only: it drains rows from runnable SQL templates and is mainly for SQL microbenchmarks, `EXPLAIN`, and candidate SQL comparison, not Go scan or handler cost. The optional `http` runner uses Echo `httptest` to call the real `MessageHandler` path without TCP network noise.

## Run

```bash
go run ./benchmarks/chat_turn_sql \
  -config benchmarks/chat_turn_sql/config.example.toml \
  -mode seed-run
```

Useful modes:

```bash
go run ./benchmarks/chat_turn_sql -mode estimate
go run ./benchmarks/chat_turn_sql -mode seed
go run ./benchmarks/chat_turn_sql -mode run
go run ./benchmarks/chat_turn_sql -mode explain
go run ./benchmarks/chat_turn_sql -mode cleanup
go run ./benchmarks/chat_turn_sql -mode wrk-server
```

You can override the DSN and scenario without editing SQL:

```bash
go run ./benchmarks/chat_turn_sql \
  -dsn "$MEMOH_BENCH_DSN" \
  -scenario latest_page \
  -runner sqlc \
  -mode run
```

Go profiling can be enabled for the measured benchmark phase:

```bash
go run ./benchmarks/chat_turn_sql \
  -config benchmarks/chat_turn_sql/config.example.toml \
  -mode run \
  -cpuprofile benchmarks/chat_turn_sql/out/chat-turn.cpu.pprof \
  -memprofile benchmarks/chat_turn_sql/out/chat-turn.mem.pprof \
  -trace benchmarks/chat_turn_sql/out/chat-turn.trace.out

go tool pprof -top benchmarks/chat_turn_sql/out/chat-turn.cpu.pprof
go tool pprof -http=:8089 benchmarks/chat_turn_sql/out/chat-turn.cpu.pprof
go tool trace benchmarks/chat_turn_sql/out/chat-turn.trace.out
```

For `seed-run`, profiling starts after seeding completes, so data generation cost is excluded from the flame graph.

Runner modes:

- `workload.runner = "sqlc"` or `-runner sqlc`: generated Postgres sqlc methods, closest to the SaaS production Go hot path.
- `workload.runner = "sql"` or `-runner sql`: SQL template runner, drains rows only and supports `EXPLAIN`; use it as a DB-only microbench.
- `workload.runner = "http"` or `-runner http`: handler-level benchmark for `ListMessages` / `LocateMessage`, covering auth context parsing, bot/session authorization, selected-head validation, message service calls, UI conversion, decorators, JSON encoding, and optional JSON response decode.

HTTP runner scope notes:

- The handler is invoked directly through Echo `httptest` contexts, so Echo route matching, the middleware chain, and JWT signature verification are not measured; only claim extraction and everything below it are.
- The media service is not wired, so `fillAssetMimeFromStorage` no-ops and per-asset media mime resolution cost is not measured, even though the seed creates assets.
- HTTP `locate_window` calls `LocateMessage` with a symmetric window. It is intentionally not named `after_page`; low-level SQL `after_page` remains a standalone pagination component and should not be compared horizontally with HTTP locate results.

wrk sidecar benchmark:

`wrk` cannot call the in-process sqlc runner directly, and the public local-channel message endpoint is not equivalent to `write_turn_pair` / `write_tool_tail` because it enters the channel and agent pipeline. For wrk tests around the sqlc benchmark paths, start the benchmark-only sidecar:

```bash
go run ./benchmarks/chat_turn_sql \
  -dsn "$MEMOH_BENCH_DSN" \
  -scenario mixed_saas_read \
  -runner sqlc \
  -mode wrk-server \
  -addr 127.0.0.1:18083
```

Then run wrk against the sidecar:

```bash
MEMOH_WRK_SCENARIO=mixed_saas_read MEMOH_WRK_DIST=random \
  wrk -t4 -c16 -d30s --latency \
  -s benchmarks/chat_turn_sql/wrk_read.lua \
  http://127.0.0.1:18083

MEMOH_WRK_SCENARIO=mixed_saas_write MEMOH_WRK_DIST=random \
  wrk -t4 -c16 -d30s --latency \
  -s benchmarks/chat_turn_sql/wrk_write.lua \
  http://127.0.0.1:18083

MEMOH_WRK_SCENARIO=write_tool_tail MEMOH_WRK_DIST=hot \
  wrk -t4 -c16 -d30s --latency \
  -s benchmarks/chat_turn_sql/wrk_write.lua \
  http://127.0.0.1:18083
```

Supported `MEMOH_WRK_SCENARIO` values include `mixed_saas_read`, `chat_page_ui`, `before_page`, `after_page`, `locate_window`, `external_lookup`, `mixed_saas_write`, `write_turn_pair`, `write_tool_tail`, `write_user_message`, and `write_assistant_message`. `MEMOH_WRK_DIST=random` samples non-hot seeded sessions; `MEMOH_WRK_DIST=hot` samples hot sessions only. This measures real TCP/HTTP overhead around the benchmark sqlc/DBService paths, but it is still a benchmark adapter, not a public production API route. Public HTTP route coverage remains the `runner = "http"` benchmark.

`-mode explain` always uses SQL templates, even if the config says `runner = "sqlc"`, because generated sqlc methods do not expose a query string that can be prefixed with `EXPLAIN`.

## Configuration

Edit `config.example.toml` or pass another TOML file with `-config`.

Important knobs:

- `seed.bots`, `seed.sessions_per_bot`, `seed.turns_per_session`: total tenant/session/history scale.
- `seed.hot_session_ratio` and `workload.hot_traffic_ratio`: SaaS skew where a small set of sessions receives most traffic.
- `seed.branch_factor`, `seed.branch_depth`, `seed.active_heads_per_session`: legacy branching keys kept so old TOML files still parse. The current harness generates single-head linear history and ignores them.
- `seed.approval_every_n_turns`, `seed.user_input_every_n_turns`, `seed.asset_every_n_messages`: decoration/query side-table density.
- `workload.runner`: `sqlc` for production-path benchmark, `sql` for SQL templates.
- `workload.random_seed`: reproducible sampling.
- `workload.fail_on_error`: returns non-zero if measured query errors occur.
- `workload.query_weights`: scenario mix for `mixed_saas_read`. The default mix uses semantic production-ish paths (`chat_page_ui`, `before_page`, `locate_window`, `approval_resolve`, `user_input_resolve`) instead of randomly interleaving every component query. Keep low-level scenarios such as `latest_page`, `before_page`, `after_page`, and `external_lookup` for SQL component microbenchmarks.
- `mixed_saas_write` uses the same `workload.query_weights` table, but with write scenarios. If no weights are provided, it defaults to `write_turn_pair = 55`, `write_tool_tail = 35`, `write_user_message = 5`, and `write_assistant_message = 5`.
- `workload.http_format`: HTTP runner query `format`. Use `"ui"` for chat UI shape or `""` for raw message REST shape.
- `workload.http_decode_json`: HTTP runner decodes the JSON response and counts `items` when true; disabling it counts response bytes only.
- `workload.serialize_session_writes`: write runner only. Defaults to true for write scenarios. It models the normal one-writer-per-session chat flow and prevents artificial same-session races in `Persist(assistant)`. Set it to false only to intentionally test same-session concurrent write conflicts.
- `output.explain`: writes `EXPLAIN (ANALYZE, BUFFERS, WAL, FORMAT JSON)` per hot query.

Config parsing is strict. Unknown keys and invalid explicit values fail early instead of being silently clamped.

## Scenarios

- `chat_page_ui`: semantic chat UI page path. `runner=http` calls `ListMessages(format=ui)` and covers handler UI conversion/decorators. `runner=sqlc` / `runner=sql` run the SQL component bundle: latest page, message assets, and tool-call decoration lookups when the seed has matching tool calls.
- `locate_window`: semantic locate path. `runner=http` calls `/messages/locate` with a before/after window. `runner=sqlc` / `runner=sql` run external lookup plus before/after window components. Do not compare this with the low-level `after_page` component scenario.
- `approval_resolve`: representative approval resolution path for non-page UI operations.
- `user_input_resolve`: representative user-input resolution path for non-page UI operations.
- `latest_page`: low-level latest visible message page component.
- `before_page`: low-level cursor pagination toward older messages. Cursors are sampled from old/mid/recent positions in the same single-head session.
- `after_page`: low-level cursor pagination toward newer messages. This is a SQL component scenario only; the HTTP runner uses `locate_window` for the handler locate path.
- `external_lookup`: low-level visible-message lookup by external message id. Seeds sample recent/mid/old external ids instead of always using the first message.
- `approval_tool_calls`: direct UI decoration query for `ListToolApprovalsBySessionToolCalls`.
- `user_input_tool_calls`: direct UI decoration query for `ListUserInputsBySessionToolCalls`.
- `write_turn_pair`: production message write path for a normal user+assistant turn. It calls `message.DBService.Persist` twice and covers `CreateMessageWithTurn`, `CreateHistoryTurnWithID`, `CreateMessageInHistoryTurnByRequest`, and `BindHistoryTurnAssistantByRequest`.
- `write_user_message`: production message write path for a user message. It calls `message.DBService.Persist` and covers direct message+turn linking via `CreateMessageWithTurn` plus `CreateHistoryTurnWithID`.
- `write_assistant_message`: production message write path for an assistant message. It calls `message.DBService.Persist` and covers message insert plus binding or appending to the latest visible turn.
- `write_tool_tail`: hot agent-tool write path for `user -> assistant(tool-call) -> tool result -> assistant final`. It covers direct user turn creation, request-bound assistant binding, and direct request-turn insertion for tool/final assistant tail messages.
- `approval_pending_list`, `approval_latest`, `approval_short_id`, `approval_reply_message`: production tool approval read paths kept as component microbenchmarks.
- `user_input_pending_list`, `user_input_latest`, `user_input_short_id`, `user_input_reply_message`: production user input read paths kept as component microbenchmarks.
- `runner=http` supports `chat_page_ui`, `latest_page`, `before_page`, `locate_window`, and `external_lookup`.
- `mixed_saas_read`: weighted mix of the configured scenarios.
- `mixed_saas_write`: weighted mix of write scenarios. It requires `runner = "sqlc"` and mutates the seeded benchmark rows. Use `hot_traffic_ratio = 1.0` with a very small hot set to measure single-session serialized throughput; set `serialize_session_writes = false` only when deliberately probing unsafe same-session concurrent write behavior.

## Output

The runner prints a compact table and writes JSON/CSV files with:

- `runner`, `query_source`, and `scan_mode`
- count and errors
- rows returned
- p50, p90, p95, p99, max, average latency
- per-query QPS
- production source query name and argument shape

`count`, latency, and QPS include successful queries only. `total_count`, `errors`, `error_rate`, and `last_error` are reported separately. With the default `fail_on_error = true`, a run with measured errors still writes JSON/CSV, then exits with an error. For `sqlc`, `scan_mode = sqlc_struct_scan`; for `sql`, `scan_mode = row_drain_only`; for `http`, `scan_mode = http_json_decode`. For `http`, `rows` means decoded UI/API items (or response bytes when JSON decode is disabled), not SQL rows, so do not compare HTTP row counts with `sqlc`/`sql` row counts.

Benchmark-owned rows can be removed with:

```bash
go run ./benchmarks/chat_turn_sql -mode cleanup
```
