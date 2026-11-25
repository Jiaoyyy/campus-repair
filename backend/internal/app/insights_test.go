package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestInsightArgumentAndIntentGrounding(t *testing.T) {
	for _, tt := range []struct{ input, mode string }{{"最近出现最多的问题是什么", "frequent"}, {"现在有几单白跑了", "no_show"}, {"有几单上门异常", "exceptions"}, {"总结当前有哪些问题", "overview"}, {"研发楼有什么问题", ""}, {"你好", ""}} {
		if got := analysisIntent(tt.input); got != tt.mode {
			t.Fatalf("intent %s: %s", tt.input, got)
		}
		if tt.mode != "" {
			q := groundAnalysisQuery(OrderQuery{Keyword: "白跑"}, tt.input, nil)
			wantPeriod := "recent"
			if tt.mode == "overview" {
				wantPeriod = "all"
			}
			if q.Analysis != tt.mode || q.Keyword != "" || q.Period != wantPeriod || tt.mode == "overview" && q.Queue != "active" {
				t.Fatalf("ground %+v", q)
			}
		}
	}
	for _, raw := range []string{`{"analysis":"overview"}`, `{"analysis":"no_show","period":"yesterday"}`, `{"analysis":"frequent","location":"楼%_"}`} {
		if !validMCPArguments(json.RawMessage(raw), MCPQueryOrders) {
			t.Fatal("valid rejected", raw)
		}
	}
	for _, raw := range []string{`{"analysis":"assign"}`, `{"analysis":"overview","org_id":"2"}`, `{"analysis":"overview","analysis":"no_show"}`, `{"analysis":true}`, `{"analysis":"frequent","user_id":"8"}`} {
		if validMCPArguments(json.RawMessage(raw), MCPQueryOrders) {
			t.Fatal("invalid accepted", raw)
		}
	}
	prev := &BusinessResult{Kind: "analysis", Query: OrderQuery{Analysis: "overview", DateFrom: "2026-10-01", DateTo: "2026-10-07"}}
	q := groundAnalysisQuery(OrderQuery{Analysis: "frequent"}, "那高发问题呢", prev)
	if q.DateFrom != "2026-10-01" || q.Period != "" {
		t.Fatal("follow range", q)
	}
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	f, args, basis := noShowWindow(OrderQuery{Period: "yesterday"}, now)
	if !strings.Contains(f, "no_show_at") || strings.Contains(f, "created_at") || len(args) != 2 || !strings.Contains(basis, "2026-10-29") {
		t.Fatal("event time", f, args, basis)
	}
	for _, role := range []string{"requester", "technician", "admin"} {
		where, _, err := analysisBase(User{ID: 7, OrgID: 2, Role: role}, OrderQuery{Analysis: "overview", Period: "today"}, now)
		if err != nil || strings.Contains(where, "created_at") || !strings.Contains(where, "org_id=?") {
			t.Fatal("base scope", where, err)
		}
		if role == "requester" && !strings.Contains(where, "requester_id=?") {
			t.Fatal("requester scope")
		}
		if role == "technician" && !strings.Contains(where, "assignee_id=?") {
			t.Fatal("tech scope")
		}
	}
}
func TestEquivalentAnalysisDatesStayStrict(t *testing.T) {
	now := time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)
	good := queryPlan{Name: MCPQueryOrders, Arguments: json.RawMessage(`{"analysis":"frequent","period":"recent","date_from":"2026-10-24","date_to":"2026-10-30"}`)}
	normalized := normalizeEquivalentAnalysisDates(good, now)
	if !validMCPArguments(normalized.Arguments, MCPQueryOrders) || strings.Contains(string(normalized.Arguments), `"recent"`) {
		t.Fatal("equivalent window not normalized")
	}
	for _, raw := range []string{
		`{"analysis":"frequent","period":"recent","date_from":"2026-10-01","date_to":"2026-10-07"}`,
		`{"analysis":"frequent","period":"recent","date_from":"2026-10-24","date_to":"2026-10-30","org_id":"2"}`,
		`{"analysis":"frequent","analysis":"frequent","period":"recent","date_from":"2026-10-24","date_to":"2026-10-30"}`,
	} {
		p := normalizeEquivalentAnalysisDates(queryPlan{Name: MCPQueryOrders, Arguments: json.RawMessage(raw)}, now)
		if validMCPArguments(p.Arguments, MCPQueryOrders) {
			t.Fatal("strict guard weakened", raw)
		}
	}
}

func TestExplicitAnalysisLocationSelection(t *testing.T) {
	for _, tt := range []struct {
		names []string
		want  string
		bad   bool
	}{{nil, "", false}, {[]string{"研发楼一楼201", "研发楼"}, "研发楼一楼201", false}, {[]string{"研发楼一楼201", "图书馆"}, "", true}} {
		got, err := chooseAnalysisLocation(tt.names)
		if got != tt.want || (err != nil) != tt.bad {
			t.Fatal("location scope", got, err)
		}
	}
}
