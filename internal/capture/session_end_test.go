package capture

import "testing"

func TestSessionEndTaskStatusDefaultsToCompleted(t *testing.T) {
	status := sessionEndTaskStatus(ObserveRequest{
		Task: &TaskInput{Status: StatusActive},
	})
	if status != StatusCompleted {
		t.Fatalf("sessionEndTaskStatus() = %q, want %q", status, StatusCompleted)
	}
}
