package app

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWorklistKeepsFullSnapshotAndFiveRowPreview(t *testing.T) {
	items := make([]BusinessOrder, 17)
	for i := range items {
		items[i] = BusinessOrder{ID: int64(i + 1), Number: fmt.Sprintf("RP%08d", i+1), Status: "submitted", Category: "lighting", Advice: "核对报修"}
	}
	b := BusinessResult{Kind: "worklist", Query: OrderQuery{Queue: "active", Period: "recent"}, At: time.Now(),
		Total: len(items), Items: items, Statuses: map[string]int{"submitted": len(items)}}
	result, _, err := decodeWorklist(map[string]any{"worklist": b}, "recent_review", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 17 || len(result.Items) != 5 || len(result.worklistRows) != 17 {
		t.Fatalf("unexpected preview and snapshot lengths: %d, %d", len(result.Items), len(result.worklistRows))
	}
	b.Items = b.Items[:5]
	legacy, _, err := decodeWorklist(map[string]any{"worklist": b}, "recent_review", nil)
	if err != nil || legacy.Total != 17 || len(legacy.worklistRows) != 0 {
		t.Fatalf("legacy preview should remain readable: %v", err)
	}
}

func TestAdminWorklistScope(t *testing.T) {
	for _, tc := range []struct{ role, input, want string }{
		{"admin", "帮我把没派单的派单", "dispatch"}, {"admin", "最近哪些工单该先处理？", "recent_review"},
		{"admin", "帮我把最近的工单处理一下", "recent_review"}, {"technician", "把没派单的派单", ""},
		{"requester", "哪些工单比较急", ""}, {"admin", "把研发楼没派单的派单", ""},
	} {
		if got := adminWorklistIntent(tc.role, tc.input); got != tc.want {
			t.Errorf("%s = %q", tc.input, got)
		}
	}
	for _, raw := range []string{`{"worklist":"other"}`, `{"worklist":"dispatch","category":"lighting"}`, `{"worklist":"dispatch","role":"admin"}`} {
		if validMCPArguments([]byte(raw), MCPDispatch) {
			t.Errorf("accepted %s", raw)
		}
	}
}
func TestWorklistRecommendations(t *testing.T) {
	people := []BusinessCandidate{{ID: 1, Name: "甲", Categories: []string{"lighting"}}, {ID: 2, Name: "乙", Categories: []string{"lighting"}}}
	planned := map[int64]int{}
	for _, name := range []string{"甲", "乙", "甲"} {
		person, advice := recommendCandidate(people, planned)
		if person.Name != name || !strings.Contains(advice, name) {
			t.Error("planned load ignored")
		}
	}
	person, advice := recommendCandidate(nil, planned)
	if person != nil || !strings.Contains(advice, "没有") {
		t.Error("expected no candidate")
	}
	o := BusinessOrder{Status: "submitted", Location: "二楼", Description: "插座周围有积水"}
	if priorityClue(o.Description) == "" || !strings.Contains(reviewAdvice(o), "补充") {
		t.Error("risk and missing location should coexist")
	}
	if priorityClue("未见火花，灯不亮") != "" {
		t.Error("negated risk")
	}
	o.Status = "awaiting_confirmation"
	if !strings.Contains(reviewAdvice(o), "等待报修人") {
		t.Error("incorrect responsibility")
	}
}

func TestAdminFollowupQuestions(t *testing.T) {
	if !asksUrgency("哪个工单比较紧急？") || !asksUrgency("这几单里哪张最紧急？") || asksUrgency("研发楼有哪些紧急工单？") {
		t.Error("incorrect urgency scope")
	}
	for _, input := range []string{"把超市的工单都结束", "把超时的工单都结束"} {
		if !closeOrdersRequest(input) {
			t.Error("missed bulk close request")
		}
	}
	if closeOrdersRequest("昨天有哪些工单已结束") {
		t.Error("read-only status question classified as a write")
	}
}
