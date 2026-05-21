# Aegis API Config Guide

This guide explains the JSON files in `configs/` and what each option does.

The important model is:

- one `.json` file = one upstream API surface
- one config = one MCP endpoint
- one config = one set of exposed MCP tools
- one config = one bearer-token boundary for SSE

## Minimal example

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
    "Accept": "application/vnd.github+json"
  },
  "exposed_endpoints": [
    {
      "path": "/repos/{owner}/{repo}/issues",
      "method": "GET",
      "mcp_tool_name": "github_list_issues",
      "description": "Lists issues for a repository."
    }
  ]
}
```

## Supported fields

### `mcp_prefix`

Required.

This becomes the endpoint path:

```text
/mcp/<mcp_prefix>/sse
```

### `base_url`

Required.

This is the upstream API base URL. It must use `http` or `https`.

Example:

```json
"base_url": "https://api.github.com"
```

### `api_keys`

Optional for `stdio`, but effectively required for `sse`.

These are the bearer tokens allowed to connect to this one MCP prefix.

Example:

```json
"api_keys": [
  "change-me-github-mcp-key"
]
```

### `auth`

Optional.

This is the upstream API authentication model, not the MCP endpoint auth.

Supported `type` values:

- `Bearer`
- `Basic`
- `Header`
- `Query`

Bearer example:

```json
"auth": {
  "type": "Bearer",
  "token": "ghp_secureSecretHere"
}
```

Static header example:

```json
"auth": {
  "type": "Header",
  "header_name": "X-API-Key",
  "value": "secure-value"
}
```

### `default_headers`

Optional.

These headers are sent on every upstream request for this config.

Example:

```json
"default_headers": {
  "Accept": "application/json"
}
```

### `timeout_seconds`

Optional.

Default:

```json
30
```

### `allow_insecure_tls`

Optional.

Default:

```json
false
```

Set this to `true` only for local or lab APIs using self-signed certificates.

### `exposed_endpoints`

Required.

This is the list of upstream operations that become MCP tools.

Each item supports:

- `path`
- `method`
- `mcp_tool_name`
- `description`

Path placeholders such as `{owner}` become required tool arguments.

Example:

```json
{
  "path": "/repos/{owner}/{repo}/issues",
  "method": "POST",
  "mcp_tool_name": "github_create_issue",
  "description": "Creates a new issue in a repository."
}
```

## Tool argument model

For an endpoint like:

```json
{
  "path": "/repos/{owner}/{repo}/issues",
  "method": "POST",
  "mcp_tool_name": "github_create_issue"
}
```

the tool accepts:

- `owner`
- `repo`
- `query`
- `headers`
- `body`
- `body_raw_json`

Use `body` for ordinary JSON objects and `body_raw_json` when you need a raw JSON string instead.

## Hot reload behavior

Aegis watches `configs/` and hot-reloads changes:

- adding a config file creates a new MCP prefix
- editing a config file updates its tools and auth
- deleting a config file removes that MCP prefix

Invalid config files are skipped with a warning so a single bad file does not take down the service.
