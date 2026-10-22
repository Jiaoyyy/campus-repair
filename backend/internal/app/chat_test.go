package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatStreamProtocol(t *testing.T) {
	for _, tt := range []struct {
		body string
		ok   bool
	}{
		{"data: {\"choices\":[{\"delta\":{\"content\":\"中文\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", true},
		{"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n", false},
		{"data: {bad\n\n", false}, {"data: [DONE]\n\n", false},
		{"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{}]},\"finish_reason\":\"tool_calls\"}]}\n\n", false},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write([]byte(tt.body))
		}))
		g := ChatGateway{URL: srv.URL, Key: "test-only", Model: "mock"}
		out := ""
		err := g.stream(context.Background(), nil, func(text string) error { out += text; return nil })
		if (err == nil) != tt.ok {
			t.Fatal("invalid stream accepted")
		}
		if tt.ok && out != "中文" {
			t.Fatal("unicode changed")
		}
		srv.Close()
	}
}
func TestQuestionBounds(t *testing.T) {
	for _, tt := range []struct {
		text string
		ok   bool
	}{{" ", false}, {strings.Repeat("💡", 2000), true}, {strings.Repeat("灯", 2001), false}, {string([]byte{255}), false}} {
		if validQuestion(tt.text) != tt.ok {
			t.Fatal("question length/encoding")
		}
	}
}
func TestSafetyGuard(t *testing.T) {
	for _, text := range []string{"拆开配电箱。", "先接线再处理。", "剥线后短接。"} {
		if !unsafeAnswer(text) {
			t.Fatal("unsafe sentence accepted")
		}
	}
	if unsafeAnswer("请填写楼栋与房间。") {
		t.Fatal("safe answer blocked")
	}
}

func TestFinalContentWithStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"error\":null,\"choices\":[{\"delta\":{\"content\":\"final\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	out := ""
	g := ChatGateway{URL: srv.URL, Model: "mock", Key: "fixture"}
	if err := g.stream(context.Background(), nil, func(v string) error { out += v; return nil }); err != nil || out != "final" {
		t.Fatal("final content with stop rejected")
	}
}
