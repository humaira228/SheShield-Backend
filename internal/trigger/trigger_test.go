package trigger

import "testing"

func TestNormalizeAndRisk(t *testing.T) {
	if Normalize(" MOTION_FALL ") != MotionFall || Normalize("nope") != Manual || Normalize("") != Manual {
		t.Fatal("normalize")
	}
	if Risk(MotionSprint, false) != RiskMedium || Risk(MotionSprint, true) != RiskHigh {
		t.Fatal("sprint is medium unless duress")
	}
	for _, x := range []string{Manual, Voice, MotionFall, MotionStruggle, MotionInactive, MissedCheckin} {
		if Risk(x, false) != RiskHigh {
			t.Errorf("%s should be high", x)
		}
		if Label(x) == "" {
			t.Errorf("%s has no label", x)
		}
	}
}
