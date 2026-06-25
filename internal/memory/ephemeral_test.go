package memory

import "testing"

func TestIsEphemeralMemoryID(t *testing.T) {
	if !IsEphemeralMemoryID("rawevt:evt_123") {
		t.Fatal("expected rawevt prefix to be ephemeral")
	}
	if IsEphemeralMemoryID("mem_123") {
		t.Fatal("did not expect normal memory id to be ephemeral")
	}
}

func TestFilterPersistentMemoryIDs(t *testing.T) {
	got := FilterPersistentMemoryIDs([]string{"mem_a", "rawevt:evt_b", "", "mem_c"})
	if len(got) != 2 || got[0] != "mem_a" || got[1] != "mem_c" {
		t.Fatalf("filtered ids = %+v, want mem_a and mem_c only", got)
	}
}
