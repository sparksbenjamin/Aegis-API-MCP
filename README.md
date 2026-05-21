# Aegis-API-MCP

![Go 1.23](https://img.shields.io/badge/Go-1.23-00ADD8?style=flat-square&logo=go)
![MCP compatible](https://img.shields.io/badge/MCP-Compatible-blue?style=flat-square)
![Docker supported](https://img.shields.io/badge/Docker-Supported-2496ED?style=flat-square&logo=docker)
![MIT license](https://img.shields.io/github/license/sparksbenjamin/Aegis-API-MCP?style=flat-square)

A thin MCP-native HTTP API bridge for AI agents.

Aegis lets you expose a tightly scoped set of upstream API endpoints as MCP tools without giving an agent a full arbitrary HTTP client.

One JSON config file equals one MCP prefix. Each config gets its own SSE endpoint:

```text
http://YOUR_AEGIS_HOST:8443/mcp/<prefix>/sse
```

and only exposes the endpoint tools declared in that one config file.

Quick links:

- [Quick start](#quick-start)
- [How it works](#how-it-works)
- [Configuration](#configuration)
- [Connect a client](#connect-a-client)
- [Docs](#docs)

## Quick start

The checked-in [docker-compose.yml](docker-compose.yml) is the recommended deployment path. It runs Aegis over SSE on `http://localhost:8443` by default.

1. Create or populate this folder next to `docker-compose.yml`:

```text
./configs
```

2. Add a config file in `configs/github.json`:

```json
{
  "mcp_prefix": "github",
  "base_url": "https://api.github.com",
  "api_keys": [
    "change-me-github-mcp-key"
  ],
  "auth": {
    "type": "Bearer",
    "token": "ghp_secureSecretHere"
  },
  "default_headers": {
    "Accept": "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28"
  },
  "exposed_endpoints": [
    {
      "path": "/repos/{owner}/{repo}/issues",
      "method": "GET",
      "mcp_tool_name": "github_list_issues",
      "description": "Lists issues for a specific repository."
    },
    {
      "path": "/repos/{owner}/{repo}/issues",
      "method": "POST",
      "mcp_tool_name": "github_create_issue",
      "description": "Creates a new issue in a repository."
    }
  ]
}
```

3. Start the service:

```bash
docker compose pull
docker compose up -d
docker compose logs -f aegis-api-mcp
```

4. Connect your MCP client to:

```text
http://localhost:8443/mcp/github/sse
Authorization: Bearer change-me-github-mcp-key
```

If you want HTTPS, uncomment the TLS lines in [docker-compose.yml](docker-compose.yml) and set `AEGIS_SSE_BASE_URL` to your real external address.

<details>
<summary>Build from source</summary>

```bash
git clone https://github.com/sparksbenjamin/Aegis-API-MCP.git
cd Aegis-API-MCP
go build -o aegis-api-mcp .
```

</details>

## Why it exists

Most agents either get:

- no API access at all
- full arbitrary HTTP tooling with too much reach

Aegis is the middle path:

> Keep the upstream API authoritative.
> Keep the integration surface explicit.
> Keep the blast radius small.

## What it does

- Creates one MCP SSE endpoint per config file
- Exposes only the listed upstream API operations as MCP tools
- Keeps upstream auth in the config file instead of the agent prompt
- Supports both `sse` and `stdio` transport
- Hot-reloads config changes from disk
- Adds bearer-token isolation per MCP prefix for SSE
- Supports bearer, basic, static header, and static query auth upstream

## What it does not do

- Discover or introspect an API automatically from OpenAPI
- Validate response schemas or approval workflows
- Turn a large API into a safe surface without deliberate curation
- Replace upstream IAM, rate limits, or audit controls

## How it works

For each config file, Aegis registers the declared tools and binds them to one prefix-scoped SSE endpoint. When a tool is called, Aegis substitutes path parameters, applies configured auth, forwards the request upstream, and returns the upstream response body to the MCP client.

```text
+-------------------+
| MCP Client / LLM  |
+---------+---------+
          |
          | MCP (HTTP/SSE or stdio)
          |
+---------v---------+
|  Aegis-API-MCP    |
|-------------------|
| Bearer Auth       |
| Prefix Isolation  |
| Config Hot Reload |
| Upstream HTTP     |
+---------+---------+
          |
          | HTTPS / HTTP
          |
+---------v---------+
| Upstream API      |
|-------------------|
| GitHub / Jira     |
| Slack / Internal  |
| Existing Auth     |
+-------------------+
```

## Configuration

### One config file equals one MCP prefix

One JSON file in `configs/` becomes:

- one MCP SSE endpoint
- one bearer-token boundary
- one set of MCP tools
- one upstream base URL and auth model

The `mcp_prefix` becomes the URL segment:

```text
/mcp/<prefix>/sse
```

Use lowercase letters, digits, and hyphens when possible.

### Tool arguments

Each exposed endpoint tool accepts:

- required top-level path parameters such as `owner` or `repo`
- optional `query` object for query string parameters
- optional `headers` object for extra upstream headers
- optional `body` object for JSON request bodies
- optional `body_raw_json` string for advanced body shapes

## Connect a client

For a config like:

```json
{
  "mcp_prefix": "github",
  "api_keys": [
    "change-me-github-mcp-key"
  ]
}
```

the client should connect to:

```text
http://localhost:8443/mcp/github/sse
```

and send:

```text
Authorization: Bearer change-me-github-mcp-key
```

Quick reachability check:

```bash
curl -i -N \
  -H "Authorization: Bearer change-me-github-mcp-key" \
  http://localhost:8443/mcp/github/sse
```

## Docs

- Full config guide: [docs/config.md](docs/config.md)

## License

MIT License
