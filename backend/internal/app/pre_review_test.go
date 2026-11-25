package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReviewSuggestionsRequireGroundedModelOutput(t *testing.T) {
	rows := []BusinessOrder{{ID: 1, Location: "三号楼201", Description: "顶灯不亮"}, {ID: 2, Location: "三号楼202", Description: "插座冒烟"}, {ID: 3, Location: "二楼", Description: "坏了"}, {ID: 4, Location: "实验楼301", Description: "桌脚松动"}}
	output := `{"items":[{"id":1,"decision":"approve","evidence":"顶灯不亮","reason":"位置和故障明确"},{"id":2,"decision":"approve","evidence":"冒烟","reason":"已有描述"},{"id":3,"decision":"supplement","evidence":"坏了","reason":"缺少设备和位置","question":"请提供房间号和故障设备。"},{"id":4,"decision":"approve","evidence":"灯不亮","reason":"位置明确"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		delta, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": output}, "finish_reason": "stop"}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", delta)
	}))
	defer server.Close()
	suggestReviews(context.Background(), ChatGateway{URL: server.URL, Key: "test", Model: "test", Timeout: time.Second}, rows, "", nil)
	for i, want := range []string{"approve", "manual", "supplement", "manual"} {
		if rows[i].Review.Decision != want {
			t.Fatalf("row %d: %+v", i, rows[i].Review)
		}
	}
	suggestReviews(context.Background(), ChatGateway{}, rows, "", nil)
	for _, row := range rows {
		if row.Review.Decision != "manual" {
			t.Fatal("gateway unavailable but row selectable")
		}
	}
}
func TestPreReviewScopeAndRole(t *testing.T) {
	if adminWorklistIntent("admin", "帮我审核并派单") != "pre_review" || adminWorklistIntent("technician", "帮我审核并派单") != "" {
		t.Fatal("review intent leaked")
	}
	prior := OrderQuery{Status: "submitted", Location: "图书馆", Category: "network", Period: "this_week"}
	q := pendingReviewScope(&prior)
	if q.Location != prior.Location || q.Category != prior.Category || q.Period != prior.Period || q.Status != "submitted" {
		t.Fatal(q)
	}
	payload, _ := json.Marshal(mcpCategoryInput{Worklist: "pre_review", Scope: &q})
	if !validMCPArguments(payload, MCPDispatch) || validMCPArguments([]byte(`{"worklist":"pre_review","scope":{"status":"assigned"}}`), MCPDispatch) {
		t.Fatal("review argument bounds")
	}
}

func TestFocusedReviewLocationUsesSelectedSnapshot(t *testing.T) {
	card := BusinessResult{Kind: "pre_review", ReviewOrder: 7, Total: 2, Items: []BusinessOrder{{ID: 7, Number: "RP7", Location: "演练楼301"}}}
	answer, err := explainReview(context.Background(), ChatGateway{}, Turn{}, "第二张的位置在哪里？", &card)
	if err != nil || answer != "RP7 当时记录的位置是「演练楼301」。" {
		t.Fatalf("selected snapshot: %q %v", answer, err)
	}
}

func TestAdminStartsReviewWithoutPriorQueue(t *testing.T) {
	for _, input := range []string{"审核", "帮我审核", "审核未审核的工单", "审核最近工单", "请帮我审核一下待审核的工单"} {
		if got := adminWorklistIntent("admin", input); got != "pre_review" {
			t.Errorf("%s: got %q", input, got)
		}
		if got := adminWorklistIntent("requester", input); got != "" {
			t.Errorf("requester %s: got %q", input, got)
		}
	}
	for _, input := range []string{"还有几单没审核", "为什么建议审核通过", "审核通过所有工单"} {
		if got := adminWorklistIntent("admin", input); got == "pre_review" {
			t.Errorf("unexpected initial review for %s", input)
		}
	}
}

func TestMissingBuildingCannotPassReview(t *testing.T) {
	for _, loc := range []string{"测试%_楼201", "某某楼301", "XX栋201", "201室"} {
		rows := []BusinessOrder{{Location: loc, Description: "洗手池水管漏水"}, {Location: loc, Description: "插座冒烟"}}
		suggestReviews(context.Background(), ChatGateway{}, rows, "", nil)
		if rows[0].Review.Decision != "supplement" || rows[1].Review.Decision != "manual" {
			t.Fatal(loc, rows)
		}
	}
	for _, loc := range []string{"一教一楼厕所", "研发楼走廊", "测试中心201", "实验楼301", "3号楼 3201"} {
		if needsReviewBuilding(loc) {
			t.Fatalf("valid address blocked: %s", loc)
		}
	}
}
