---
name: twilight-ai
description: Assist with development in the Twilight AI Go SDK. Use when working in this repository, adding or updating providers, embeddings, tool calling, streaming, examples, or docs for Twilight AI.
---

# Twilight AI

## When To Use

Use this skill when the task involves `twilight-ai`, especially:

- implementing or refactoring SDK APIs in `sdk/`
- adding or updating providers under `provider/`
- working on `Client.Generate`, `Client.Stream`, `Request`, `ModelResult`, `Embed`, or `EmbedMany`
- migrating off `GenerateText`, `GenerateTextResult`, or `StreamText`
- adding tool-calling, streaming, reasoning, or embedding support
- writing examples, docs, or usage guidance for this library

## Project Snapshot

Twilight AI is a lightweight Go AI SDK with a provider-agnostic core API.

- Text generation: `sdk.Client.Generate` and `sdk.Client.Stream` over an `sdk.Request`,
  returning an `sdk.ModelResult` or an `sdk.ModelStream`
- Deprecated: `sdk.GenerateText`, `sdk.GenerateTextResult`, `sdk.StreamText`, which
  run the SDK's own multi-step tool loop and its option-built request/result types
- Embeddings: `sdk.Embed`, `sdk.EmbedMany`
- Tool calling: `sdk.Tool`, `sdk.ToolDefinition`, `sdk.ExecuteTools`, `sdk.BuildStepMessages`
- Deprecated: the client-side `WithMaxSteps` loop and its approval flow
- MCP tool integration: `sdk.CreateMCPClient`, `sdk.MCPClient`, `sdk.MCPClientConfig`
- Streaming: typed `StreamPart` events over Go channels
- Current providers:
  - `provider/openai/completions`
  - `provider/openai/responses`
  - `provider/anthropic/messages`
  - `provider/google/generativeai`
  - `provider/openai/embedding`
  - `provider/google/embedding`

## Default Mental Model

Prefer the single-call seam, then drop to provider details only when needed.

- `sdk.Model` binds a chat model to a `sdk.Provider`
- `sdk.EmbeddingModel` binds an embedding model to an `sdk.EmbeddingProvider`
- `sdk.Client.Generate` takes an `sdk.Model` and one `sdk.Request` and returns one
  `sdk.ModelResult`; `sdk.Client.Stream` returns an `sdk.ModelStream` of live
  `sdk.StreamPart` values. The model carries the provider binding, so
  `Request.Model` must be empty or match the model's ID
- A runtime owns the orchestration around those calls: the multi-step loop, tool
  execution through `sdk.ExecuteTools`, approvals, and its own step record.
  Deprecated: the client layer that used to own that loop
- MCP clients can load remote MCP tools and turn them into ordinary `sdk.Tool` values
- Providers handle backend-specific HTTP, request mapping, response parsing, and SSE translation

## Core API Guidance

Choose the narrowest API that matches the task:

- Need one model call and its result: build an `sdk.Request` and use `sdk.Client.Generate`
- Need live output: use `sdk.Client.Stream` and consume the `sdk.ModelStream` parts
- Need tool schemas on the wire: `sdk.ToolDefinition` (via `sdk.ToolDefinitionsFromTools`)
- Need to run the model's tool calls: `sdk.ExecuteTools`
- Need one vector: use `sdk.Embed`
- Need multiple vectors or embedding token usage: use `sdk.EmbedMany`
- Deprecated for text generation: `sdk.GenerateText`, `sdk.GenerateTextResult` and
  `sdk.StreamText` run the SDK's own multi-step tool loop

If the task introduces examples or docs, prefer simple end-to-end snippets that start with:

1. construct provider
2. get model
3. call SDK API
4. handle error

## Provider Selection Rules

- Use `openai/completions` for broad OpenAI-compatible support such as DeepSeek, Groq, Ollama, Azure-style compatible endpoints, and generic `/chat/completions` backends.
- Use `openai/responses` when the task needs OpenAI Responses API features such as first-class reasoning models, reasoning summaries, URL citation annotations, or flat input mapping.
- Use `anthropic/messages` for Claude and Anthropic extended thinking via `WithThinking`.
- Use `google/generativeai` for Gemini chat, tool calling, vision, streaming, and Gemini reasoning.
- Use `openai/embedding` or `google/embedding` for embeddings. Keep embedding-provider work separate from chat-provider work.

## Implementation Rules

### Chat Providers

If adding or changing a chat provider, preserve the `sdk.Provider` contract:

- `Name()`
- `ListModels(ctx)`
- `Test(ctx)`
- `TestModel(ctx, modelID)`
- `DoGenerate(ctx, req sdk.Request) (sdk.ModelResult, error)`
- `DoStream(ctx, req sdk.Request) (<-chan sdk.StreamPart, error)`

Keep provider responsibilities focused:

- translate an `sdk.Request` into backend request format
- parse backend responses into `sdk.ModelResult`
- map backend streaming events into typed `sdk.StreamPart` values, and let the SDK
  assemble them: a provider emits parts and stops, it never folds its own result
- report usage, finish reasons, reasoning, tool calls, sources, and files when supported

### Embedding Providers

Embedding providers are separate from chat providers. Use `sdk.EmbeddingProvider` and return an `sdk.EmbeddingModel` via `EmbeddingModel(id)`.

When updating embeddings:

- keep `sdk.Embed` for single-string convenience
- keep `sdk.EmbedMany` for batched requests
- preserve `Usage.Tokens`
- only expose dimensions/task-type behavior when the backend supports it

### Tool Calling

Prefer `sdk.NewTool[T]` for new tool examples and integrations. It gives typed input and inferred JSON Schema.

Use these defaults unless the task requires something else:

- `Request.ToolChoice` left zero, or `sdk.ToolChoice{Mode: sdk.ToolChoiceAuto}`, for normal use
- inspect `ModelResult.ToolCalls` without running anything for inspection-only use
- `sdk.ExecuteTools` to run one step's tool batch; the runtime owns the loop around it
- `RequireApproval: true` only for sensitive side effects

When streaming with tools, ensure the implementation can emit:

- tool input construction parts
- tool execution parts
- progress updates
- denial/error events when applicable

### MCP Tool Calling

Use MCP when the task needs remote tools exposed by an MCP server rather than locally implemented `Execute` handlers.

Default guidance:

- use `sdk.CreateMCPClient(ctx, &sdk.MCPClientConfig{...})`
- use `sdk.MCPTransportHTTP` for streamable HTTP MCP servers
- use `sdk.MCPTransportSSE` only when the server exposes legacy SSE transport
- for stdio, build the transport with the official MCP Go SDK and pass `Transport: ...`
- call `mcpClient.Tools(ctx)` and pass the result into `sdk.WithTools(...)`
- call `defer mcpClient.Close()` after successful creation

Important behavior:

- MCP tools become ordinary `sdk.Tool` values from the caller's perspective
- Twilight AI converts MCP `InputSchema` into `*jsonschema.Schema`
- MCP tool execution is delegated to `tools/call` on the remote server
- remote MCP text output becomes the tool result visible to the model

### Streaming

Twilight AI streaming is channel-first and type-safe. Prefer type switches over loosely typed event parsing.

Important expectations:

- `Client.Stream` returns an `sdk.ModelStream`
- `ModelStream.Parts` must be fully consumed before calling `ModelStream.Result`
- `sdk.CollectStream` folds a part channel into one `sdk.ModelResult` for backends
  whose only transport is streaming

### Messages And Results

Preserve the SDK message model and avoid backend-specific shapes leaking into public usage.

- user, assistant, system, and tool messages should stay in SDK types
- support rich parts where relevant: text, image, file, reasoning, tool call, tool result
- keep finish reason mapping aligned with SDK constants such as `stop`, `length`, `content-filter`, and `tool-calls`

## Common Task Patterns

### Add A New Usage Example

Use this structure:

1. pick the correct provider package
2. create provider with explicit options
3. create model via `ChatModel` or `EmbeddingModel`
4. call the top-level `sdk` function
5. show minimal but idiomatic result handling

### Add Or Update A Provider Feature

Check all affected layers:

1. request mapping
2. non-streaming response mapping
3. streaming event mapping
4. finish-reason and usage mapping
5. reasoning/tool/source/file support if the backend exposes them
6. model discovery and provider health checks if endpoints exist

### Add A Custom Provider

Use the built-in providers as the template. A custom provider should feel identical to existing ones from the caller's perspective.

Minimum behavior:

1. return a provider-bound model from `ChatModel`
2. implement discovery and health-check methods
3. support `DoGenerate`
4. support `DoStream` with correct lifecycle parts

## Documentation Rules

When writing Twilight AI docs or README content:

- prefer provider-agnostic phrasing first, provider-specific details second
- use Go examples, not pseudocode, unless explaining an interface contract
- keep examples small and runnable in spirit
- mention exact package paths for imports
- explain when to choose Completions vs Responses when OpenAI is involved
- keep embeddings, tool calling, and streaming as separate concerns unless the example truly combines them

## Terminology

Use these terms consistently:

- Provider: backend implementation for chat generation
- Embedding provider: backend implementation for embeddings
- Model: provider-bound chat model
- Embedding model: provider-bound embedding model
- Tool calling: model requests a tool invocation
- Multi-step execution: a tool loop the runtime owns, driving one `Client.Generate`
  or `Client.Stream` call per step
- Stream part: a typed event from `Client.Stream`

## Quick Checklist

Before finishing work in this repo, verify:

- the chosen provider package matches the intended backend capabilities
- chat and embedding concerns are not mixed accidentally
- public examples use top-level `sdk` APIs unless lower-level behavior is the point
- streaming logic uses typed `StreamPart` handling
- tool-calling changes cover both inspection of `ModelResult.ToolCalls` and execution through `sdk.ExecuteTools` when relevant
- MCP examples show both transport setup and normal `WithTools(...)` usage when relevant
- provider work includes health checks or model discovery behavior if the backend supports them

## Additional Resources

- For exported APIs, signatures, provider options, and stream/event types, see [reference.md](reference.md)
