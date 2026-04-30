package harnas

import (
	"errors"
	"testing"
	"time"
)

func TestRetryPolicyRetriesTransientHTTP(t *testing.T) {
	policy := RetryPolicy{
		MaxAttempts:   3,
		RetryableHTTP: map[int]bool{503: true},
		Backoff:       func(int) time.Duration { return 7 * time.Millisecond },
	}

	decision := policy.Decide(HTTPError{Status: 503, Body: "unavailable"}, 1)
	if !decision.Retry || decision.Delay != 7*time.Millisecond {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestRetryPolicyAbortsPermanentHTTP(t *testing.T) {
	decision := DefaultRetryPolicy().Decide(HTTPError{Status: 400, Body: "bad"}, 1)
	if decision.Retry {
		t.Fatalf("expected abort")
	}
}

func TestRetryPolicyAbortsAtMaxAttempts(t *testing.T) {
	decision := DefaultRetryPolicy().Decide(HTTPError{Status: 503, Body: "bad"}, 3)
	if decision.Retry {
		t.Fatalf("expected abort")
	}
}

func TestRetryPolicyRetriesNetworkStyleErrors(t *testing.T) {
	decision := DefaultRetryPolicy().Decide(errors.New("connection reset by peer"), 1)
	if !decision.Retry {
		t.Fatalf("expected retry")
	}
}
