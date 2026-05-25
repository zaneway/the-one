package mvp

import "testing"

func TestSyntheticScenarioFixturesCoverRegistry(t *testing.T) {
	fixtures := SyntheticScenarioFixtures()
	if len(fixtures) != 10 {
		t.Fatalf("fixture count = %d, want 10", len(fixtures))
	}
	seen := map[string]bool{}
	for i, fixture := range fixtures {
		if _, ok := FindScenario(fixture.ScenarioID); !ok {
			t.Fatalf("fixture %d scenario_id = %s, not in registry", i, fixture.ScenarioID)
		}
		if seen[fixture.ScenarioID] {
			t.Fatalf("duplicate fixture scenario_id = %s", fixture.ScenarioID)
		}
		seen[fixture.ScenarioID] = true
		if fixture.AgentType == "" || fixture.Round <= 0 || fixture.LatencyMS <= 0 {
			t.Fatalf("fixture %d = %+v, want agent/round/latency", i, fixture)
		}
		if _, err := fixture.ExpectedJSON(); err != nil {
			t.Fatalf("fixture %s ExpectedJSON() error = %v", fixture.ScenarioID, err)
		}
		if _, err := fixture.ObservedJSON(); err != nil {
			t.Fatalf("fixture %s ObservedJSON() error = %v", fixture.ScenarioID, err)
		}
	}
}
