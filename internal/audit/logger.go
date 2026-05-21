package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type Entry struct {
	Timestamp       string `json:"timestamp"`
	MCPPrefix       string `json:"mcp_prefix"`
	ToolName        string `json:"tool_name"`
	Method          string `json:"method,omitempty"`
	TargetURL       string `json:"target_url,omitempty"`
	Result          string `json:"result"`
	Reason          string `json:"reason,omitempty"`
	StatusCode      int    `json:"status_code,omitempty"`
	DurationMs      int64  `json:"duration_ms"`
	ResponsePreview string `json:"response_preview,omitempty"`
}

type Logger struct {
	mu  sync.Mutex
	out *os.File
}

func New() *Logger {
	return &Logger{out: os.Stderr}
}

func NewWithFile(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open log file %s: %w", path, err)
	}
	return &Logger{out: f}, nil
}

func (l *Logger) Log(e Entry) {
	e.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)

	data, err := json.Marshal(e)
	if err != nil {
		l.mu.Lock()
		fmt.Fprintf(l.out, `{"timestamp":%q,"error":"marshal failed: %v"}`+"\n", e.Timestamp, err)
		l.mu.Unlock()
		return
	}

	l.mu.Lock()
	fmt.Fprintln(l.out, string(data))
	l.mu.Unlock()
}

func (l *Logger) System(action, result, reason string) {
	l.Log(Entry{
		MCPPrefix:  "aegis-system",
		ToolName:   action,
		Result:     result,
		Reason:     reason,
		DurationMs: 0,
	})
}
