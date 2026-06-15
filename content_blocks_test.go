package harnas

import (
	"net/http"
	"testing"
	"time"
)

func TestFetchAttachmentURLUsesRequestTimeout(t *testing.T) {
	previous := DefaultAttachmentHTTPTimeout
	DefaultAttachmentHTTPTimeout = 20 * time.Millisecond
	defer func() { DefaultAttachmentHTTPTimeout = previous }()

	_, _, err := fetchAttachmentURLWithClient(blockingHTTPDoer{}, "https://example.com/attachment.png")
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func (blockingHTTPDoer) RoundTrip(*http.Request) (*http.Response, error) {
	panic("blockingHTTPDoer must be used through Do")
}
