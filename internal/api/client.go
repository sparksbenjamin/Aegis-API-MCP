package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"aegis-api-mcp/internal/config"
)

var blockedUserHeaders = map[string]struct{}{
	"authorization":     {},
	"content-length":    {},
	"host":              {},
	"transfer-encoding": {},
}

type Request struct {
	Config    *config.APIConfig
	Endpoint  *config.EndpointConfig
	Arguments map[string]interface{}
}

type Response struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
	URL        string
}

func Execute(ctx context.Context, req Request) (*Response, error) {
	if req.Config == nil {
		return nil, fmt.Errorf("config is required")
	}
	if req.Endpoint == nil {
		return nil, fmt.Errorf("endpoint is required")
	}

	requestURL, err := buildRequestURL(req.Config, req.Endpoint, req.Arguments)
	if err != nil {
		return nil, err
	}

	bodyReader, contentType, err := buildRequestBody(req.Arguments)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.Endpoint.Method, requestURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}

	for key, value := range req.Config.DefaultHeaders {
		httpReq.Header.Set(key, value)
	}

	extraHeaders, err := mapArgument(req.Arguments, "headers")
	if err != nil {
		return nil, err
	}
	for key, value := range extraHeaders {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		if _, blocked := blockedUserHeaders[lowerKey]; blocked {
			return nil, fmt.Errorf("headers.%s cannot override protected header %q", key, key)
		}
		httpReq.Header.Set(strings.TrimSpace(key), stringifyScalar(value))
	}

	if contentType != "" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", contentType)
	}

	if err := applyAuth(httpReq, req.Config.Auth); err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: time.Duration(req.Config.TimeoutSeconds) * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: req.Config.AllowInsecureTLS,
			},
		},
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read upstream response body: %w", err)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header.Clone(),
		Body:       body,
		URL:        resp.Request.URL.String(),
	}, nil
}

func buildRequestURL(cfg *config.APIConfig, endpoint *config.EndpointConfig, arguments map[string]interface{}) (string, error) {
	path := endpoint.Path
	for _, pathParam := range endpoint.PathParams {
		rawValue, ok := arguments[pathParam]
		if !ok {
			return "", fmt.Errorf("missing required path parameter %q", pathParam)
		}
		path = strings.ReplaceAll(path, "{"+pathParam+"}", url.PathEscape(stringifyScalar(rawValue)))
	}

	fullURL := cfg.BaseURL + path
	parsedURL, err := url.Parse(fullURL)
	if err != nil {
		return "", fmt.Errorf("build upstream URL: %w", err)
	}

	queryArgument, err := mapArgument(arguments, "query")
	if err != nil {
		return "", err
	}
	query := parsedURL.Query()
	for key, rawValue := range queryArgument {
		if rawValue == nil {
			continue
		}
		switch typed := rawValue.(type) {
		case []interface{}:
			for _, item := range typed {
				query.Add(key, stringifyScalar(item))
			}
		default:
			query.Set(key, stringifyScalar(rawValue))
		}
	}
	parsedURL.RawQuery = query.Encode()

	return parsedURL.String(), nil
}

func buildRequestBody(arguments map[string]interface{}) (io.Reader, string, error) {
	bodyValue, hasBody := arguments["body"]
	rawJSONValue, hasRawJSON := arguments["body_raw_json"]
	if hasBody && hasRawJSON {
		return nil, "", fmt.Errorf("provide either body or body_raw_json, not both")
	}

	if hasBody {
		data, err := json.Marshal(bodyValue)
		if err != nil {
			return nil, "", fmt.Errorf("marshal body: %w", err)
		}
		return bytes.NewReader(data), "application/json", nil
	}

	if hasRawJSON {
		rawJSON, ok := rawJSONValue.(string)
		if !ok {
			return nil, "", fmt.Errorf("body_raw_json must be a string")
		}
		rawJSON = strings.TrimSpace(rawJSON)
		if rawJSON == "" {
			return nil, "", nil
		}
		if !json.Valid([]byte(rawJSON)) {
			return nil, "", fmt.Errorf("body_raw_json must contain valid JSON")
		}
		return strings.NewReader(rawJSON), "application/json", nil
	}

	return nil, "", nil
}

func applyAuth(req *http.Request, auth config.AuthConfig) error {
	switch auth.Type {
	case "":
		return nil
	case config.AuthTypeBearer:
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case config.AuthTypeBasic:
		req.SetBasicAuth(auth.Username, auth.Password)
	case config.AuthTypeHeader:
		req.Header.Set(auth.HeaderName, auth.Value)
	case config.AuthTypeQuery:
		query := req.URL.Query()
		query.Set(auth.QueryName, auth.Value)
		req.URL.RawQuery = query.Encode()
	default:
		return fmt.Errorf("unsupported auth type %q", auth.Type)
	}
	return nil
}

func mapArgument(arguments map[string]interface{}, key string) (map[string]interface{}, error) {
	rawValue, ok := arguments[key]
	if !ok || rawValue == nil {
		return map[string]interface{}{}, nil
	}

	typed, ok := rawValue.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}

	return typed, nil
}

func stringifyScalar(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}
