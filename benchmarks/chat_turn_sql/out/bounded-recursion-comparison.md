# Latest/Before Bounded Recursion Benchmark Comparison

Date: 2026-07-02

Branch: `optimize/chat-turn-hot-path`

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
| page size | 50 |
| hot traffic ratio | 0.95 |
| selected head ratio | 0.35 |

Commits:

| label | commit |
|---|---|
| baseline | `56a3a6cb` |
| optimized | `ace6d29a` |

Results:

| scenario | label | total | ok | errors | p50 | p95 | p99 | avg | qps |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|
| latest_page | baseline | 9,980 | 9,980 | 0 | 47.124ms | 66.988ms | 84.764ms | 48.065ms | 332.67 |
| latest_page | optimized | 199,186 | 199,186 | 0 | 1.978ms | 5.089ms | 9.312ms | 2.409ms | 6,639.53 |
| before_page | baseline | 10,024 | 10,024 | 0 | 48.071ms | 81.437ms | 100.230ms | 47.824ms | 334.13 |
| before_page | optimized | 19,639 | 19,639 | 0 | 21.460ms | 46.159ms | 78.373ms | 24.430ms | 654.63 |

Delta:

| scenario | p50 | p95 | p99 | avg | qps |
|---|---:|---:|---:|---:|---:|
| latest_page | -95.80% | -92.40% | -89.02% | -94.99% | +1,895.27% |
| before_page | -55.36% | -43.32% | -21.81% | -48.92% | +95.92% |

Result files:

| scenario | label | json | csv | config |
|---|---|---|---|---|
| latest_page | baseline | `bounded-latest-baseline-results.json` | `bounded-latest-baseline-results.csv` | `bounded-latest-baseline.toml` |
| latest_page | optimized | `bounded-latest-optimized-results.json` | `bounded-latest-optimized-results.csv` | `bounded-latest-optimized.toml` |
| before_page | baseline | `bounded-before-baseline-results.json` | `bounded-before-baseline-results.csv` | `bounded-before-baseline.toml` |
| before_page | optimized | `bounded-before-optimized-results.json` | `bounded-before-optimized-results.csv` | `bounded-before-optimized.toml` |

Notes:

- `latest_page` now stops ancestor recursion once enough messages are covered for the requested page.
- `before_page` uses the cursor message when available: it first verifies cursor visibility from the selected head, then starts bounded recursion from the cursor turn toward older ancestors.
- The timestamp-only `before` fallback remains supported and bounded by counting eligible messages per turn.
- This is a local macOS PostgreSQL run on the existing SaaS seed database. Treat exact numbers as local-machine dependent, but the direction and magnitude for `latest_page` are large enough to be robust.
