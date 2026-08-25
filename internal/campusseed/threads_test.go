package campusseed

import "testing"

func TestCampusThreadAndGoalIDsAreStable(t *testing.T) {
	tagIDs := []string{
		"campus-it-infrastructure",
		"campus-instructional-labs",
		"campus-visual-arts",
		"campus-workforce-devices",
	}
	for _, id := range tagIDs {
		if stableID("threads-tag", id) == "" {
			t.Fatalf("expected stable id for tag %q", id)
		}
	}
	goalIDs := []string{
		"campus-refresh-lab-fleet",
		"campus-reduce-license-waste",
		"campus-support-student-success",
	}
	for _, id := range goalIDs {
		if stableID("threads-goal", id) == "" {
			t.Fatalf("expected stable id for goal %q", id)
		}
	}
}

func TestCampusDemoBridgeAndSignalsMarkersAreStable(t *testing.T) {
	if campusDemoBridgeClientID != stableID("bridge-client", "campus-demo-mcp") {
		t.Fatalf("unexpected bridge client id: %q", campusDemoBridgeClientID)
	}
	if campusDemoSignalsRulePrefix != "campus-demo-" {
		t.Fatalf("unexpected signals prefix: %q", campusDemoSignalsRulePrefix)
	}
}
