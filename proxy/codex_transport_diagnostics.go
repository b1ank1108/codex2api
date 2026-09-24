package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/codex2api/auth"
	"github.com/klauspost/compress/zstd"
)

const codexDiagnosticBodyLimit = 2 << 20

var codexDiagnosticCaptureEnabled atomic.Bool
var codexDiagnosticSink atomic.Value // func([]byte)

func SetCodexDiagnosticCaptureEnabled(enabled bool) {
	codexDiagnosticCaptureEnabled.Store(enabled)
}

func CodexDiagnosticCaptureEnabled() bool {
	return codexDiagnosticCaptureEnabled.Load()
}

func SetCodexDiagnosticSink(sink func([]byte)) {
	if sink != nil {
		codexDiagnosticSink.Store(sink)
	}
}

type codexTransportDiagnostic struct {
	CapturedAt        string      `json:"captured_at"`
	Transport         string      `json:"transport"`
	Stage             string      `json:"stage"`
	Status            int         `json:"status,omitempty"`
	AccountID         int64       `json:"account_id,omitempty"`
	Method            string      `json:"method,omitempty"`
	URL               string      `json:"url,omitempty"`
	Proxy             string      `json:"proxy,omitempty"`
	RequestID         string      `json:"request_id"`
	UpstreamRequestID string      `json:"upstream_request_id,omitempty"`
	RequestHeaders    http.Header `json:"request_headers,omitempty"`
	ResponseHeaders   http.Header `json:"response_headers,omitempty"`
	RequestBody       string      `json:"request_body,omitempty"`
	ResponseBody      string      `json:"response_body,omitempty"`
	RequestSHA256     string      `json:"request_sha256,omitempty"`
	ResponseSHA256    string      `json:"response_sha256,omitempty"`
	RequestTruncated  bool        `json:"request_truncated,omitempty"`
	ResponseTruncated bool        `json:"response_truncated,omitempty"`
	Error             string      `json:"error,omitempty"`
}

func emitCodexTransportDiagnostic(ctx context.Context, record codexTransportDiagnostic) {
	if !CodexDiagnosticCaptureEnabled() {
		return
	}
	trace := snapshotUpstreamTrace(ctx)
	if record.RequestID == "" {
		record.RequestID = trace.RequestID
	}
	if record.UpstreamRequestID == "" {
		record.UpstreamRequestID = trace.UpstreamRequestID
	}
	if record.RequestID == "" {
		return
	}
	if record.CapturedAt == "" {
		record.CapturedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return
	}
	if sink, ok := codexDiagnosticSink.Load().(func([]byte)); ok && sink != nil {
		sink(payload)
	}
}

func cloneDiagnosticHeaders(headers http.Header) http.Header {
	if len(headers) == 0 {
		return nil
	}
	return headers.Clone()
}

func isCodexTransportDiagnosticAccount(account *auth.Account) bool {
	return account != nil && !account.IsRelayStyle()
}

func diagnosticBody(body []byte) (text, hash string, truncated bool) {
	if len(body) == 0 {
		return "", "", false
	}
	sum := sha256.Sum256(body)
	hash = hex.EncodeToString(sum[:])
	if !utf8.Valid(body) {
		return "[binary body omitted]", hash, len(body) > codexDiagnosticBodyLimit
	}
	text = string(body)
	if len(text) > codexDiagnosticBodyLimit {
		captured := []byte(text[:codexDiagnosticBodyLimit])
		for len(captured) > 0 && !utf8.Valid(captured) {
			captured = captured[:len(captured)-1]
		}
		text = string(captured)
		truncated = true
	}
	return text, hash, truncated
}

func diagnosticRequestBody(req *http.Request) []byte {
	if req == nil || req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil || body == nil {
		return nil
	}
	defer body.Close()
	data, _ := io.ReadAll(io.LimitReader(body, codexDiagnosticBodyLimit*2+1))
	if strings.EqualFold(strings.TrimSpace(req.Header.Get("Content-Encoding")), "zstd") {
		decoder, decodeErr := zstd.NewReader(
			bytes.NewReader(data),
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxMemory(uint64(codexDiagnosticBodyLimit*8)),
		)
		if decodeErr == nil {
			defer decoder.Close()
			if decoded, decodeErr := io.ReadAll(io.LimitReader(decoder, codexDiagnosticBodyLimit*2+1)); decodeErr == nil {
				return decoded
			}
		}
	}
	return data
}

func captureCodexHTTPResponse(ctx context.Context, req *http.Request, resp *http.Response, account *auth.Account, proxyURL, stage string, responseBody []byte, responseBodyTruncated bool, requestErr error) {
	if !CodexDiagnosticCaptureEnabled() || req == nil {
		return
	}
	requestText, requestHash, requestTruncated := diagnosticBody(diagnosticRequestBody(req))
	responseText, responseHash, responseTruncated := diagnosticBody(responseBody)
	responseTruncated = responseTruncated || responseBodyTruncated
	record := codexTransportDiagnostic{
		Transport: "http", Stage: stage, Method: req.Method, URL: req.URL.String(), Proxy: proxyURL,
		RequestHeaders: cloneDiagnosticHeaders(req.Header), RequestBody: requestText, RequestSHA256: requestHash, RequestTruncated: requestTruncated,
		ResponseBody: responseText, ResponseSHA256: responseHash, ResponseTruncated: responseTruncated,
	}
	if account != nil {
		record.AccountID = account.ID()
	}
	if resp != nil {
		record.Status = resp.StatusCode
		record.ResponseHeaders = cloneDiagnosticHeaders(resp.Header)
	}
	if requestErr != nil {
		record.Error = requestErr.Error()
	}
	emitCodexTransportDiagnostic(ctx, record)
}

type codexDiagnosticHTTPBody struct {
	base      io.ReadCloser
	buffer    bytes.Buffer
	ctx       context.Context
	req       *http.Request
	resp      *http.Response
	account   *auth.Account
	proxyURL  string
	once      sync.Once
	truncated bool
}

func (body *codexDiagnosticHTTPBody) Read(buffer []byte) (int, error) {
	n, err := body.base.Read(buffer)
	if n > 0 {
		remaining := codexDiagnosticBodyLimit - body.buffer.Len()
		if remaining > 0 {
			writeCount := n
			if writeCount > remaining {
				writeCount = remaining
				body.truncated = true
			}
			_, _ = body.buffer.Write(buffer[:writeCount])
		} else {
			body.truncated = true
		}
	}
	if err == io.EOF {
		body.finish()
	}
	return n, err
}

func (body *codexDiagnosticHTTPBody) Close() error {
	body.finish()
	return body.base.Close()
}

func (body *codexDiagnosticHTTPBody) finish() {
	if body == nil {
		return
	}
	body.once.Do(func() {
		captureCodexHTTPResponse(body.ctx, body.req, body.resp, body.account, body.proxyURL, "response_body", body.buffer.Bytes(), body.truncated, nil)
	})
}

func CaptureCodexWebsocketDiagnostic(ctx context.Context, account *auth.Account, requestBody []byte, requestHeaders, responseHeaders http.Header, proxyURL, stage string, status int, responseBody []byte, responseBodyTruncated bool, captureErr error) {
	if !CodexDiagnosticCaptureEnabled() {
		return
	}
	requestText, requestHash, requestTruncated := diagnosticBody(requestBody)
	responseText, responseHash, responseTruncated := diagnosticBody(responseBody)
	responseTruncated = responseTruncated || responseBodyTruncated
	record := codexTransportDiagnostic{
		Transport: "websocket", Stage: stage, Status: status, Proxy: proxyURL,
		RequestHeaders: cloneDiagnosticHeaders(requestHeaders), ResponseHeaders: cloneDiagnosticHeaders(responseHeaders),
		RequestBody: requestText, ResponseBody: responseText, RequestSHA256: requestHash, ResponseSHA256: responseHash,
		RequestTruncated: requestTruncated, ResponseTruncated: responseTruncated,
	}
	if account != nil {
		record.AccountID = account.ID()
	}
	if captureErr != nil {
		record.Error = captureErr.Error()
	}
	emitCodexTransportDiagnostic(ctx, record)
}
