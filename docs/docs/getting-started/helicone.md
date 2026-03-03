# Helicone Observability

Memoh supports optional [Helicone](https://helicone.ai) integration for LLM usage analytics. When enabled, all LLM API calls are routed through a Helicone gateway for token usage, cost tracking, and per-bot/per-channel breakdowns.

## Configuration

Add the `[helicone]` section to your `config.toml`:

```toml
[helicone]
enabled = true
api_key = "sk-helicone-your-key-here"
base_url = ""
```

| Field      | Description |
|------------|-------------|
| `enabled`  | Set to `true` to enable Helicone proxying. |
| `api_key`  | Your Helicone API key. |
| `base_url` | Leave empty for [Helicone Cloud](https://helicone.ai). For [self-hosted](https://docs.helicone.ai/getting-started/self-host), set to your instance URL (e.g., `http://helicone:8585`). |

Restart the services after updating:

```bash
docker compose restart server agent
```

## Custom Properties

Each request is automatically tagged with `BotId` and `Channel`. To view them in the Helicone dashboard, go to **Requests** → **Columns** → enable them under **Custom Properties**.

## Self-Hosted Notes

- Self-hosted Helicone validates target URLs against a built-in provider allowlist. Third-party OpenAI-compatible providers with custom URLs may not be supported. Use Helicone Cloud for full compatibility.
- When Memoh and Helicone run in separate Docker environments on the same host, use the Docker gateway IP (e.g., `http://172.17.0.1:8585`) instead of `localhost` for `base_url`.
