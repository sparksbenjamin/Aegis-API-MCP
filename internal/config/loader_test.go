package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAPIConfigAppliesDefaultsAndExtractsPathParams(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "github.json")
	err := os.WriteFile(path, []byte(`{
  "mcp_prefix": "github",
  "base_url": "https://api.github.com/",
  "api_keys": [" alpha ", "", "beta", "alpha"],
  "auth": {
    "type": "Bearer",
    "token": "ghp_test"
  },
  "exposed_endpoints": [
    {
      "path": "/repos/{owner}/{repo}/issues",
      "method": "GET",
      "mcp_tool_name": "github_list_issues",
      "description": "List issues."
    }
  ]
}`), 0o600)
	if err != nil {
		t.Fatalf("write API config: %v", err)
	}

	cfg, err := LoadAPIConfig(path)
	if err != nil {
		t.Fatalf("load API config: %v", err)
	}

	if cfg.TimeoutSeconds != 30 {
		t.Fatalf("expected default timeout 30, got %d", cfg.TimeoutSeconds)
	}
	if cfg.BaseURL != "https://api.github.com" {
		t.Fatalf("expected trimmed base URL, got %q", cfg.BaseURL)
	}
	if len(cfg.APIKeys) != 2 || cfg.APIKeys[0] != "alpha" || cfg.APIKeys[1] != "beta" {
		t.Fatalf("unexpected normalized API keys: %v", cfg.APIKeys)
	}
	if len(cfg.ExposedEndpoints[0].PathParams) != 2 {
		t.Fatalf("expected 2 path params, got %d", len(cfg.ExposedEndpoints[0].PathParams))
	}
	if cfg.ExposedEndpoints[0].PathParams[0] != "owner" || cfg.ExposedEndpoints[0].PathParams[1] != "repo" {
		t.Fatalf("unexpected path params: %v", cfg.ExposedEndpoints[0].PathParams)
	}
}

func TestValidateRejectsMissingBearerToken(t *testing.T) {
	t.Parallel()

	cfg := &APIConfig{
		MCPPrefix: "github",
		BaseURL:   "https://api.github.com",
		Auth: AuthConfig{
			Type: AuthTypeBearer,
		},
		ExposedEndpoints: []EndpointConfig{
			{
				Path:        "/repos/{owner}/{repo}/issues",
				Method:      "GET",
				MCPToolName: "github_list_issues",
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected bearer token validation error")
	}
	if !strings.Contains(err.Error(), `auth.token is required`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScanConfigDirRejectsDuplicatePrefixes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	first := filepath.Join(dir, "one.json")
	second := filepath.Join(dir, "two.json")

	for _, path := range []string{first, second} {
		err := os.WriteFile(path, []byte(`{
  "mcp_prefix": "github",
  "base_url": "https://api.github.com",
  "exposed_endpoints": [
    {
      "path": "/repos/{owner}/{repo}/issues",
      "method": "GET",
      "mcp_tool_name": "github_list_issues"
    }
  ]
}`), 0o600)
		if err != nil {
			t.Fatalf("write config %s: %v", path, err)
		}
	}

	_, err := ScanConfigDir(dir)
	if err == nil {
		t.Fatal("expected duplicate prefix error")
	}
	if !strings.Contains(err.Error(), `duplicate mcp_prefix "github"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
