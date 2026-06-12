package main

import (
	"io"
	"strings"
	"testing"
)

func TestDecodeJSONParams(t *testing.T) {
	t.Run("valid object", func(t *testing.T) {
		params, err := decodeJSONParams(strings.NewReader(`{"event_type":"session.start","agent_type":"cursor"}`))
		if err != nil {
			t.Fatalf("decodeJSONParams() error = %v", err)
		}
		if params["event_type"] != "session.start" {
			t.Fatalf("event_type = %v, want session.start", params["event_type"])
		}
	})

	t.Run("empty stdin", func(t *testing.T) {
		_, err := decodeJSONParams(strings.NewReader(""))
		if err == nil || !strings.Contains(err.Error(), "stdin is empty") {
			t.Fatalf("decodeJSONParams() error = %v, want empty stdin error", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := decodeJSONParams(strings.NewReader(`{"event_type":`))
		if err == nil {
			t.Fatalf("decodeJSONParams() error = nil, want json error")
		}
	})

	t.Run("empty object", func(t *testing.T) {
		_, err := decodeJSONParams(strings.NewReader(`{}`))
		if err == nil || !strings.Contains(err.Error(), "params object is empty") {
			t.Fatalf("decodeJSONParams() error = %v, want empty object error", err)
		}
	})

	t.Run("only whitespace", func(t *testing.T) {
		_, err := decodeJSONParams(strings.NewReader("   \n\t"))
		if err == nil {
			t.Fatalf("decodeJSONParams() error = nil, want error")
		}
		if !strings.Contains(err.Error(), "stdin is empty") && err != io.EOF {
			t.Fatalf("decodeJSONParams() error = %v, want empty stdin or EOF", err)
		}
	})
}
