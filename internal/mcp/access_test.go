package mcp

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"aegis-api-mcp/internal/config"
)

func TestBuildAccessStateBuildsPrefixAndToolMaps(t *testing.T) {
	t.Parallel()

	cfgs := []*config.APIConfig{
		{
			MCPPrefix: "github",
			APIKeys:   []string{"github-token"},
			ExposedEndpoints: []config.EndpointConfig{
				{MCPToolName: "github_list_issues"},
			},
		},
		{
			MCPPrefix: "jira",
			APIKeys:   []string{"jira-token"},
			ExposedEndpoints: []config.EndpointConfig{
				{MCPToolName: "jira_list_issues"},
			},
		},
	}

	tokenPrefixes, pathAliases, toolPrefixes, err := buildAccessState(cfgs)
	if err != nil {
		t.Fatalf("build access state: %v", err)
	}

	if got := tokenPrefixes["github-token"]; got != "github" {
		t.Fatalf("expected github-token to map to github, got %q", got)
	}
	if got := pathAliases["jira"]; got != "jira" {
		t.Fatalf("expected jira endpoint alias to map to jira, got %q", got)
	}
	if got := toolPrefixes["github_list_issues"]; got != "github" {
		t.Fatalf("expected github_list_issues to map to github, got %q", got)
	}
}

func TestBuildAccessStateRejectsSharedTokenAcrossPrefixes(t *testing.T) {
	t.Parallel()

	cfgs := []*config.APIConfig{
		{MCPPrefix: "github", APIKeys: []string{"shared-token"}},
		{MCPPrefix: "jira", APIKeys: []string{"shared-token"}},
	}

	_, _, _, err := buildAccessState(cfgs)
	if err == nil {
		t.Fatal("expected shared token error")
	}
	if !strings.Contains(err.Error(), `bearer token "shared-token" is assigned to both "github" and "jira"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildAccessStateRejectsToolNameCollision(t *testing.T) {
	t.Parallel()

	cfgs := []*config.APIConfig{
		{
			MCPPrefix: "github",
			ExposedEndpoints: []config.EndpointConfig{
				{MCPToolName: "list_issues"},
			},
		},
		{
			MCPPrefix: "jira",
			ExposedEndpoints: []config.EndpointConfig{
				{MCPToolName: "list_issues"},
			},
		},
	}

	_, _, _, err := buildAccessState(cfgs)
	if err == nil {
		t.Fatal("expected tool name collision error")
	}
	if !strings.Contains(err.Error(), `tool name "list_issues" is assigned to both "github" and "jira"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVisiblePrefixesForContextFiltersAccess(t *testing.T) {
	t.Parallel()

	ctx := withAccessContext(context.Background(), "demo-token", "jira")

	got := visiblePrefixesForContext(ctx, []string{"github", "jira", "slack"})
	if len(got) != 1 || got[0] != "jira" {
		t.Fatalf("unexpected visible prefixes: %v", got)
	}
}

func TestExtractAPIKeyFromRequestSupportsBearerOnly(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest("GET", "https://example.com/mcp/github/sse", nil)
	req.Header.Set("Authorization", "Bearer bearer-key")
	if got := extractAPIKeyFromRequest(req); got != "bearer-key" {
		t.Fatalf("expected bearer key, got %q", got)
	}

	req = httptest.NewRequest("GET", "https://example.com/mcp/github/sse", nil)
	req.Header.Set("Authorization", "plain-key")
	if got := extractAPIKeyFromRequest(req); got != "" {
		t.Fatalf("expected non-bearer auth header to be ignored, got %q", got)
	}
}

func TestEndpointBasePathScopesByPrefix(t *testing.T) {
	t.Parallel()

	if got := endpointBasePath("/mcp", "github"); got != "/mcp/github" {
		t.Fatalf("unexpected endpoint base path: %q", got)
	}
}
