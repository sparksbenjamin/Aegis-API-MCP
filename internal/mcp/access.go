package mcp

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"aegis-api-mcp/internal/config"
)

type accessContextKey struct{}

type accessContext struct {
	Token  string
	Prefix string
}

func buildAccessState(cfgs []*config.APIConfig) (map[string]string, map[string]string, map[string]string, error) {
	tokenPrefixes := make(map[string]string)
	pathAliases := make(map[string]string)
	toolPrefixes := make(map[string]string)

	for _, cfg := range cfgs {
		endpointAlias := sanitizeAlias(cfg.MCPPrefix)
		if previousPrefix, exists := pathAliases[endpointAlias]; exists && previousPrefix != cfg.MCPPrefix {
			return nil, nil, nil, fmt.Errorf(
				"duplicate MCP endpoint alias %q from %q and %q",
				endpointAlias,
				previousPrefix,
				cfg.MCPPrefix,
			)
		}
		pathAliases[endpointAlias] = cfg.MCPPrefix

		for _, token := range cfg.APIKeys {
			if previousPrefix, exists := tokenPrefixes[token]; exists && previousPrefix != cfg.MCPPrefix {
				return nil, nil, nil, fmt.Errorf(
					"bearer token %q is assigned to both %q and %q",
					token,
					previousPrefix,
					cfg.MCPPrefix,
				)
			}
			tokenPrefixes[token] = cfg.MCPPrefix
		}

		for _, endpoint := range cfg.ExposedEndpoints {
			if previousPrefix, exists := toolPrefixes[endpoint.MCPToolName]; exists && previousPrefix != cfg.MCPPrefix {
				return nil, nil, nil, fmt.Errorf(
					"tool name %q is assigned to both %q and %q",
					endpoint.MCPToolName,
					previousPrefix,
					cfg.MCPPrefix,
				)
			}
			toolPrefixes[endpoint.MCPToolName] = cfg.MCPPrefix
		}
	}

	return tokenPrefixes, pathAliases, toolPrefixes, nil
}

func withAccessContext(ctx context.Context, token, prefix string) context.Context {
	return context.WithValue(ctx, accessContextKey{}, accessContext{
		Token:  token,
		Prefix: prefix,
	})
}

func accessFromContext(ctx context.Context) (accessContext, bool) {
	access, ok := ctx.Value(accessContextKey{}).(accessContext)
	return access, ok
}

func visiblePrefixesForContext(ctx context.Context, prefixes []string) []string {
	access, ok := accessFromContext(ctx)
	if !ok {
		out := append([]string(nil), prefixes...)
		sort.Strings(out)
		return out
	}

	visible := make([]string, 0, 1)
	for _, prefix := range prefixes {
		if prefix == access.Prefix {
			visible = append(visible, prefix)
			break
		}
	}
	sort.Strings(visible)
	return visible
}

func prefixAllowedForContext(ctx context.Context, prefix string) bool {
	access, ok := accessFromContext(ctx)
	if !ok {
		return true
	}
	return access.Prefix == prefix
}

func toolVisibleForContext(ctx context.Context, toolName string, toolPrefixes map[string]string) bool {
	if toolName == "aegis_status" {
		return true
	}

	access, ok := accessFromContext(ctx)
	if !ok {
		return true
	}

	prefix, exists := toolPrefixes[toolName]
	if !exists {
		return false
	}
	return prefix == access.Prefix
}

func extractAPIKeyFromRequest(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return ""
}

func endpointBasePath(basePath, prefix string) string {
	base := strings.TrimRight(strings.TrimSpace(basePath), "/")
	if base == "" {
		base = "/mcp"
	}
	return base + "/" + sanitizeAlias(prefix)
}
