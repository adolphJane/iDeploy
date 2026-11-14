package sharedworkspace

import (
	"context"
	"io"
	"net/http"
	"testing"
)

type retryTransport struct {
	calls int
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	if t.calls == 1 {
		return nil, io.EOF
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(http.NoBody),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestGetRetriesTunnelEOF(t *testing.T) {
	transport := &retryTransport{}
	client := &Client{http: &http.Client{Transport: transport}}
	if err := client.request(context.Background(), http.MethodGet, "/v2/changes?after=62", nil, nil); err != nil {
		t.Fatal(err)
	}
	if transport.calls != 2 {
		t.Fatalf("GET attempts = %d, want 2", transport.calls)
	}
}

func TestPostDoesNotRetryTunnelEOF(t *testing.T) {
	transport := &retryTransport{}
	client := &Client{http: &http.Client{Transport: transport}}
	if err := client.request(context.Background(), http.MethodPost, "/v2/batches", nil, nil); err == nil {
		t.Fatal("POST should return the initial EOF")
	}
	if transport.calls != 1 {
		t.Fatalf("POST attempts = %d, want 1", transport.calls)
	}
}
