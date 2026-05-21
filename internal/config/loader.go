package config

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	pathVarPattern  = regexp.MustCompile(`\{([A-Za-z0-9_]+)\}`)
)

type AuthType string

const (
	AuthTypeBearer AuthType = "Bearer"
	AuthTypeBasic  AuthType = "Basic"
	AuthTypeHeader AuthType = "Header"
	AuthTypeQuery  AuthType = "Query"
)

type APIConfig struct {
	MCPPrefix        string            `json:"mcp_prefix"`
	BaseURL          string            `json:"base_url"`
	APIKeys          []string          `json:"api_keys"`
	DefaultHeaders   map[string]string `json:"default_headers"`
	TimeoutSeconds   int               `json:"timeout_seconds"`
	AllowInsecureTLS bool              `json:"allow_insecure_tls"`
	Auth             AuthConfig        `json:"auth"`
	ExposedEndpoints []EndpointConfig  `json:"exposed_endpoints"`
}

type AuthConfig struct {
	Type       AuthType `json:"type"`
	Token      string   `json:"token"`
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	HeaderName string   `json:"header_name"`
	Value      string   `json:"value"`
	QueryName  string   `json:"query_name"`
}

type EndpointConfig struct {
	Path        string   `json:"path"`
	Method      string   `json:"method"`
	MCPToolName string   `json:"mcp_tool_name"`
	Description string   `json:"description"`
	PathParams  []string `json:"-"`
}

func (c *APIConfig) Validate() error {
	c.MCPPrefix = strings.TrimSpace(c.MCPPrefix)
	if c.MCPPrefix == "" {
		return fmt.Errorf("mcp_prefix is required")
	}

	c.BaseURL = strings.TrimSpace(c.BaseURL)
	if c.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	parsedBaseURL, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("base_url is invalid: %w", err)
	}
	if parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https" {
		return fmt.Errorf("base_url must use http or https")
	}
	if parsedBaseURL.Host == "" {
		return fmt.Errorf("base_url host is required")
	}
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")

	if c.TimeoutSeconds == 0 {
		c.TimeoutSeconds = 30
	}
	if c.TimeoutSeconds < 0 {
		return fmt.Errorf("timeout_seconds must be greater than or equal to 0")
	}

	if err := c.Auth.Validate(); err != nil {
		return err
	}

	c.APIKeys = normalizeStringList(c.APIKeys)

	cleanHeaders := make(map[string]string, len(c.DefaultHeaders))
	for key, value := range c.DefaultHeaders {
		cleanKey := strings.TrimSpace(key)
		if cleanKey == "" {
			return fmt.Errorf("default_headers contains an empty header name")
		}
		if _, err := http.NewRequest(http.MethodGet, c.BaseURL, nil); err != nil {
			return fmt.Errorf("base_url is invalid: %w", err)
		}
		cleanHeaders[cleanKey] = strings.TrimSpace(value)
	}
	c.DefaultHeaders = cleanHeaders

	if len(c.ExposedEndpoints) == 0 {
		return fmt.Errorf("exposed_endpoints must contain at least one endpoint")
	}

	seenToolNames := make(map[string]struct{}, len(c.ExposedEndpoints))
	for i := range c.ExposedEndpoints {
		if err := c.ExposedEndpoints[i].Validate(); err != nil {
			return fmt.Errorf("endpoint %d: %w", i, err)
		}
		if _, exists := seenToolNames[c.ExposedEndpoints[i].MCPToolName]; exists {
			return fmt.Errorf("duplicate mcp_tool_name %q within config", c.ExposedEndpoints[i].MCPToolName)
		}
		seenToolNames[c.ExposedEndpoints[i].MCPToolName] = struct{}{}
	}

	return nil
}

func (a *AuthConfig) Validate() error {
	a.Type = AuthType(strings.TrimSpace(string(a.Type)))
	switch a.Type {
	case "":
		return nil
	case AuthTypeBearer:
		if strings.TrimSpace(a.Token) == "" {
			return fmt.Errorf("auth.token is required when auth.type is %q", AuthTypeBearer)
		}
	case AuthTypeBasic:
		if strings.TrimSpace(a.Username) == "" {
			return fmt.Errorf("auth.username is required when auth.type is %q", AuthTypeBasic)
		}
		if a.Password == "" {
			return fmt.Errorf("auth.password is required when auth.type is %q", AuthTypeBasic)
		}
	case AuthTypeHeader:
		if strings.TrimSpace(a.HeaderName) == "" {
			return fmt.Errorf("auth.header_name is required when auth.type is %q", AuthTypeHeader)
		}
		if a.Value == "" {
			return fmt.Errorf("auth.value is required when auth.type is %q", AuthTypeHeader)
		}
	case AuthTypeQuery:
		if strings.TrimSpace(a.QueryName) == "" {
			return fmt.Errorf("auth.query_name is required when auth.type is %q", AuthTypeQuery)
		}
		if a.Value == "" {
			return fmt.Errorf("auth.value is required when auth.type is %q", AuthTypeQuery)
		}
	default:
		return fmt.Errorf("auth.type must be empty, %q, %q, %q, or %q", AuthTypeBearer, AuthTypeBasic, AuthTypeHeader, AuthTypeQuery)
	}

	return nil
}

func (e *EndpointConfig) Validate() error {
	e.Path = strings.TrimSpace(e.Path)
	if e.Path == "" {
		return fmt.Errorf("path is required")
	}
	if !strings.HasPrefix(e.Path, "/") {
		return fmt.Errorf("path must start with '/'")
	}
	if strings.Contains(e.Path, " ") {
		return fmt.Errorf("path must not contain spaces")
	}
	remainder := pathVarPattern.ReplaceAllString(e.Path, "")
	if strings.Contains(remainder, "{") || strings.Contains(remainder, "}") {
		return fmt.Errorf("path contains malformed template placeholders")
	}

	e.Method = strings.ToUpper(strings.TrimSpace(e.Method))
	if e.Method == "" {
		return fmt.Errorf("method is required")
	}
	switch e.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions:
	default:
		return fmt.Errorf("unsupported method %q", e.Method)
	}

	e.MCPToolName = strings.TrimSpace(e.MCPToolName)
	if e.MCPToolName == "" {
		return fmt.Errorf("mcp_tool_name is required")
	}
	if !toolNamePattern.MatchString(e.MCPToolName) {
		return fmt.Errorf("mcp_tool_name %q must contain only letters, digits, underscores, or hyphens", e.MCPToolName)
	}

	matches := pathVarPattern.FindAllStringSubmatch(e.Path, -1)
	seenPathParams := make(map[string]struct{}, len(matches))
	e.PathParams = e.PathParams[:0]
	for _, match := range matches {
		name := match[1]
		if _, exists := seenPathParams[name]; exists {
			return fmt.Errorf("path placeholder %q is duplicated", name)
		}
		seenPathParams[name] = struct{}{}
		e.PathParams = append(e.PathParams, name)
	}

	e.Description = strings.TrimSpace(e.Description)

	return nil
}

func LoadAPIConfig(path string) (*APIConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var cfg APIConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation for %s: %w", path, err)
	}

	return &cfg, nil
}

func ScanConfigDir(dir string) ([]*APIConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("configs directory %q does not exist", dir)
		}
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}

	var cfgs []*APIConfig
	seenPrefixes := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		cfg, err := LoadAPIConfig(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[AEGIS] WARNING: skipping %s: %v\n", entry.Name(), err)
			continue
		}
		if previous, exists := seenPrefixes[cfg.MCPPrefix]; exists {
			return nil, fmt.Errorf("duplicate mcp_prefix %q in %s and %s", cfg.MCPPrefix, previous, path)
		}

		seenPrefixes[cfg.MCPPrefix] = path
		cfgs = append(cfgs, cfg)
	}

	return cfgs, nil
}

func normalizeStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}
