package harnas

import (
	"net"
	"strings"
	"time"
)

type RetryDecision struct {
	Retry bool
	Delay time.Duration
}

type RetryPolicy struct {
	MaxAttempts   int
	RetryableHTTP map[int]bool
	Backoff       func(attempt int) time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		RetryableHTTP: map[int]bool{
			408: true,
			429: true,
			500: true,
			502: true,
			503: true,
			504: true,
		},
		Backoff: func(attempt int) time.Duration {
			return time.Duration(250*(1<<(attempt-1))) * time.Millisecond
		},
	}
}

func (p RetryPolicy) Decide(err error, attempt int) RetryDecision {
	maxAttempts := p.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}
	if attempt >= maxAttempts || !p.retryable(err) {
		return RetryDecision{Retry: false}
	}
	backoff := p.Backoff
	if backoff == nil {
		backoff = DefaultRetryPolicy().Backoff
	}
	return RetryDecision{Retry: true, Delay: backoff(attempt)}
}

func (p RetryPolicy) retryable(err error) bool {
	status := providerStatus(err)
	if status > 0 {
		retryableHTTP := p.RetryableHTTP
		if retryableHTTP == nil {
			retryableHTTP = DefaultRetryPolicy().RetryableHTTP
		}
		return retryableHTTP[status]
	}
	if netErr, ok := err.(net.Error); ok && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	name := strings.ToLower(err.Error())
	return strings.Contains(name, "connection reset") ||
		strings.Contains(name, "connection refused") ||
		strings.Contains(name, "timeout")
}
