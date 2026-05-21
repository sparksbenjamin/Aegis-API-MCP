package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"aegis-api-mcp/internal/config"
)

func TestExecuteBuildsPathQueryHeadersAndAuth(t *testing.T) {
	t.Parallel()

	type echoResponse struct {
		Method string              `json:"method"`
		Path   string              `json:"path"`
		Query  map[string][]string `json:"query"`
		Header map[string][]string `json:"header"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := echoResponse{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  map[string][]string(r.URL.Query()),
			Header: map[string][]string(r.Header),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	cfg := &config.APIConfig{
		MCPPrefix: "github",
		BaseURL:   server.URL,
		Auth: config.AuthConfig{
			Type:  config.AuthTypeBearer,
			Token: "ghp_test",
		},
		TimeoutSeconds: 30,
	}
	endpoint := &config.EndpointConfig{
		Path:        "/repos/{owner}/{repo}/issues",
		Method:      http.MethodGet,
		MCPToolName: "github_list_issues",
		PathParams:  []string{"owner", "repo"},
	}

	resp, err := Execute(context.Background(), Request{
		Config:   cfg,
		Endpoint: endpoint,
		Arguments: map[string]interface{}{
			"owner": "openai",
			"repo":  "gpt",
			"query": map[string]interface{}{
				"state":  "open",
				"labels": []interface{}{"bug", "triage"},
			},
			"headers": map[string]interface{}{
				"X-Test": "demo",
			},
		},
	})
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}

	var payload echoResponse
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Path != "/repos/openai/gpt/issues" {
		t.Fatalf("unexpected path: %q", payload.Path)
	}
	if payload.Method != http.MethodGet {
		t.Fatalf("unexpected method: %q", payload.Method)
	}
	if got := payload.Query["state"]; len(got) != 1 || got[0] != "open" {
		t.Fatalf("unexpected state query: %v", got)
	}
	if got := payload.Query["labels"]; len(got) != 2 || got[0] != "bug" || got[1] != "triage" {
		t.Fatalf("unexpected labels query: %v", got)
	}
	if got := payload.Header["Authorization"]; len(got) != 1 || got[0] != "Bearer ghp_test" {
		t.Fatalf("unexpected auth header: %v", got)
	}
	if got := payload.Header["X-Test"]; len(got) != 1 || got[0] != "demo" {
		t.Fatalf("unexpected extra header: %v", got)
	}
}

func TestExecuteBuildsJSONBody(t *testing.T) {
	t.Parallel()

	type echoResponse struct {
		Method string                 `json:"method"`
		Body   map[string]interface{} `json:"body"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		payload := echoResponse{
			Method: r.Method,
			Body:   body,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	defer server.Close()

	cfg := &config.APIConfig{
		MCPPrefix:      "github",
		BaseURL:        server.URL,
		TimeoutSeconds: 30,
	}
	endpoint := &config.EndpointConfig{
		Path:        "/repos/{owner}/{repo}/issues",
		Method:      http.MethodPost,
		MCPToolName: "github_create_issue",
		PathParams:  []string{"owner", "repo"},
	}

	resp, err := Execute(context.Background(), Request{
		Config:   cfg,
		Endpoint: endpoint,
		Arguments: map[string]interface{}{
			"owner": "openai",
			"repo":  "gpt",
			"body": map[string]interface{}{
				"title": "Bug report",
				"labels": []interface{}{
					"bug",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("execute request: %v", err)
	}

	var payload echoResponse
	if err := json.Unmarshal(resp.Body, &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if payload.Method != http.MethodPost {
		t.Fatalf("unexpected method: %q", payload.Method)
	}
	if payload.Body["title"] != "Bug report" {
		t.Fatalf("unexpected title: %v", payload.Body["title"])
	}
}

func TestExecuteRejectsProtectedHeaderOverride(t *testing.T) {
	t.Parallel()

	cfg := &config.APIConfig{
		MCPPrefix:      "github",
		BaseURL:        "https://api.github.com",
		TimeoutSeconds: 30,
	}
	endpoint := &config.EndpointConfig{
		Path:        "/repos/{owner}/{repo}/issues",
		Method:      http.MethodGet,
		MCPToolName: "github_list_issues",
		PathParams:  []string{"owner", "repo"},
	}

	_, err := Execute(context.Background(), Request{
		Config:   cfg,
		Endpoint: endpoint,
		Arguments: map[string]interface{}{
			"owner": "openai",
			"repo":  "gpt",
			"headers": map[string]interface{}{
				"Authorization": "Bearer nope",
			},
		},
	})
	if err == nil {
		t.Fatal("expected protected header error")
	}
}
