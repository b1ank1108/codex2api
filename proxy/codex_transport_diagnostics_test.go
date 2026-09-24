package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/codex2api/auth"
	"github.com/klauspost/compress/zstd"
)

func TestCloneDiagnosticHeadersPreservesCredentials(t *testing.T) {
	headers := http.Header{
		"Authorization":       {"Bearer secret-token"},
		"Cookie":              {"session=secret"},
		"X-Oai-Attestation":   {"signed-device-token"},
		"X-Client-Request-Id": {"123e4567-e89b-12d3-a456-426614174000"},
	}
	captured := cloneDiagnosticHeaders(headers)
	for name, want := range headers {
		if got := captured.Values(name); !bytes.Equal([]byte(got[0]), []byte(want[0])) {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	headers.Set("Authorization", "changed")
	if got := captured.Get("Authorization"); got != "Bearer secret-token" {
		t.Fatalf("captured Authorization changed with source: %q", got)
	}
}

func TestDiagnosticBodyPreservesSensitiveValuesAndCapsCapture(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6","authorization":"secret","nested":{"api_key":"key","request_id":"123e4567-e89b-12d3-a456-426614174000"}}`)
	text, hash, truncated := diagnosticBody(body)
	if truncated {
		t.Fatal("small JSON body must not be truncated")
	}
	if text != string(body) {
		t.Fatalf("diagnostic body = %q, want exact input %q", text, body)
	}
	wantSum := sha256.Sum256(body)
	if hash != hex.EncodeToString(wantSum[:]) {
		t.Fatalf("hash = %q, want hash of original body", hash)
	}

	large := bytes.Repeat([]byte("a"), codexDiagnosticBodyLimit+17)
	text, _, truncated = diagnosticBody(large)
	if !truncated || len(text) != codexDiagnosticBodyLimit {
		t.Fatalf("large body capture len/truncated = %d/%t", len(text), truncated)
	}
}

func TestDiagnosticBodyPreservesNDJSONAndSSEFrames(t *testing.T) {
	body := []byte("{\"type\":\"response.created\",\"access_token\":\"first-secret\"}\n" +
		"event: message\n" +
		"data: {\"type\":\"response.completed\",\"nested\":{\"api_key\":\"second-secret\"}}\n\n" +
		"data: [DONE]")
	text, _, _ := diagnosticBody(body)
	if text != string(body) {
		t.Fatalf("framed diagnostic body = %q, want exact input %q", text, body)
	}
}

func TestCaptureCodexHTTPResponsePreservesAllRawDiagnosticFields(t *testing.T) {
	payloads := make(chan []byte, 1)
	SetCodexDiagnosticSink(func(payload []byte) { payloads <- append([]byte(nil), payload...) })
	SetCodexDiagnosticCaptureEnabled(true)
	t.Cleanup(func() {
		SetCodexDiagnosticCaptureEnabled(false)
		SetCodexDiagnosticSink(func([]byte) {})
	})

	ctx := context.WithValue(context.Background(), upstreamTraceContextKey{}, &upstreamTraceAudit{requestID: "gateway-request-id"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://url-user:url-password@example.test/responses?access_token=query-token", bytes.NewBufferString(`{"api_key":"body-secret","request_id":"123e4567-e89b-12d3-a456-426614174000"}`))
	if err != nil {
		t.Fatalf("http.NewRequestWithContext: %v", err)
	}
	req.Header.Set("Authorization", "Bearer header-token")
	req.Header.Set("Cookie", "session=cookie-secret")
	resp := &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{"Set-Cookie": {"upstream=raw-cookie"}}}
	captureCodexHTTPResponse(ctx, req, resp, &auth.Account{DBID: 42}, "socks5://proxy-user:proxy-password@proxy.example:1080", "request_error", []byte(`{"refresh_token":"response-secret"}`), false, errors.New("request 123e4567-e89b-12d3-a456-426614174000 failed"))

	select {
	case payload := <-payloads:
		var capture codexTransportDiagnostic
		if err := json.Unmarshal(payload, &capture); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if capture.RequestID != "gateway-request-id" || capture.AccountID != 42 || capture.Status != http.StatusUnauthorized {
			t.Fatalf("capture identity/status = %+v", capture)
		}
		if capture.URL != req.URL.String() || capture.Proxy != "socks5://proxy-user:proxy-password@proxy.example:1080" {
			t.Fatalf("raw URL/proxy = %q / %q", capture.URL, capture.Proxy)
		}
		if capture.RequestHeaders.Get("Authorization") != "Bearer header-token" || capture.RequestHeaders.Get("Cookie") != "session=cookie-secret" || capture.ResponseHeaders.Get("Set-Cookie") != "upstream=raw-cookie" {
			t.Fatalf("raw headers = request %#v response %#v", capture.RequestHeaders, capture.ResponseHeaders)
		}
		if capture.RequestBody != `{"api_key":"body-secret","request_id":"123e4567-e89b-12d3-a456-426614174000"}` || capture.ResponseBody != `{"refresh_token":"response-secret"}` {
			t.Fatalf("raw bodies = %q / %q", capture.RequestBody, capture.ResponseBody)
		}
		if capture.Error != "request 123e4567-e89b-12d3-a456-426614174000 failed" {
			t.Fatalf("raw error = %q", capture.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("diagnostic sink was not called")
	}
}

func TestDiagnosticRequestBodyDecodesZstd(t *testing.T) {
	original := []byte(`{"model":"gpt-5.6","input":"hello"}`)
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	compressed := encoder.EncodeAll(original, nil)
	encoder.Close()
	req, err := http.NewRequest(http.MethodPost, "https://example.test/responses", bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	req.Header.Set("Content-Encoding", "zstd")
	if got := diagnosticRequestBody(req); !bytes.Equal(got, original) {
		t.Fatalf("decoded request body = %q, want %q", got, original)
	}
}
