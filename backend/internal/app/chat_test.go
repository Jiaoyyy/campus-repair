package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestConversationContextSelection(t *testing.T) {
	history := []chatMessage{{Role: "user", Content: "宿舍灯坏了"}, {Role: "assistant", Content: "在哪栋楼、哪个房间？"}}
	if knowledgeQuery("你好，你能做什么", history) != "" {
		t.Fatal("greeting should not search previous repair text")
	}
	if q := knowledgeQuery("三号楼201", history); !strings.Contains(q, "灯坏了") || !strings.Contains(q, "201") {
		t.Fatal("follow-up lost repair context")
	}
	if knowledgeQuery("你好", nil) != "" {
		t.Fatal("greeting should not depend on knowledge")
	}
	if len([]rune(knowledgeQuery(strings.Repeat("报修", 500), nil))) > 500 {
		t.Fatal("query must respect retrieval limit")
	}
	messages := buildChatMessages(Agent{Prompt: "帮助报修人"}, history, "三号楼201", nil, "本轮无资料，继续追问。")
	if len(messages) != 5 || !reflect.DeepEqual(messages[2:4], history) || messages[4].Content != "三号楼201" {
		t.Fatal("completed conversation history was lost")
	}
	if !strings.Contains(messages[0].Content, "不能创建工单") || !strings.Contains(messages[0].Content, "不编造位置") {
		t.Fatal("read-only and missing-information constraints absent")
	}
}

func TestSafetyWarningReferences(t *testing.T) {
	for _, text := range []string{
		"不要自行拆卸、接线或带电操作。[1]",
		"请远离故障区域。[1]不要自行拆卸、接线或带电维修。[1]",
		"请勿拆开配电箱。[1]", "严禁带电维修[2]。",
		"发现冒烟请停止使用；不要自行拆卸、接线或带电维修。[1]",
		"不要自行拆电路或进行带电操作", safeAnswer,
	} {
		if unsafeAnswer(text) {
			t.Errorf("prohibition blocked: %s", text)
		}
	}
	for _, text := range []string{
		"不要拆卸，但可以接线。", "并非不要接线。", "不要不接线。",
		"不要先拆卸再接线。", "不要犹豫，拆开配电箱。",
		"不要拆卸、然后接线。", "不要拆卸或尝试接线。",
		"不要拆卸，接线即可。", "可以带电维修。", "拆电路。",
		"不要拆卸[1]然后接线", "如果不要拆卸就接线。",
	} {
		if !unsafeAnswer(text) {
			t.Errorf("unsafe instructions accepted: %s", text)
		}
	}
	if knowledgeQuery("不要自行拆电路或进行带电操作", nil) == "" {
		t.Fatal("standalone electrical safety query skipped retrieval")
	}
}

func TestNumberedEvidencePrompt(t *testing.T) {
	messages := buildChatMessages(Agent{}, nil, "维修安全", []Evidence{{ID: 80, MaterialID: 42, Chunk: Chunk{Text: "禁止带电维修。"}}, {ID: 90, MaterialID: 43, Chunk: Chunk{Text: "停止使用。"}}}, "")
	raw := strings.SplitN(messages[1].Content, "：", 2)[1]
	var entries []struct {
		Citation string `json:"citation"`
		Evidence
	}
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Citation != "[1]" || entries[1].Citation != "[2]" || entries[0].ID != 80 || entries[0].Text != "禁止带电维修。" {
		t.Fatal("citation labels must match candidate order, not database IDs")
	}
}

func TestSafetyProhibitionCoordinatingConjunction(t *testing.T) {
	if unsafeAnswer("不要自行拆卸、接线及带电维修。[1]") {
		t.Fatal("simple coordinated prohibition rejected")
	}
	if !unsafeAnswer("不要拆卸及可以接线。") {
		t.Fatal("positive step bypassed prohibition")
	}
}
