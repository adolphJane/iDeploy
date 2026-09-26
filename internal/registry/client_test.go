package registry

import (
	"context"
	"net/http"
	"testing"
)

func TestTesterProbeOCIAuthRequired(t *testing.T) {
	tester := &Tester{client: &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v2/" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: http.NoBody}, nil
	})}}

	result := tester.Probe(context.Background(), Connection{Kind: KindOCI, Endpoint: "https://registry.example.com"})
	if result.Status != StatusAuthRequired {
		t.Fatalf("got %q, want %q", result.Status, StatusAuthRequired)
	}
}

func TestTesterProbeHarbor(t *testing.T) {
	tester := &Tester{client: &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/api/v2.0/health" {
			t.Fatalf("unexpected path %q", request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})}}

	result := tester.Probe(context.Background(), Connection{Kind: KindHarbor, Endpoint: "https://harbor.example.com"})
	if result.Status != StatusHealthy {
		t.Fatalf("got %q, want %q", result.Status, StatusHealthy)
	}
}

func TestTesterProbeUsesBasicCredentials(t *testing.T) {
	tester := &Tester{client: &http.Client{Transport: roundTripper(func(request *http.Request) (*http.Response, error) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "robot" || password != "token" {
			t.Fatalf("missing registry basic authentication: %q %q %v", username, password, ok)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})}}
	result := tester.Probe(context.Background(), Connection{Kind: KindOCI, Endpoint: "https://registry.example.com", Username: "robot", Password: "token"})
	if result.Status != StatusHealthy {
		t.Fatalf("got %q, want %q", result.Status, StatusHealthy)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (fn roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
