package controlplane

import "testing"

func TestAIJobStatusTransitionContract(t *testing.T) {
	tests := []struct {
		from   string
		to     string
		reason string
		want   bool
	}{
		{AIJobStatusAccepted, AIJobStatusQueued, "", true},
		{AIJobStatusQueued, AIJobStatusDispatching, "", true},
		{AIJobStatusDispatching, AIJobStatusQueued, "capacity_unavailable", true},
		{AIJobStatusDispatching, AIJobStatusRunning, "", true},
		{AIJobStatusRunning, AIJobStatusUnknown, "provider_timeout", true},
		{AIJobStatusUnknown, AIJobStatusQueued, "", false},
		{AIJobStatusUnknown, AIJobStatusQueued, "proven_not_created", true},
		{AIJobStatusCanceling, AIJobStatusSucceeded, "", true},
		{AIJobStatusSucceeded, AIJobStatusExpired, "", true},
		{AIJobStatusSucceeded, AIJobStatusRunning, "", false},
		{AIJobStatusExpired, AIJobStatusQueued, "", false},
	}
	for _, test := range tests {
		if got := aiJobStatusTransitionAllowed(test.from, test.to, test.reason); got != test.want {
			t.Errorf("transition %s -> %s reason=%q = %t, want %t", test.from, test.to, test.reason, got, test.want)
		}
	}
}
