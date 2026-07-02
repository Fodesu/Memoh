# Graph Metadata Benchmark Comparison

Date: 2026-07-02

Branch: `optimize/chat-turn-hot-path`

Scenario: `graph_metadata`

Runner: `sqlc`

Seed marker: `saas-chat-turn-sql-20260702`

Seed scale:

| metric | value |
|---|---:|
| bots | 8 |
| sessions | 400 |
| turns | 1,076,800 |
| messages | 2,153,600 |
| heads | 1,200 |
| approvals | 26,920 |
| user inputs | 17,946 |
| assets | 10,768 |

Workload:

| setting | value |
|---|---:|
| duration | 30s |
| warmup | 5s |
| concurrency | 16 |
| max DB conns | 32 |
| hot traffic ratio | 0.95 |
| selected head ratio | 0.35 |

Commits:

| label | commit | description |
|---|---|---|
| baseline | `48172570` | before `perf(chat-turn): collapse graph metadata reads` |
| optimized | `ce8dc25e` | after `perf(chat-turn): collapse graph metadata reads` |

Result files:

| label | json | csv | config |
|---|---|---|---|
| baseline | `graph-metadata-baseline-results.json` | `graph-metadata-baseline-results.csv` | `graph-metadata-baseline.toml` |
| optimized | `graph-metadata-optimized-results.json` | `graph-metadata-optimized-results.csv` | `graph-metadata-optimized.toml` |

Results:

| label | total | ok | errors | rows | p50 | p90 | p95 | p99 | max | avg | qps |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| baseline | 2,902 | 2,902 | 0 | 7,812,184 | 158.534ms | 209.586ms | 240.081ms | 301.041ms | 479.875ms | 164.960ms | 96.73 |
| optimized | 3,031 | 3,031 | 0 | 8,159,452 | 150.698ms | 209.308ms | 235.401ms | 306.504ms | 472.001ms | 157.662ms | 101.03 |

Delta:

| metric | change |
|---|---:|
| p50 | -4.94% |
| p90 | -0.13% |
| p95 | -1.95% |
| p99 | +1.82% |
| avg | -4.42% |
| qps | +4.45% |

Notes:

- This is a same-seed, same-workload, consecutive local macOS run.
- The optimized query did less SQL and Go work, but this specific paired run shows only modest latency movement. Earlier ad hoc runs showed a larger p99 drop, so this scenario is sensitive to local PostgreSQL cache and machine load.
- Use these saved JSON/CSV files as the formal result for this run.
