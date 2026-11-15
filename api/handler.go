package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxBodyBytes    = 64 << 10 // 64KB
	maxQuestionLen  = 500      // 글자
	maxSnippets     = 10
	maxSnippetChars = 4000
)

type statusResponse struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Register는 /api/status와 /api/ask를 붙인다.
// origin은 이 서버의 주소(예: http://127.0.0.1:52341)다. 다른 사이트에서 보낸 요청은 막는다.
func Register(mux *http.ServeMux, p Provider, origin string) {
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, errorResponse{"method_not_allowed"})
			return
		}
		writeJSON(w, http.StatusOK, statusResponse{
			Enabled:  p.Name() != "none",
			Provider: p.Name(),
			Model:    p.Model(),
		})
	})

	mux.HandleFunc("/api/ask", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, errorResponse{"method_not_allowed"})
			return
		}
		// 다른 웹사이트가 내 컴퓨터의 이 서버로 몰래 요청해 내 API 키를 쓰지 못하게 막는다.
		if o := r.Header.Get("Origin"); o != "" && o != origin {
			writeJSON(w, http.StatusForbidden, errorResponse{"forbidden_origin"})
			return
		}
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeJSON(w, http.StatusUnsupportedMediaType, errorResponse{"json_required"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorResponse{"invalid_json"})
			return
		}
		if msg := validate(&req); msg != "" {
			writeJSON(w, http.StatusBadRequest, errorResponse{msg})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		ans, err := p.Ask(ctx, req)
		switch {
		case errors.Is(err, ErrDisabled):
			writeJSON(w, http.StatusServiceUnavailable, errorResponse{"ai_disabled"})
		case errors.Is(err, ErrRefused):
			writeJSON(w, http.StatusUnprocessableEntity, errorResponse{"ai_refused"})
		case err != nil:
			// 자세한 오류는 이 컴퓨터의 창에만 남긴다. 키는 SDK가 오류 문구에 넣지 않는다.
			log.Printf("AI 호출 실패: %v", err)
			writeJSON(w, http.StatusBadGateway, errorResponse{"ai_error"})
		default:
			writeJSON(w, http.StatusOK, ans)
		}
	})
}

func validate(req *Request) string {
	req.Question = strings.TrimSpace(req.Question)
	switch {
	case req.Question == "":
		return "question_required"
	case utf8.RuneCountInString(req.Question) > maxQuestionLen:
		return "question_too_long"
	case len(req.Snippets) > maxSnippets:
		return "too_many_snippets"
	}
	for _, s := range req.Snippets {
		if utf8.RuneCountInString(s.Text) > maxSnippetChars {
			return "snippet_too_long"
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
