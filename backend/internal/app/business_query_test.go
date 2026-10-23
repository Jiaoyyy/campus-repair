package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBusinessScenarioRouting(t *testing.T) {
	for _, tt := range []struct {
		role, text, queue, status, category, period string
		ok                                          bool
	}{
		{"admin", "我现在有多少工单要处理", "action", "", "", "all", true},
		{"admin", "有哪些单等我审核", "all", "submitted", "", "all", true},
		{"admin", "今天新增了多少报修", "all", "", "", "today", true},
		{"requester", "我的报修还有多少单未结束", "active", "", "", "all", true},
		{"requester", "有哪些报修待我确认", "all", "awaiting_confirmation", "", "all", true},
		{"technician", "今天派给我的有哪些活", "all", "", "", "today", true},
		{"admin", "照明有多少待派单工单", "all", "unassigned", "lighting", "all", true},
		{"requester", "照明报修需要什么信息", "all", "", "", "all", false},
		{"admin", "我现在有多少工单需要处理", "action", "", "", "all", true},
		{"requester", "报修需要多少时间", "all", "", "", "all", false},
		{"requester", "报修需要填写哪些信息", "all", "", "", "all", false},
		{"requester", "宿舍灯坏了，帮我报修", "all", "", "", "all", false},
		{"admin", "照明维修的安全规定有哪些", "all", "", "", "all", false},
	} {
		q, ok := businessIntent(tt.role, tt.text, nil)
		if ok != tt.ok || (ok && (q.Queue != tt.queue || q.Status != tt.status || q.Category != tt.category || q.Period != tt.period)) {
			t.Fatalf("%s/%s => %+v %v", tt.role, tt.text, q, ok)
		}
	}
	prev := OrderQuery{Queue: "active", Period: "today"}
	q, ok := businessIntent("requester", "那照明的呢", &prev)
	if !ok || q.Queue != "active" || q.Category != "lighting" || q.Period != "today" {
		t.Fatal("followup lost scope")
	}
	q, ok = businessIntent("requester", "全部类别", &q)
	if !ok || q.Category != "" || q.Queue != "active" {
		t.Fatal("category reset lost other filters")
	}
	if _, ok = businessIntent("admin", "你好", &prev); ok {
		t.Fatal("greeting became business query")
	}
	if _, ok = businessIntent("admin", "照明的", nil); ok {
		t.Fatal("context-free short word queried data")
	}
}
func TestBusinessDateAndScope(t *testing.T) {
	now := time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC)
	start, end := chinaToday(now)
	if start != time.Date(2026, 10, 22, 16, 0, 0, 0, time.UTC) || end.Sub(start) != 24*time.Hour {
		t.Fatal("Beijing midnight boundary")
	}
	for _, role := range []string{"admin", "requester", "technician"} {
		where, args, basis, err := businessQuerySQL(User{ID: 7, OrgID: 2, Role: role}, OrderQuery{Queue: "action", Period: "today"}, now)
		if err != nil || !strings.Contains(where, "org_id=?") || args[0] != int64(2) || basis == "" {
			t.Fatal("missing authorized scope")
		}
		if role == "requester" && !strings.Contains(where, "requester_id=?") {
			t.Fatal("requester leaked")
		}
		if role == "technician" && (!strings.Contains(where, "assignee_id=?") || !strings.Contains(where, "MAX(e.created_at)")) {
			t.Fatal("technician date or assignment leaked")
		}
	}
	for _, raw := range []string{`{"queue":"action","org_id":2}`, `{"status":"unknown"}`, `{"period":"last-year"}`, `{"category":"unknown"}`, `{"queue":"action","user_id":8}`, `{"number":"' OR 1=1"}`, `{"location":null}`, `{"period":"recent","period":"all"}`, `{"queue":7}`} {
		if validMCPArguments(json.RawMessage(raw), MCPQueryOrders) {
			t.Fatal("invalid/identity args accepted", raw)
		}
	}
	if !validMCPArguments(json.RawMessage(`{"queue":"action","category":"lighting"}`), MCPQueryOrders) {
		t.Fatal("valid query rejected")
	}
	if !mcpRoleAllowed(MCPQueryOrders, "admin") || !mcpRoleAllowed(MCPQueryOrders, "requester") || mcpRoleAllowed(MCPDispatch, "requester") || mcpRoleAllowed(MCPQueryOrders, "other") {
		t.Fatal("shared tool role bounds")
	}
}

func TestQueryIsNotAWriteRequest(t *testing.T) {
	if businessWriteRequest("今天派给我的有哪些活") {
		t.Fatal("read-only assignment question blocked")
	}
	if !businessWriteRequest("帮我直接派单给第一位师傅") {
		t.Fatal("write request not identified")
	}
}

func TestUnsupportedFiltersAreExplicit(t *testing.T) {
	for _, q := range []string{"上周有多少报修", "照明和电器有多少工单"} {
		if queryClarification("admin", q, false) == "" {
			t.Fatal("unsupported scope silently treated as all", q)
		}
	}
	if queryClarification("requester", "宿舍灯坏了，帮我报修", false) != "" {
		t.Fatal("draft instruction intercepted")
	}
}
