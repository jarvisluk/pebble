package push

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"bot-hub/internal/state"
)

type mockSender struct {
	mu   sync.Mutex
	reqs []SendRequest
	err  error
}

func (m *mockSender) Mode() string { return "direct" }

func (m *mockSender) Send(_ context.Context, req SendRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs = append(m.reqs, req)
	return m.err
}

func (m *mockSender) list() []SendRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SendRequest, len(m.reqs))
	copy(out, m.reqs)
	return out
}

func TestManagerLiveActivityThrottleAndTakeover(t *testing.T) {
	store, err := state.NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.UpsertPushDevice(state.PushDevice{
		DeviceID:               "iphone-1",
		Platform:               "ios",
		Env:                    "sandbox",
		BundleID:               "local.pebble.mobile",
		PushToken:              "push-tok",
		LiveActivityStartToken: "start-tok",
		Enabled:                true,
	}); err != nil {
		t.Fatal(err)
	}

	sender := &mockSender{}
	mgr := NewManagerWithSender(store, sender)
	defer mgr.Close()

	// 1. Bot becomes busy -> Push-to-Start live activity.
	mgr.processEvent(rawEvent{
		Type:  "bot_status",
		BotID: "alpha",
		Payload: map[string]any{
			"id":     "alpha",
			"name":   "Alpha",
			"status": "busy",
		},
	})
	reqs := sender.list()
	if len(reqs) != 1 || reqs[0].PushType != PushTypeLiveActivity || reqs[0].Token != "start-tok" {
		t.Fatalf("expected push-to-start liveactivity, got %+v", reqs)
	}

	// Register activity token for bot:alpha, then burst two tool_use events -> both throttled within 3s.
	_ = store.SetPushActivityToken("iphone-1", "bot:alpha", "act-tok")
	mgr.processEvent(rawEvent{Type: "tool_use", BotID: "alpha", Payload: map[string]any{"botId": "alpha", "tool": "bash"}})
	mgr.processEvent(rawEvent{Type: "tool_use", BotID: "alpha", Payload: map[string]any{"botId": "alpha", "tool": "read"}})
	if len(sender.list()) != 1 {
		t.Fatalf("expected tool_use within 3s to be throttled, got %d requests", len(sender.list()))
	}

	// 2. Bot returns idle -> end live activity.
	mgr.processEvent(rawEvent{
		Type:  "bot_status",
		BotID: "alpha",
		Payload: map[string]any{
			"id":     "alpha",
			"name":   "Alpha",
			"status": "idle",
		},
	})
	reqs = sender.list()
	if len(reqs) != 2 || reqs[1].Token != "act-tok" {
		t.Fatalf("expected end liveactivity on act-tok, got %+v", reqs)
	}
	endPayload, _ := reqs[1].Payload.(APNSPayload)
	if endPayload.APS.Event != "end" || endPayload.APS.ContentState.StepCount != 2 {
		t.Fatalf("unexpected end payload: %+v", endPayload.APS)
	}

	// 3. Takeover request -> time-sensitive alert + desktop liveactivity start.
	mgr.processEvent(rawEvent{
		Type: "takeover",
		Payload: map[string]any{
			"items": []map[string]any{
				{"id": "tk-1", "target": "local", "reason": "需要扫码登录"},
			},
		},
	})
	reqs = sender.list()
	if len(reqs) != 4 {
		t.Fatalf("expected 4 total requests after takeover, got %d", len(reqs))
	}
	alertPayload, _ := reqs[2].Payload.(APNSPayload)
	if alertPayload.APS.InterruptionLevel != "time-sensitive" || alertPayload.DeepLink != "pebble://desktop?target=local" {
		t.Fatalf("unexpected takeover alert payload: %+v", alertPayload)
	}

	// 4. Question request -> alert emits pebble://bot/ deep link.
	mgr.processEvent(rawEvent{
		Type: "questions",
		Payload: map[string]any{
			"items": []map[string]any{
				{"id": "q-1", "botId": "alpha", "prompt": "Proceed?"},
			},
		},
	})
	reqs = sender.list()
	if len(reqs) != 5 {
		t.Fatalf("expected 5 total requests after question, got %d", len(reqs))
	}
	qPayload, _ := reqs[4].Payload.(APNSPayload)
	if qPayload.DeepLink != "pebble://bot/alpha" {
		t.Fatalf("expected question deep link pebble://bot/alpha, got %+v", qPayload)
	}
}

func TestCompactPreviewStripsMarkdown(t *testing.T) {
	raw := `## 周报汇总

| 模块 | 状态 |
| :--- | ---: |
| **API** | ~~旧版~~ *正常*<br>详见 [文档](https://example.com) |

- [x] 完成 ` + "`deploy.sh`" + ` 检查
> 引用内容 [^1]

![架构图](https://example.com/a.png) ![](local.png)

$$
\sum_{i=1}^{n} i = \frac{n(n+1)}{2}
$$

<details><summary>展开详情</summary>支持 <kbd>⌘K</kbd> 与 H<sub>2</sub>O</details>

` + "```go\nfmt.Println(1)\n```" + `

[^1]: 脚注说明，价格 $100 - $200 与 snake_case 保留。`

	got := compactPreview(raw, 500)
	wantParts := []string{
		"周报汇总",
		"模块 · 状态",
		"API · 旧版 正常 详见 文档",
		"完成 deploy.sh 检查",
		"引用内容 [1]",
		"[图片: 架构图]",
		"[图片]",
		"∑_i=1^n i = (n(n+1))/2",
		"展开详情 支持 ⌘K 与 H2O",
		"[代码]",
		"[1] 脚注说明，价格 $100 - $200 与 snake_case 保留。",
	}
	for _, part := range wantParts {
		if !strings.Contains(got, part) {
			t.Fatalf("compactPreview missing %q in %q", part, got)
		}
	}
	for _, forbidden := range []string{"##", "| :---", "~~", "**", "```", "https://example.com", "$$", `\sum`, `\frac`, "<details>", "<kbd>"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("compactPreview still contains %q in %q", forbidden, got)
		}
	}

	trunc := compactPreview("**一二三四五六七八九十**", 6)
	if trunc != "一二三四五…" {
		t.Fatalf("expected truncated preview '一二三四五…', got %q", trunc)
	}
}
