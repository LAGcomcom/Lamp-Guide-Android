package lane

import (
	"context"
	"testing"
)

// 同一个 id 在两条 lane 里各有一条（免费车道的 deepseek-v4.1-flash 与
// WorkBuddy 的同名条目）。按 id 探测只能捞到第一条，界面上点 WorkBuddy 的
// 那一行会得到免费车道的结论（"需要配置渠道 API Key"）。channel 限定必须
// 能挑中正确的那一条。
func TestProbeOneCtxChannelPicksTheRightRow(t *testing.T) {
	l := NewLane()
	l.mu.Lock()
	l.catalog = append(l.catalog,
		ModelInfo{ID: "deepseek-v4.1-flash", Channel: "", Wire: "chat", RequiresKey: true},
		ModelInfo{ID: "deepseek-v4.1-flash", Channel: ChannelWorkBuddy, Wire: "chat"},
	)
	l.mu.Unlock()

	// 限定 workbuddy：挑中的那条不需要 key，不会走"需要配置渠道 API Key"。
	got := l.ProbeOneCtxChannel(context.Background(), "deepseek-v4.1-flash", ChannelWorkBuddy)
	if got.Detail == "此模型需要配置渠道 API Key" {
		t.Fatalf("channel 限定失效，探到了免费车道那条: %+v", got)
	}

	// 不限定：保持旧行为，按 id 取第一条（RequiresKey 那条）。
	got = l.ProbeOneCtxChannel(context.Background(), "deepseek-v4.1-flash", "")
	if got.Detail != "此模型需要配置渠道 API Key" {
		t.Fatalf("空 channel 应保持旧行为（取第一条）: %+v", got)
	}
}

// 找不到对应 (id, channel) 时返回 unknown，而不是错误地套用别的来源的结论。
func TestProbeOneCtxChannelUnknownWhenMissing(t *testing.T) {
	l := NewLane()
	got := l.ProbeOneCtxChannel(context.Background(), "nope", ChannelWorkBuddy)
	if got.State != StateUnknown {
		t.Errorf("不存在的模型应判 unknown，得到 %+v", got)
	}
}
