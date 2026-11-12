package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
			if q.Analysis != tt.mode || q.Keyword != "" || q.Period != "recent" {
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
func TestInsightNotesEvidenceAndClaims(t *testing.T) {
	facts := []InsightFact{{ID: "category:top", Count: 8}, {ID: "parts", Count: 0}}
	good := `{"notes":[{"evidence":["category:top"],"recommendation":"建议核对集中报修位置，结合现场巡检确定处理安排。"}]}`
	if _, err := parseInsightNotes(good, facts); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		strings.ReplaceAll(good, "category:top", "other-org"), strings.ReplaceAll(good, "category:top", "parts"),
		strings.ReplaceAll(good, "核对集中报修位置", "安排8单"), strings.ReplaceAll(good, "核对集中报修位置", "已有三次白跑"), strings.ReplaceAll(good, "核对集中报修位置", "已经通知维修人员"), strings.ReplaceAll(good, "核对集中报修位置", "根因是电线老化"), strings.ReplaceAll(good, "核对集中报修位置", "拆开配电箱"),
		good + " trailing", `{"notes":[],"org_id":2}`, `{"notes":[{"evidence":["category:top"],"recommendation":"建议核对","assign":true}]}`,
	} {
		if _, err := parseInsightNotes(raw, facts); err == nil {
			t.Fatal("invalid notes accepted", raw)
		}
	}
}
func TestInsightSynthesisNoRetryAndCancel(t *testing.T) {
	for _, mode := range []string{"valid", "invalid", "failed", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if mode == "cancel" {
					select {
					case <-r.Context().Done():
					case <-time.After(300 * time.Millisecond):
					}
					return
				}
				if mode == "failed" {
					w.WriteHeader(503)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				answer := `{"notes":[{"evidence":["aged"],"recommendation":"建议核对积压工单的当前阻塞与负责方。"}]}`
				if mode == "invalid" {
					answer = `{"notes":[{"evidence":["forged"],"recommendation":"建议核对"}]}`
				}
				raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": answer}, "finish_reason": "stop"}}})
				fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			g := ChatGateway{URL: srv.URL, Model: "fixture", Key: "fixture", Timeout: time.Second}
			_, err := g.analyzeBusiness(ctx, "总结问题", BusinessResult{Analysis: &BusinessAnalysis{Facts: []InsightFact{{ID: "aged", Count: 5}}}})
			if (err == nil) != (mode == "valid") {
				t.Fatal(mode, err)
			}
			if calls.Load() != 1 {
				t.Fatal("retried")
			}
		})
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

func TestInsightNumeralsInVerifiedNames(t *testing.T) {
	facts := []InsightFact{{ID: "location:top", Title: "报修最多的准确位置", Count: 4}, {ID: "aged", Title: "未结束超过七天", Count: 3}}
	raw := `{"notes":[{"evidence":["location:top","aged"],"recommendation":"建议核对3号楼201和未结束超过七天的工单，并跟进负责方。"}]}`
	if _, err := parseInsightNotes(raw, facts, []InsightGroup{{Label: "3号楼201", Count: 4}}); err != nil {
		t.Fatal("verified name rejected", err)
	}
	for _, bad := range []string{strings.ReplaceAll(raw, "3号楼201", "4号楼202"), strings.ReplaceAll(raw, "跟进负责方", "处理999单")} {
		if _, err := parseInsightNotes(bad, facts, []InsightGroup{{Label: "3号楼201", Count: 4}}); err == nil {
			t.Fatal("invented numeral allowed")
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
