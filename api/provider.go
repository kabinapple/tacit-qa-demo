// Package api는 데모에 생성형 AI를 붙이는 자리다.
//
// 화면(web/index.html)은 지금 AI 없이 브라우저 안 검색만으로 답한다.
// 이 패키지는 그다음 단계를 위해, 근거 문장을 받아 그 안에서만 답하는
// AI 연결부를 Provider라는 하나의 틀로 묶어 둔다.
// 다른 회사 AI를 붙이고 싶으면 Provider를 하나 더 만들면 된다.
package api

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
)

// Snippet은 AI에 건네는 근거 하나다. 예: 규정 조항 원문, 담당자 답.
type Snippet struct {
	Source string `json:"source"` // 예: "제12조(경조 휴가)", "담당자 답 (총무 담당, 2026-11-14)"
	Text   string `json:"text"`
}

// Request는 질문과 근거 목록이다.
type Request struct {
	Question string    `json:"question"`
	Snippets []Snippet `json:"snippets"`
}

// Answer는 AI의 답이다. 근거에서 답을 찾지 못하면 Found가 false다.
type Answer struct {
	Found    bool     `json:"found"`
	Text     string   `json:"text"`
	Sources  []string `json:"sources"`
	Provider string   `json:"provider"`
}

// Provider는 AI 연결부가 지켜야 할 틀이다.
type Provider interface {
	Name() string
	Model() string
	Ask(ctx context.Context, req Request) (Answer, error)
}

// ErrDisabled는 AI가 꺼져 있을 때 돌려준다.
var ErrDisabled = errors.New("api: AI 연결이 꺼져 있습니다")

// FromEnv는 환경 변수(.env)를 보고 Provider를 고른다.
// 설정이 없거나 키가 비어 있으면 항상 꺼진 상태(None)로 시작한다.
func FromEnv() Provider {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AI_PROVIDER"))) {
	case "claude":
		key := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
		if key == "" {
			log.Println("AI_PROVIDER=claude 이지만 ANTHROPIC_API_KEY가 비어 있어 AI 없이 실행합니다.")
			return None{}
		}
		return NewClaude(key, os.Getenv("CLAUDE_MODEL"))
	default:
		return None{}
	}
}

// None은 기본값이다. AI를 부르지 않는다.
type None struct{}

func (None) Name() string  { return "none" }
func (None) Model() string { return "" }
func (None) Ask(context.Context, Request) (Answer, error) {
	return Answer{}, ErrDisabled
}
