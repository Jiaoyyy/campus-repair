package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
)

const requesterFollowupKind = "prepare_requester_followup"

type requesterFollowup struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

func latestRepairID(ctx context.Context, q visitQuerier, orderID int64) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, "SELECT id FROM repair_records WHERE work_order_id=? ORDER BY id DESC LIMIT 1", orderID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return id, err
}

func recurrenceExists(ctx context.Context, q visitQuerier, recordID int64) (bool, error) {
	var exists bool
	err := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM work_order_recurrences WHERE source_record_id=?)", recordID).Scan(&exists)
	return exists, err
}

func (s *Server) prepareRequesterFollowup(ctx context.Context, u User, input string, raw json.RawMessage, previous *BusinessResult) (BusinessResult, string, error) {
	fields, ok := mcpStringFields(raw)
	if u.Role != "requester" || !ok {
		return BusinessResult{}, "", denied()
	}
	draft := requesterFollowup{Decision: fields["decision"]}
	q := OrderQuery{Queue: "all", Period: "all", Status: "awaiting_confirmation"}
	if previous != nil && previous.Kind == requesterFollowupKind && previous.Followup != nil &&
		previous.Followup.Decision == draft.Decision && (fields["resume"] == "yes" || draft.Decision == "recurrence" && previous.Followup.Reason == "") {
		q = previous.Query
		draft = *previous.Followup
	}
	q, err := s.draftOrderScope(ctx, u, input, fields["location"], q)
	if err != nil {
		return BusinessResult{}, "", err
	}
	if previous != nil && previous.Kind == requesterFollowupKind && !sameDraftTarget(previous, q) {
		draft.Reason = ""
	}
	if reason := strings.TrimSpace(fields["reason"]); draft.Decision == "recurrence" && reason != "" && strings.Contains(input, reason) {
		draft.Reason = reason
	}
	b, err := queryBusinessOrders(ctx, s.DB, u, q)
	if err != nil {
		return b, "", err
	}
	if draft.Decision == "recurrence" {
		closed := q
		closed.Status = "closed"
		extra, e := queryBusinessOrders(ctx, s.DB, u, closed)
		if e != nil {
			return b, "", e
		}
		b.Total += extra.Total
		b.Statuses["closed"] = extra.Total
		b.Items = append(b.Items, extra.Items...)
	}
	b.Kind = requesterFollowupKind
	b.Followup = &draft
	if b.Total == 0 {
		return b, "没有找到符合条件的本人报修。请核对位置或工单编号。", nil
	}
	if b.Total > 5 {
		b.Items = []BusinessOrder{}
		return b, "匹配到多条报修，请补充具体位置或工单编号。", nil
	}
	sort.SliceStable(b.Items, func(i, j int) bool { return b.Items[i].ID > b.Items[j].ID })
	for i := range b.Items {
		item := &b.Items[i]
		var record OrderRepair
		e := s.DB.QueryRowContext(ctx, "SELECT id,measures,result,created_at FROM repair_records WHERE work_order_id=? ORDER BY id DESC LIMIT 1", item.ID).Scan(&record.ID, &record.Measures, &record.Result, &record.At)
		if e == nil {
			item.LatestRepair = &record
			if item.Status == "closed" {
				item.RecurrenceExists, e = recurrenceExists(ctx, s.DB, record.ID)
				if e != nil {
					return b, "", e
				}
			}
		}
		if e != nil && e != sql.ErrNoRows {
			return b, "", e
		}
	}
	if draft.Decision == "recurrence" && draft.Reason == "" {
		return b, "请描述故障是怎么复发的，或具体还有什么现象。", nil
	}
	if b.Total > 1 {
		return b, "请先选出你说的那张工单，核对最近一次维修结果。", nil
	}
	if draft.Decision == "recurrence" {
		return b, "请核对原工单和复发情况，再进入对应表单提交反馈。", nil
	}
	return b, "请核对最近一次维修结果，再确认是否解决。", nil
}
