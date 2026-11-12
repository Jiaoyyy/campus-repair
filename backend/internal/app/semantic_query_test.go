package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestGroundQueryTimeScope(t *testing.T) {
	previous := &BusinessResult{Kind: "orders", Query: OrderQuery{Location: "研发楼一楼", Period: "recent"}}
	for _, tt := range []struct {
		input    string
		previous *BusinessResult
		period   string
	}{
		{"研发楼有什么问题", nil, "all"},
		{"研发楼一楼怎么了", nil, "all"},
		{"3号楼有什么问题", nil, "all"},
		{"研发楼最近有什么故障", nil, "recent"},
		{"研发楼有什么问题", previous, "all"},
		{"那研发楼有哪些问题", previous, "recent"},
		{"只看漏水", previous, "recent"},
		{"不限日期的未结束工单", previous, "all"},
	} {
		q := groundQueryDates(OrderQuery{Location: "研发楼", Period: "recent", Keyword: "问题"}, tt.input, tt.previous)
		if q.Period != tt.period || q.Keyword != "" {
			t.Fatalf("%s => %+v", tt.input, q)
		}
	}
	q := groundQueryDates(OrderQuery{Location: "研发楼", DateFrom: "2026-10-01", DateTo: "2026-10-07"}, "研发楼有什么问题", nil)
	if q.Period != "all" || q.DateFrom != "" || q.DateTo != "" {
		t.Fatal("invented dates retained")
	}
	previous.Query = OrderQuery{DateFrom: "2026-10-01", DateTo: "2026-10-07"}
	q = groundQueryDates(OrderQuery{Period: "recent"}, "那照明的呢", previous)
	if q.DateFrom != previous.Query.DateFrom || q.DateTo != previous.Query.DateTo || q.Period != "" {
		t.Fatal("followup changed actual date range")
	}
	q = groundQueryDates(OrderQuery{Keyword: "问题", Period: "today"}, "查今天故障描述包含问题的工单", nil)
	if q.Keyword != "问题" || q.Period != "today" {
		t.Fatal("explicit literal keyword/date lost")
	}
	for _, input := range []string{"研发楼10/21有什么故障", "研发楼二十一号有什么故障", "研发楼过去三天有什么故障", "研发楼近3天有什么故障", "研发楼周一有什么故障"} {
		q := groundQueryDates(OrderQuery{DateFrom: "2026-10-21", DateTo: "2026-10-21"}, input, nil)
		if q.DateFrom != "2026-10-21" || q.DateTo != "2026-10-21" {
			t.Fatalf("explicit date expression cleared: %s", input)
		}
	}
}

func TestDateLimitedEmptyResultExplainsScope(t *testing.T) {
	b := BusinessResult{Kind: "orders", Query: OrderQuery{Location: "研发楼", Period: "recent"}, TimeBasis: "创建时间（北京时间）：2026-10-22 至 2026-10-28"}
	text := formatBusinessResult("admin", b)
	if !strings.Contains(text, b.TimeBasis) || !strings.Contains(text, "时间范围外仍可能有未结束工单") {
		t.Fatal("recent empty result hid old unresolved orders")
	}
	b.Query.Period, b.TimeBasis = "all", ""
	if !strings.Contains(formatBusinessResult("admin", b), "不限日期") {
		t.Fatal("undated scope not displayed")
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

func TestQueryGatewayTransportFailures(t *testing.T) {
	for _, mode := range []string{"refused", "tls", "headers-timeout", "stream-timeout"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				io.Copy(io.Discard, r.Body)
				if mode == "stream-timeout" {
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{}}]}\n\n")
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
			})
			var server *httptest.Server
			if mode == "tls" {
				server = httptest.NewUnstartedServer(handler)
				server.Config.ErrorLog = log.New(io.Discard, "", 0)
				server.StartTLS()
			} else {
				server = httptest.NewServer(handler)
			}
			defer server.Close()
			if mode == "refused" {
				server.Close()
			}
			g := ChatGateway{URL: server.URL + "/private-path", Key: "private-fixture-key", Model: "fixture", Timeout: 100 * time.Millisecond}
			_, err := g.planQuery(context.Background(), "admin", nil, "fixture", nil, nil)
			var failure *queryGatewayError
			if !errors.As(err, &failure) || !errors.Is(err, errChat) {
				t.Fatalf("transport error was classified as plan validation: %v", err)
			}
			want := "connection"
			if strings.HasSuffix(mode, "timeout") {
				want = "timeout"
			}
			if failure.Transport != want || failure.Status != 0 || failure.Quota {
				t.Fatalf("wrong classification: %+v", failure)
			}
			if strings.Contains(err.Error(), server.URL) || strings.Contains(err.Error(), "private") {
				t.Fatal("transport diagnostic leaked private URL or credential")
			}
			wantCalls := int32(0)
			if strings.HasSuffix(mode, "timeout") {
				wantCalls = 1
			}
			if calls.Load() != wantCalls {
				t.Fatalf("unexpected request count/retry: %d", calls.Load())
			}
		})
	}
}

func TestQueryTransportDiagnosticRedaction(t *testing.T) {
	for _, err := range []error{
		&net.DNSError{Err: "private DNS diagnostic", Name: "private-gateway.invalid"},
		&net.OpError{Op: "dial", Net: "tcp", Err: errors.New("private connection diagnostic")},
		context.DeadlineExceeded,
	} {
		failure := queryTransportFailure(err)
		if strings.Contains(failure.Error(), "private") || failure.Status != 0 {
			t.Fatal("raw network diagnostic was retained")
		}
	}
}
