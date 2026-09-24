package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestRedactDiagnosticHeadersRemovesCredentials(t *testing.T) {
	headers := http.Header{
		"Authorization":       {"Bearer secret-token"},
		"Cookie":              {"session=secret"},
		"X-Oai-Attestation":   {"signed-device-token"},
		"X-Client-Request-Id": {"request-123"},
	}
	redacted := redactDiagnosticHeaders(headers)
	if got := redacted.Get("Authorization"); got != "[REDACTED]" {
		t.Fatalf("Authorization = %q, want redacted", got)
	}
	if got := redacted.Get("Cookie"); got != "[REDACTED]" {
		t.Fatalf("Cookie = %q, want redacted", got)
	}
	if got := redacted.Get("X-Oai-Attestation"); got != "[REDACTED]" {
		t.Fatalf("X-Oai-Attestation = %q, want redacted", got)
	}
	if got := redacted.Get("X-Client-Request-Id"); got != "request-123" {
		t.Fatalf("X-Client-Request-Id = %q, want preserved", got)
	}
}

func TestDiagnosticProxyURLRemovesUserInfo(t *testing.T) {
	got := diagnosticProxyURL("socks5://username:password@proxy.example:1080")
	if got != "socks5://proxy.example:1080" {
		t.Fatalf("diagnosticProxyURL = %q", got)
	}
}

func TestDiagnosticBodyRedactsNestedJSONAndCapsCapture(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6","authorization":"secret","nested":{"api_key":"key","safe":"value"}}`)
	text, hash, truncated := diagnosticBody(body)
	if truncated {
		t.Fatal("small JSON body must not be truncated")
	}
	if strings.Contains(text, "secret") || strings.Contains(text, `"key"`) {
		t.Fatalf("diagnostic body leaked a secret: %s", text)
	}
	if !strings.Contains(text, "[REDACTED]") || !strings.Contains(text, "value") {
		t.Fatalf("diagnostic body did not preserve safe fields: %s", text)
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

func TestDiagnosticBodyRedactsNDJSONAndSSEFrames(t *testing.T) {
	body := []byte("{\"type\":\"response.created\",\"access_token\":\"first-secret\"}\n" +
		"event: message\n" +
		"data: {\"type\":\"response.completed\",\"nested\":{\"api_key\":\"second-secret\"}}\n\n" +
		"data: [DONE]")
	text, _, _ := diagnosticBody(body)
	if strings.Contains(text, "first-secret") || strings.Contains(text, "second-secret") {
		t.Fatalf("framed diagnostic body leaked a secret: %s", text)
	}
	if strings.Count(text, "[REDACTED]") != 2 || !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("framed diagnostic body = %s", text)
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
