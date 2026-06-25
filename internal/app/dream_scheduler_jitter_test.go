package app

import (
	"testing"
	"time"
)

func TestDreamExportTickIntervalAppliesJitter(t *testing.T) {
	base := time.Minute
	got := dreamExportTickInterval(base, 0.5)
	if got <= 0 {
		t.Fatalf("dreamExportTickInterval() = %v, want positive duration", got)
	}
	if got == base {
		t.Fatal("dreamExportTickInterval() returned exact base interval with jitter enabled")
	}
}

func TestDreamExportTickIntervalWithoutJitter(t *testing.T) {
	base := time.Minute
	if got := dreamExportTickInterval(base, 0); got != base {
		t.Fatalf("dreamExportTickInterval() = %v, want %v", got, base)
	}
}
