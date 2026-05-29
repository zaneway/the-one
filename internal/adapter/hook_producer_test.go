package adapter

import "testing"

func TestHookProducer(t *testing.T) {
	if got := HookProducer("cursor", "beforeSubmitPrompt"); got != "cursor_hook:beforeSubmitPrompt" {
		t.Fatalf("cursor = %q", got)
	}
	if got := HookProducer("claude_code", "UserPromptSubmit"); got != "claude_code_hook:UserPromptSubmit" {
		t.Fatalf("claude = %q", got)
	}
}
