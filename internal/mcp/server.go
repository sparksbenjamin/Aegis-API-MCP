package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/fsnotify/fsnotify"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"aegis-api-mcp/internal/api"
	"aegis-api-mcp/internal/audit"
	"aegis-api-mcp/internal/config"
)

type AegisServer struct {
	mcpSrv *mcpserver.MCPServer
	logger *audit.Logger

	watcher    *fsnotify.Watcher
	configsDir string
	startedAt  time.Time

	mu             sync.RWMutex
	configs        map[string]*config.APIConfig
	apiKeyPrefixes map[string]string
	pathAliases    map[string]string
	toolPrefixes   map[string]string
	sseServers     map[string]http.Handler
	sessionAccess  map[string]accessContext
	httpSrv        *http.Server
	sseCfg         *SSEConfig
	stopCh         chan struct{}
	stopOnce       sync.Once
}

func NewAegisServer(configsDir string, logger *audit.Logger) (*AegisServer, error) {
	hooks := &mcpserver.Hooks{}
	mcpSrv := mcpserver.NewMCPServer("Aegis-API-MCP", "1.0.0", mcpserver.WithHooks(hooks))

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}
	if err := watcher.Add(configsDir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("watch %s: %w", configsDir, err)
	}

	a := &AegisServer{
		mcpSrv:         mcpSrv,
		logger:         logger,
		watcher:        watcher,
		configsDir:     configsDir,
		startedAt:      time.Now(),
		configs:        make(map[string]*config.APIConfig),
		apiKeyPrefixes: make(map[string]string),
		pathAliases:    make(map[string]string),
		toolPrefixes:   make(map[string]string),
		sseServers:     make(map[string]http.Handler),
		sessionAccess:  make(map[string]accessContext),
		stopCh:         make(chan struct{}),
	}

	hooks.AddAfterListTools(func(ctx context.Context, _ any, _ *mcp.ListToolsRequest, result *mcp.ListToolsResult) {
		if result == nil {
			return
		}

		a.mu.RLock()
		toolPrefixes := cloneStringMap(a.toolPrefixes)
		a.mu.RUnlock()

		filtered := make([]mcp.Tool, 0, len(result.Tools))
		for _, tool := range result.Tools {
			if toolVisibleForContext(ctx, tool.Name, toolPrefixes) {
				filtered = append(filtered, tool)
			}
		}
		result.Tools = filtered
	})

	hooks.AddOnRegisterSession(func(ctx context.Context, session mcpserver.ClientSession) {
		access, ok := accessFromContext(ctx)
		if !ok {
			return
		}

		a.mu.Lock()
		a.sessionAccess[session.SessionID()] = access
		a.mu.Unlock()

		go func() {
			<-ctx.Done()
			a.mu.Lock()
			delete(a.sessionAccess, session.SessionID())
			a.mu.Unlock()
		}()
	})

	if err := a.syncConfigs(); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("sync configs: %w", err)
	}

	a.mu.RLock()
	configCount := len(a.configs)
	a.mu.RUnlock()

	fmt.Fprintf(os.Stderr, "[AEGIS] Registered %d API config(s).\n", configCount)

	return a, nil
}

func (a *AegisServer) Stop() {
	a.stopOnce.Do(func() {
		close(a.stopCh)
		a.mu.RLock()
		httpSrv := a.httpSrv
		a.mu.RUnlock()
		if httpSrv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(ctx)
		}
		_ = a.watcher.Close()
	})
}

func (a *AegisServer) syncConfigs() error {
	cfgs, err := config.ScanConfigDir(a.configsDir)
	if err != nil {
		return err
	}

	apiKeyPrefixes, pathAliases, toolPrefixes, err := buildAccessState(cfgs)
	if err != nil {
		return err
	}

	desired := make(map[string]*config.APIConfig, len(cfgs))
	for _, cfg := range cfgs {
		desired[cfg.MCPPrefix] = cfg
	}

	tools := a.buildServerTools(cfgs)
	a.mcpSrv.SetTools(tools...)

	a.mu.Lock()
	a.configs = desired
	a.apiKeyPrefixes = apiKeyPrefixes
	a.pathAliases = pathAliases
	a.toolPrefixes = toolPrefixes
	a.sseServers = make(map[string]http.Handler)
	a.mu.Unlock()

	return nil
}

func (a *AegisServer) buildServerTools(cfgs []*config.APIConfig) []mcpserver.ServerTool {
	tools := make([]mcpserver.ServerTool, 0, 1)
	tools = append(tools, mcpserver.ServerTool{
		Tool: mcp.NewTool(
			"aegis_status",
			mcp.WithDescription("[Aegis] Returns the current status of the Aegis-API-MCP gateway: number of registered API configs, visible prefixes, and server uptime. No upstream API call is made."),
		),
		Handler: a.statusHandler(),
	})

	for _, cfg := range cfgs {
		for _, endpoint := range cfg.ExposedEndpoints {
			tools = append(tools, mcpserver.ServerTool{
				Tool:    buildEndpointTool(cfg, endpoint),
				Handler: a.makeToolHandler(cfg.MCPPrefix, endpoint.MCPToolName),
			})
		}
	}

	return tools
}

func buildEndpointTool(cfg *config.APIConfig, endpoint config.EndpointConfig) mcp.Tool {
	options := []mcp.ToolOption{
		mcp.WithDescription(endpointDescription(cfg, endpoint)),
	}

	for _, pathParam := range endpoint.PathParams {
		options = append(options, mcp.WithString(
			pathParam,
			mcp.Required(),
			mcp.Description(fmt.Sprintf("Value for the path parameter {%s}.", pathParam)),
		))
	}

	options = append(options,
		mcp.WithObject(
			"query",
			mcp.Description("Optional query string parameters. Values may be strings, numbers, booleans, or arrays of those types."),
			mcp.AdditionalProperties(true),
		),
		mcp.WithObject(
			"headers",
			mcp.Description("Optional extra upstream headers for this request. Protected headers such as Authorization cannot be overridden."),
			mcp.AdditionalProperties(true),
		),
		mcp.WithObject(
			"body",
			mcp.Description("Optional JSON request body as an object or nested structure."),
			mcp.AdditionalProperties(true),
		),
		mcp.WithString(
			"body_raw_json",
			mcp.Description("Optional raw JSON string for request bodies that are not natural JSON objects. Do not provide this together with body."),
		),
	)

	return mcp.NewTool(endpoint.MCPToolName, options...)
}

func endpointDescription(cfg *config.APIConfig, endpoint config.EndpointConfig) string {
	description := endpoint.Description
	if description == "" {
		description = fmt.Sprintf("Calls %s %s on %s.", endpoint.Method, endpoint.Path, cfg.BaseURL)
	}

	return fmt.Sprintf(
		"[Aegis] %s Upstream API: %s %s. MCP prefix: %q.",
		description,
		endpoint.Method,
		endpoint.Path,
		cfg.MCPPrefix,
	)
}

func (a *AegisServer) statusHandler() mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a.mu.RLock()
		prefixes := make([]string, 0, len(a.configs))
		for prefix := range a.configs {
			prefixes = append(prefixes, prefix)
		}
		a.mu.RUnlock()

		visiblePrefixes := visiblePrefixesForContext(ctx, prefixes)
		visibleLabel := strings.Join(visiblePrefixes, ", ")
		if visibleLabel == "" {
			visibleLabel = "none"
		}

		msg := fmt.Sprintf(
			"Aegis-API-MCP v1.0.0\n"+
				"Uptime       : %s\n"+
				"API configs  : %d (%s)\n",
			time.Since(a.startedAt).Round(time.Second),
			len(visiblePrefixes),
			visibleLabel,
		)
		return mcp.NewToolResultText(msg), nil
	}
}

func (a *AegisServer) makeToolHandler(prefix, toolName string) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()

		if !prefixAllowedForContext(ctx, prefix) {
			a.logger.Log(audit.Entry{
				MCPPrefix:  prefix,
				ToolName:   toolName,
				Result:     "FAIL",
				Reason:     "bearer token is not authorized for this API prefix",
				DurationMs: time.Since(start).Milliseconds(),
			})
			return mcp.NewToolResultError(
				fmt.Sprintf("AEGIS BLOCKED - bearer token is not authorized for API prefix %q", prefix),
			), nil
		}

		a.mu.RLock()
		cfg, exists := a.configs[prefix]
		a.mu.RUnlock()
		if !exists {
			return mcp.NewToolResultError(
				fmt.Sprintf("API prefix %q has been removed from the Aegis registry", prefix),
			), nil
		}

		endpoint := endpointByToolName(cfg, toolName)
		if endpoint == nil {
			return mcp.NewToolResultError(
				fmt.Sprintf("tool %q is no longer exposed for API prefix %q", toolName, prefix),
			), nil
		}

		result, err := api.Execute(ctx, api.Request{
			Config:    cfg,
			Endpoint:  endpoint,
			Arguments: req.Params.Arguments,
		})
		if err != nil {
			a.logger.Log(audit.Entry{
				MCPPrefix:  prefix,
				ToolName:   toolName,
				Method:     endpoint.Method,
				Result:     "FAIL",
				Reason:     err.Error(),
				DurationMs: time.Since(start).Milliseconds(),
			})
			return mcp.NewToolResultError("API request error: " + err.Error()), nil
		}

		formatted := formatResponse(result)
		entry := audit.Entry{
			MCPPrefix:       prefix,
			ToolName:        toolName,
			Method:          endpoint.Method,
			TargetURL:       result.URL,
			StatusCode:      result.StatusCode,
			DurationMs:      time.Since(start).Milliseconds(),
			ResponsePreview: preview(formatted, 200),
		}

		if result.StatusCode >= 400 {
			entry.Result = "UPSTREAM_ERROR"
			entry.Reason = result.Status
			a.logger.Log(entry)
			return mcp.NewToolResultError(formatted), nil
		}

		entry.Result = "PASS"
		a.logger.Log(entry)
		return mcp.NewToolResultText(formatted), nil
	}
}

func endpointByToolName(cfg *config.APIConfig, toolName string) *config.EndpointConfig {
	for i := range cfg.ExposedEndpoints {
		if cfg.ExposedEndpoints[i].MCPToolName == toolName {
			return &cfg.ExposedEndpoints[i]
		}
	}
	return nil
}

func formatResponse(result *api.Response) string {
	var body string
	if len(result.Body) == 0 {
		body = "(no body)"
	} else if json.Valid(result.Body) {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, result.Body, "", "  "); err == nil {
			body = pretty.String()
		} else {
			body = string(result.Body)
		}
	} else {
		body = string(result.Body)
	}

	return fmt.Sprintf("HTTP %s\nURL: %s\n\n%s", result.Status, result.URL, body)
}

func preview(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max] + " ...[truncated]"
}

func cloneStringMap(source map[string]string) map[string]string {
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func (a *AegisServer) aliasForAPIKey(key string) (string, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	prefix, ok := a.apiKeyPrefixes[key]
	return prefix, ok
}

func (a *AegisServer) runWatcher() {
	for {
		select {
		case <-a.stopCh:
			return
		case event, ok := <-a.watcher.Events:
			if !ok {
				return
			}
			a.handleFSEvent(event)
		case err, ok := <-a.watcher.Errors:
			if !ok {
				return
			}
			a.logger.System("fsnotify", "ERROR", err.Error())
		}
	}
}

func (a *AegisServer) handleFSEvent(event fsnotify.Event) {
	if filepath.Ext(event.Name) != ".json" {
		return
	}

	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		time.Sleep(50 * time.Millisecond)
	}

	if filepath.Clean(filepath.Dir(event.Name)) != filepath.Clean(a.configsDir) {
		return
	}

	if err := a.syncConfigs(); err != nil {
		a.logger.System("sync_configs", "ERROR", err.Error())
	}
}

func sanitizeAlias(alias string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(alias) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('_')
		}
	}
	return sb.String()
}
