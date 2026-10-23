package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNativeQueryPlanProtocol(t *testing.T) {
	for _, tt := range []struct {
		name, tool, args, finish string
		role                     string
		good                     bool
	}{
		{"location", MCPQueryOrders, `{"location":"研发楼","period":"recent"}`, "tool_calls", "admin", true},
		{"keyword", MCPQueryOrders, `{"location":"3号楼","keyword":"漏水","date_from":"2026-10-01","date_to":"2026-10-07"}`, "tool_calls", "requester", true},
		{"ordinary", "continue_conversation", `{}`, "tool_calls", "admin", true},
		{"write", "assign", `{}`, "tool_calls", "admin", false},
		{"identity", MCPQueryOrders, `{"org_id":2}`, "tool_calls", "admin", false},
		{"role", MCPDispatch, `{"category":"lighting"}`, "tool_calls", "requester", false},
		{"bad category", MCPDispatch, `{"category":"all"}`, "tool_calls", "admin", false},
		{"malformed", MCPQueryOrders, `{"location":`, "tool_calls", "admin", false},
		{"range", MCPQueryOrders, `{"date_from":"2026-10-08","date_to":"2026-10-07"}`, "tool_calls", "admin", false},
		{"mixed", MCPQueryOrders, `{"period":"today","date_from":"2026-10-01","date_to":"2026-10-07"}`, "tool_calls", "admin", false},
		{"truncated", MCPQueryOrders, `{}`, "length", "admin", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var req map[string]any
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					t.Error("bad request")
				}
				if req["tool_choice"] != "required" || req["parallel_tool_calls"] != false || len(req["tools"].([]any)) != 5 {
					t.Error("native tools missing")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				// Arguments split across valid JSON string deltas.
				for i, part := range []string{string([]rune(tt.args)[:len([]rune(tt.args))/2]), string([]rune(tt.args)[len([]rune(tt.args))/2:])} {
					call := map[string]any{"index": 0, "function": map[string]any{"arguments": part}}
					if i == 0 {
						call["id"] = "call_1"
						call["type"] = "function"
						call["function"].(map[string]any)["name"] = tt.tool
					}
					b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{call}}}}})
					fmt.Fprintf(w, "data: %s\n\n", b)
				}
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":%q}]}\n\ndata: [DONE]\n\n", tt.finish)
			}))
			defer server.Close()
			g := ChatGateway{URL: server.URL, Key: "fixture", Model: "fixture", Timeout: time.Second}
			_, err := g.planQuery(context.Background(), tt.role, nil, "研发楼最近有什么故障", nil, nil)
			if (err == nil) != tt.good {
				t.Fatalf("expected good=%v err=%v", tt.good, err)
			}
			if calls != 1 {
				t.Fatal("automatic retry")
			}
		})
	}
}

func TestSemanticQueryDatesAndLiteralSearch(t *testing.T) {
	now := time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC)
	for _, tt := range []struct{ period, start, end string }{
		{"recent", "2026-10-16T16:00:00Z", "2026-10-23T16:00:00Z"},
		{"yesterday", "2026-10-21T16:00:00Z", "2026-10-22T16:00:00Z"},
		{"this_week", "2026-10-18T16:00:00Z", "2026-10-23T16:00:00Z"},
		{"last_week", "2026-10-11T16:00:00Z", "2026-10-18T16:00:00Z"},
		{"this_month", "2026-09-30T16:00:00Z", "2026-10-23T16:00:00Z"},
	} {
		a, b := queryDates(OrderQuery{Period: tt.period}, now)
		if a.Format(time.RFC3339) != tt.start || b.Format(time.RFC3339) != tt.end {
			t.Fatalf("%s %s %s", tt.period, a, b)
		}
	}
	for _, role := range []string{"admin", "requester", "technician"} {
		q := OrderQuery{Location: "研发楼%_' OR 1=1", Keyword: "漏水", Period: "recent"}
		where, args, basis, err := businessQuerySQL(User{ID: 7, OrgID: 2, Role: role}, q, now)
		if err != nil || !strings.Contains(where, "LOCATE(?,location)>0") || strings.Contains(where, q.Location) || !strings.Contains(basis, "2026-10-17") || len(args) < 5 {
			t.Fatalf("invalid literal scoped query %s", where)
		}
	}
	for _, q := range []OrderQuery{{DateFrom: "2026-01-01"}, {DateFrom: "2026-02-30", DateTo: "2026-03-01"}, {DateFrom: "2024-01-01", DateTo: "2026-01-01"}, {Location: strings.Repeat("楼", 201)}} {
		if q.valid() {
			t.Fatal("bad scope accepted")
		}
	}
}

func TestUnverifiedBusinessGuard(t *testing.T) {
	for _, text := range []string{"目前没有查询到研发楼近期的故障报修记录。", "本组织工单共有3单。", "未查到维修记录。", "研发楼有3单。", "已经派单给师傅。"} {
		if !unverifiedBusinessAnswer(text) {
			t.Fatal("false claim passed", text)
		}
	}
	for _, text := range []string{"你好，可以帮你梳理故障。", "请提供楼栋和房间。", "请在工单页面查看进度。"} {
		if unverifiedBusinessAnswer(text) {
			t.Fatal("ordinary answer blocked")
		}
	}
}

func TestQueryGatewayFailures(t *testing.T) {
	for _, tt := range []struct {
		status int
		body   string
		quota  bool
	}{{429, `{"error":{"message":"rate limit exceeded"}}`, false}, {429, `{"error":{"message":"额度不足"}}`, true}, {429, `{"error":{"message":"\u989d\u5ea6\u4e0d\u8db3"}}`, true}, {503, `private upstream diagnostic`, false}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tt.status); fmt.Fprint(w, tt.body) }))
		g := ChatGateway{URL: server.URL, Key: "fixture", Model: "fixture", Timeout: time.Second}
		_, err := g.planQuery(context.Background(), "admin", nil, "研发楼最近有什么故障", nil, nil)
		e, ok := err.(*queryGatewayError)
		if !ok || e.Status != tt.status || e.Quota != tt.quota || strings.Contains(e.Error(), tt.body) {
			t.Fatal("gateway failure misclassified or leaked")
		}
		server.Close()
	}
}
