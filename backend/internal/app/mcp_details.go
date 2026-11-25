package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MCPOrderDetail = "get_work_order_detail"
	MCPOrderVisit  = "get_work_order_visit"
	MCPKnowledge   = "search_repair_knowledge"
)

type DetailInput struct {
	Number         string `json:"number"`
	IncludeHistory bool   `json:"include_history,omitempty"`
	Cursor         string `json:"cursor,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

type VisitInput struct {
	Number string `json:"number"`
}
type KnowledgeInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type OrderStep struct {
	Reason          string   `json:"reason"`
	ResponsibleRole string   `json:"responsible_role"`
	Actions         []string `json:"actions"`
	EvidenceID      string   `json:"evidence_id"`
}

type OrderEvent struct {
	ID     int64     `json:"id"`
	Action string    `json:"action"`
	From   string    `json:"from_status"`
	To     string    `json:"to_status"`
	Note   string    `json:"note"`
	Actor  string    `json:"actor_name"`
	At     time.Time `json:"created_at"`
}

type OrderRepair struct {
	ID       int64     `json:"id"`
	Measures string    `json:"measures"`
	Result   string    `json:"result"`
	At       time.Time `json:"created_at"`
}

type OrderDetail struct {
	Order        BusinessOrder `json:"order"`
	Version      int64         `json:"version"`
	UpdatedAt    time.Time     `json:"updated_at"`
	Next         OrderStep     `json:"next_step"`
	LatestRepair *OrderRepair  `json:"latest_repair,omitempty"`
	Events       []OrderEvent  `json:"events"`
	NextCursor   string        `json:"next_cursor,omitempty"`
	HasMore      bool          `json:"has_more"`
	Visit        *VisitContext `json:"visit,omitempty"`
	LatestVisit  *Visit        `json:"latest_visit,omitempty"`
}

func mcpObjectFields(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, false
		}
		key, ok := token.(string)
		if !ok {
			return nil, false
		}
		if _, exists := fields[key]; exists {
			return nil, false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, false
		}
		fields[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	_, err = decoder.Token()
	return fields, err == io.EOF
}

func validExtendedMCPArguments(raw json.RawMessage, name string) bool {
	fields, ok := mcpObjectFields(raw)
	if !ok {
		return false
	}
	switch name {
	case MCPOrderDetail:
		var input DetailInput
		if json.Unmarshal(raw, &input) != nil || !orderNumberPattern.MatchString(input.Number) || orderNumberPattern.FindString(input.Number) != input.Number || input.Limit < 0 || input.Limit > 50 || (!input.IncludeHistory && input.Cursor != "") {
			return false
		}
		for key := range fields {
			if key != "number" && key != "include_history" && key != "cursor" && key != "limit" {
				return false
			}
		}
		return len(input.Cursor) <= 200
	case MCPOrderVisit:
		var input VisitInput
		return len(fields) == 1 && json.Unmarshal(raw, &input) == nil && input.Number != "" && orderNumberPattern.FindString(input.Number) == input.Number
	case MCPKnowledge:
		var input KnowledgeInput
		if json.Unmarshal(raw, &input) != nil || strings.TrimSpace(input.Query) == "" || utf8.RuneCountInString(input.Query) > 500 || input.Limit < 0 || input.Limit > 10 {
			return false
		}
		for key := range fields {
			if key != "query" && key != "limit" {
				return false
			}
		}
		return true
	}
	return false
}

// All action codes lead to existing pages. Actual writes still validate current
// state/version/ownership through the normal business handlers.
func orderNextStep(o Order, u User, v VisitContext, now time.Time) OrderStep {
	step := OrderStep{Actions: []string{}, EvidenceID: fmt.Sprintf("order:%d:v%d", o.ID, o.Version)}
	switch o.Status {
	case "submitted":
		step.ResponsibleRole = "admin"
		step.Reason = "等待管理员审核"
	case "needs_info":
		step.ResponsibleRole = "requester"
		step.Reason = "等待报修人补充信息"
	case "unassigned":
		step.ResponsibleRole = "admin"
		step.Reason = "等待管理员安排维修人员"
	case "assigned":
		step.ResponsibleRole = "technician"
		step.Reason = "等待当前维修人员接单"
	case "in_progress":
		step.ResponsibleRole = "technician"
		step.Reason = "由当前维修人员处理"
	case "awaiting_confirmation":
		step.ResponsibleRole = "requester"
		step.Reason = "等待报修人确认维修结果"
	default:
		step.Reason = "工单已结束"
		return step
	}
	if o.Status == "in_progress" {
		if v.Exception == "waiting_parts" {
			step.Reason = "等待备件，需当前维修人员记录恢复情况"
		}
		if v.Exception != "waiting_parts" {
			switch {
			case v.NeedsAvailability || v.Exception == "no_show" && (v.Start == nil || v.End == nil || !v.End.After(now.Add(30*time.Minute))):
				step.ResponsibleRole = "requester"
				step.Reason = "需要报修人重新提供方便上门的时间"
			case v.Exception == "no_show" && v.Visit == nil:
				step.Reason = "等待当前维修人员确认新的上门预约"
			case v.Visit != nil && (v.Visit.Stage == "scheduled" || v.Visit.Stage == "departed") && now.After(v.Visit.End):
				step.Reason = "预约时间已过，需要当前维修人员核对上门情况"
			case v.Visit != nil && v.Visit.Stage == "scheduled":
				step.Reason = "已确认上门预约，等待当前维修人员出发"
			case v.Visit != nil && v.Visit.Stage == "departed":
				step.Reason = "维修人员已记录出发，等待到场"
			case v.Visit != nil && v.Visit.Stage == "arrived":
				step.Reason = "维修人员已到场处理"
			}
		}
	}
	if step.ResponsibleRole == u.Role {
		step.Actions = append(step.Actions, "order_detail")
	}
	for _, action := range visitActions(o, u, v, now) {
		if action == "availability" && (v.Start == nil || v.NeedsAvailability) {
			step.Actions = append(step.Actions, "visit_availability")
		}
		if action == "resume" || action == "schedule" || action == "depart" || action == "arrive" {
			step.Actions = append(step.Actions, "order_visit")
			break
		}
	}
	return step
}

func queryOrderDetail(ctx context.Context, db *sql.DB, u User, input DetailInput, withVisit bool) (*OrderDetail, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	scope, args := orderFilter(u)
	o, err := scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM work_orders WHERE "+scope+" AND number=? FOR SHARE", append(args, input.Number)...))
	if err != nil {
		return nil, dbError(err)
	}
	visit, err := readVisit(ctx, tx, o.ID)
	if err != nil {
		return nil, err
	}
	detail := &OrderDetail{Order: BusinessOrder{ID: o.ID, Number: o.Number, Status: o.Status, Category: o.Category, Location: o.Location, Description: o.Description}, Version: o.Version, UpdatedAt: o.UpdatedAt, Next: orderNextStep(o, u, visit, time.Now()), Events: []OrderEvent{}}
	if withVisit {
		detail.Visit = &visit
		latest := new(Visit)
		e := tx.QueryRowContext(ctx, "SELECT id,technician_id,starts_at,ends_at,stage,departed_at,arrived_at,no_show_at,end_reason FROM work_order_visits WHERE work_order_id=? ORDER BY id DESC LIMIT 1", o.ID).Scan(&latest.ID, &latest.TechnicianID, &latest.Start, &latest.End, &latest.Stage, &latest.Departed, &latest.Arrived, &latest.NoShow, &latest.EndReason)
		if e != nil && e != sql.ErrNoRows {
			return nil, e
		}
		if e == nil {
			detail.LatestVisit = latest
		}
	}
	repair := new(OrderRepair)
	err = tx.QueryRowContext(ctx, "SELECT id,measures,result,created_at FROM repair_records WHERE work_order_id=? ORDER BY id DESC LIMIT 1", o.ID).Scan(&repair.ID, &repair.Measures, &repair.Result, &repair.At)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		detail.LatestRepair = repair
	}
	if input.IncludeHistory {
		limit := input.Limit
		if limit == 0 {
			limit = 20
		}
		var upper, last int64
		if input.Cursor == "" {
			if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM work_order_events WHERE work_order_id=?", o.ID).Scan(&upper); err != nil {
				return nil, err
			}
			last = upper + 1
		} else {
			raw, e := base64.RawURLEncoding.DecodeString(input.Cursor)
			if e != nil {
				return nil, bad("时间线游标无效")
			}
			parts := strings.Split(string(raw), "/")
			if len(parts) != 3 || parts[0] != strconv.FormatInt(o.ID, 10) {
				return nil, bad("时间线游标无效")
			}
			upper, e = strconv.ParseInt(parts[1], 10, 64)
			if e != nil || upper < 0 {
				return nil, bad("时间线游标无效")
			}
			last, e = strconv.ParseInt(parts[2], 10, 64)
			if e != nil || last < 1 || last > upper {
				return nil, bad("时间线游标无效")
			}
		}
		rows, e := tx.QueryContext(ctx, "SELECT e.id,e.action,e.from_status,e.to_status,e.note,u.display_name,e.created_at FROM work_order_events e JOIN users u ON u.id=e.actor_id AND u.org_id=? WHERE e.work_order_id=? AND e.id<=? AND e.id<? ORDER BY e.id DESC LIMIT ?", u.OrgID, o.ID, upper, last, limit+1)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var event OrderEvent
			if e = rows.Scan(&event.ID, &event.Action, &event.From, &event.To, &event.Note, &event.Actor, &event.At); e != nil {
				rows.Close()
				return nil, e
			}
			detail.Events = append(detail.Events, event)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		if len(detail.Events) > limit {
			detail.Events = detail.Events[:limit]
			detail.HasMore = true
			last = detail.Events[len(detail.Events)-1].ID
			detail.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d/%d/%d", o.ID, upper, last)))
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return detail, nil
}

func extendedMCPQuery(ctx context.Context, db *sql.DB, u User, claim mcpClaims, name string, raw json.RawMessage) (map[string]any, error) {
	if name == MCPKnowledge {
		var input KnowledgeInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return nil, err
		}
		limit := input.Limit
		if limit == 0 {
			limit = 5
		}
		retrievalStarted := time.Now()
		knowledge := (&Server{DB: db}).searchAssistantKnowledge(ctx, u, claim.AgentID, input.Query, limit)
		if knowledge.State == "unavailable" {
			return nil, errors.New("knowledge unavailable")
		}
		return map[string]any{"retrieval_duration_ms": time.Since(retrievalStarted).Milliseconds(), "kind": "knowledge", "items": knowledge.Hits, "mode": knowledge.Mode, "state": knowledge.State, "notice": knowledge.Notice, "queried_at": time.Now().UTC()}, nil
	}
	var input DetailInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, err
	}
	detail, err := queryOrderDetail(ctx, db, u, input, name == MCPOrderVisit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"kind": map[bool]string{true: "visit", false: "detail"}[name == MCPOrderVisit], "detail": detail, "queried_at": time.Now().UTC()}, nil
}

func formatOrderDetail(input string, detail *OrderDetail) string {
	if containsAny(input, "原因", "为什么", "怎么还", "为何") && detail.LatestRepair == nil && (detail.Visit == nil || detail.Visit.Reason == "") {
		return "目前没有已记录的延期原因。" + detail.Next.Reason + "。"
	}
	if detail.Visit != nil {
		return detail.Next.Reason + "。上门时间和记录见下方卡片。"
	}
	return "这张工单当前为" + businessStatusNames[detail.Order.Status] + "，" + detail.Next.Reason + "。"
}
