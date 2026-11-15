package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

const testOrigin = "http://127.0.0.1:50000"

type fakeProvider struct{ ans Answer }

func (fakeProvider) Name() string  { return "fake" }
func (fakeProvider) Model() string { return "fake-model" }
func (f fakeProvider) Ask(context.Context, Request) (Answer, error) {
	return f.ans, nil
}

func newTestServer(p Provider) *httptest.Server {
	mux := http.NewServeMux()
	Register(mux, p, testOrigin)
	return httptest.NewServer(mux)
}

func post(t *testing.T, url, origin, ctype, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

const okBody = `{"question":"경조 휴가는 며칠인가요?","snippets":[{"source":"제12조","text":"본인 결혼 5일"}]}`

func TestStatusDefaultsToDisabled(t *testing.T) {
	srv := newTestServer(None{})
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	var st statusResponse
	_ = json.NewDecoder(resp.Body).Decode(&st)
	if st.Enabled || st.Provider != "none" {
		t.Fatalf("기본값은 꺼짐이어야 함: %+v", st)
	}
}

func TestAskWhenDisabledReturns503(t *testing.T) {
	srv := newTestServer(None{})
	defer srv.Close()
	resp := post(t, srv.URL+"/api/ask", testOrigin, "application/json", okBody)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", resp.StatusCode)
	}
}

func TestAskRejectsOtherOrigin(t *testing.T) {
	srv := newTestServer(fakeProvider{})
	defer srv.Close()
	resp := post(t, srv.URL+"/api/ask", "https://evil.example", "application/json", okBody)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}

func TestAskRequiresJSON(t *testing.T) {
	srv := newTestServer(fakeProvider{})
	defer srv.Close()
	resp := post(t, srv.URL+"/api/ask", testOrigin, "text/plain", okBody)
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("want 415, got %d", resp.StatusCode)
	}
}

func TestAskValidatesQuestion(t *testing.T) {
	srv := newTestServer(fakeProvider{})
	defer srv.Close()
	resp := post(t, srv.URL+"/api/ask", testOrigin, "application/json", `{"question":"  "}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestAskReturnsProviderAnswer(t *testing.T) {
	srv := newTestServer(fakeProvider{ans: Answer{Found: true, Text: "5일입니다 [1]", Provider: "fake"}})
	defer srv.Close()
	resp := post(t, srv.URL+"/api/ask", testOrigin, "application/json", okBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var ans Answer
	_ = json.NewDecoder(resp.Body).Decode(&ans)
	if !ans.Found || ans.Text == "" {
		t.Fatalf("unexpected answer: %+v", ans)
	}
}

// 가짜 Claude 서버로 요청 모양과 답 해석을 확인한다. 실제 API 키나 요금은 쓰지 않는다.
func TestClaudeRequestAndParsing(t *testing.T) {
	var gotPath, gotKey, gotBeta string
	var gotBody map[string]any
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("Anthropic-Beta")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_test","type":"message","role":"assistant","model":"claude-opus-5-5",
			"content":[{"type":"text","text":"본인 결혼 시 경조 휴가는 5일입니다 [1]."}],
			"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":12}}`)
	}))
	defer mock.Close()

	c := NewClaude("test-key", "", option.WithBaseURL(mock.URL), option.WithMaxRetries(0))
	ans, err := c.Ask(context.Background(), Request{
		Question: "결혼하면 휴가 며칠?",
		Snippets: []Snippet{{Source: "제12조(경조 휴가)", Text: "본인 결혼 5일"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path = %q", gotPath)
	}
	// SDK v2는 API 키를 Authorization: Bearer 헤더로 보낸다.
	if gotKey != "Bearer test-key" {
		t.Errorf("api key header = %q", gotKey)
	}
	if !strings.Contains(gotBeta, "server-side-fallback-2026-07-01") {
		t.Errorf("anthropic-beta = %q", gotBeta)
	}
	if gotBody["model"] != "claude-opus-5-5" || gotBody["fallbacks"] != "default" {
		t.Errorf("body model/fallbacks = %v / %v", gotBody["model"], gotBody["fallbacks"])
	}
	if !ans.Found || len(ans.Sources) != 1 || ans.Sources[0] != "제12조(경조 휴가)" {
		t.Errorf("answer = %+v", ans)
	}
}

func TestClaudeNotFoundAndNoSnippets(t *testing.T) {
	calls := 0
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_test","type":"message","role":"assistant","model":"claude-opus-5-5",
			"content":[{"type":"text","text":"NOT_FOUND"}],"stop_reason":"end_turn","stop_sequence":null,
			"usage":{"input_tokens":10,"output_tokens":2}}`)
	}))
	defer mock.Close()
	c := NewClaude("test-key", "", option.WithBaseURL(mock.URL), option.WithMaxRetries(0))

	ans, err := c.Ask(context.Background(), Request{Question: "회의실 예약은?"})
	if err != nil || ans.Found || calls != 0 {
		t.Fatalf("근거가 없으면 AI를 부르지 않아야 함: ans=%+v err=%v calls=%d", ans, err, calls)
	}
	ans, err = c.Ask(context.Background(), Request{Question: "회의실 예약은?", Snippets: []Snippet{{Source: "제3조", Text: "근로시간"}}})
	if err != nil || ans.Found {
		t.Fatalf("NOT_FOUND면 Found=false여야 함: ans=%+v err=%v", ans, err)
	}
}
