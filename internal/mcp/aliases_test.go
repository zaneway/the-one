package mcp

import "testing"

func TestMCPToolName(t *testing.T) {
	tests := []struct {
		canonical string
		want      string
	}{
		{"memory.health", "memory_health"},
		{"memory.mvp.metrics.compute", "memory_mvp_metrics_compute"},
		{"memory.retrieval.access_logs", "memory_retrieval_access_logs"},
	}
	for _, tt := range tests {
		if got := MCPToolName(tt.canonical); got != tt.want {
			t.Fatalf("MCPToolName(%q) = %q, want %q", tt.canonical, got, tt.want)
		}
	}
}
