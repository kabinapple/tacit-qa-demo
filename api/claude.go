package api

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

const (
	defaultClaudeModel = "claude-opus-5-5"
	notFoundMarker     = "NOT_FOUND"
)

// 근거 안에서만 답하게 하는 지시문. 데모의 원칙(지어낸 답을 내지 않는다)을 AI에도 똑같이 건다.
const claudeSystemPrompt = `너는 사내 규정 Q&A 도우미다.
- 사용자 메시지의 <근거> 안에 있는 내용만 써서 질문에 한두 문장으로 답한다.
- 근거를 쓴 문장 끝에는 [1]처럼 근거 번호를 붙인다.
- 근거에 없는 사실, 숫자, 날짜, 절차는 만들지 않는다.
- 근거로 답할 수 없으면 다른 말 없이 NOT_FOUND 한 단어만 출력한다.`

// Claude는 Anthropic Claude API로 답하는 Provider다.
type Claude struct {
	client anthropic.Client
	model  string
}

// NewClaude는 API 키와 모델로 Claude Provider를 만든다. 모델이 비면 기본 모델을 쓴다.
// opts는 시험용(가짜 서버 주소 등)이다.
func NewClaude(apiKey, model string, opts ...option.RequestOption) *Claude {
	if strings.TrimSpace(model) == "" {
		model = defaultClaudeModel
	}
	all := append([]option.RequestOption{option.WithAPIKey(apiKey)}, opts...)
	return &Claude{client: anthropic.NewClient(all...), model: model}
}

func (c *Claude) Name() string  { return "claude" }
func (c *Claude) Model() string { return c.model }

// ErrRefused는 AI가 안전 정책으로 답을 거절했을 때 돌려준다.
var ErrRefused = errors.New("api: AI가 답을 거절했습니다")

func (c *Claude) Ask(ctx context.Context, req Request) (Answer, error) {
	ans := Answer{Provider: c.Name()}
	// 근거가 없으면 AI를 부르지 않는다. 근거 없이 답하게 하지 않기 위해서다.
	if len(req.Snippets) == 0 {
		return ans, nil
	}

	resp, err := c.client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     c.model,
		MaxTokens: 4096,
		System:    []anthropic.BetaTextBlockParam{{Text: claudeSystemPrompt}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(buildPrompt(req))),
		},
		// 짧은 Q&A라 노력 수준은 낮게 둔다.
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortLow},
		// 안전 정책으로 거절되면 서버가 다른 모델로 다시 답하게 한다.
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	})
	if err != nil {
		return ans, err
	}

	switch resp.StopReason {
	case anthropic.BetaStopReasonRefusal:
		return ans, ErrRefused
	case anthropic.BetaStopReasonMaxTokens:
		return ans, fmt.Errorf("api: 답이 길이 한도에서 잘렸습니다")
	}

	var sb strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	text := strings.TrimSpace(sb.String())
	if text == "" || strings.Contains(text, notFoundMarker) {
		return ans, nil
	}

	ans.Found = true
	ans.Text = text
	ans.Sources = citedSources(text, req.Snippets)
	return ans, nil
}

// buildPrompt는 근거에 번호를 붙여 질문과 함께 묶는다.
func buildPrompt(req Request) string {
	var sb strings.Builder
	sb.WriteString("<근거>\n")
	for i, s := range req.Snippets {
		fmt.Fprintf(&sb, "[%d] (%s) %s\n", i+1, s.Source, s.Text)
	}
	sb.WriteString("</근거>\n\n질문: ")
	sb.WriteString(req.Question)
	return sb.String()
}

var citeRe = regexp.MustCompile(`\[(\d+)\]`)

// citedSources는 답에 붙은 [번호]를 근거 이름으로 바꾼다. 없는 번호는 버린다.
func citedSources(text string, snippets []Snippet) []string {
	seen := map[int]bool{}
	var out []string
	for _, m := range citeRe.FindAllStringSubmatch(text, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > len(snippets) || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, snippets[n-1].Source)
	}
	return out
}
