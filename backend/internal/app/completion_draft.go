package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

const completionDraftKind = "prepare_completion"

type completionDraft struct {
	Measures string `json:"measures"`
	Result   string `json:"result"`
}

func incompleteCompletion(previous *BusinessResult) bool {
	return previous != nil && previous.Kind == completionDraftKind && previous.Completion != nil &&
		(previous.Completion.Measures == "" || previous.Completion.Result == "")
}

func (s *Server) prepareCompletion(ctx context.Context, u User, input string, raw json.RawMessage, previous *BusinessResult) (BusinessResult, string, error) {
	fields, valid := mcpStringFields(raw)
	if u.Role != "technician" || !valid {
		return BusinessResult{}, "", denied()
	}
	q := OrderQuery{Status: "in_progress", Period: "all", Queue: "all"}
	draft := completionDraft{}
	if (fields["resume"] == "yes" || incompleteCompletion(previous)) && previous != nil && previous.Kind == completionDraftKind && previous.Completion != nil {
		q = previous.Query
		draft = *previous.Completion
	}
	q, err := s.draftOrderScope(ctx, u, input, fields["location"], q)
	if err != nil {
		return BusinessResult{}, "", err
	}
	if previous != nil && previous.Kind == completionDraftKind && !sameDraftTarget(previous, q) {
		draft = completionDraft{}
	}

	for _, field := range []struct {
		value  string
		target *string
	}{
		{fields["measures"], &draft.Measures}, {fields["result"], &draft.Result},
	} {
		value := strings.TrimSpace(field.value)
		if value != "" && strings.Contains(input, value) {
			*field.target = value
		}
	}
	b, err := queryBusinessOrders(ctx, s.DB, u, q)
	if err != nil {
		return b, "", err
	}
	b.Kind = completionDraftKind
	b.Completion = &draft
	if b.Total == 0 {
		return b, "没找到符合条件的处理中工单。请核对位置或工单编号。", nil
	}
	if b.Total > 5 {
		return b, "匹配到多张处理中工单。请补充具体位置或工单编号。", nil
	}
	missing := []string{}
	if draft.Measures == "" {
		missing = append(missing, "具体做了哪些检查或维修")
	}
	if draft.Result == "" {
		missing = append(missing, "处理后现在情况如何")
	}
	if len(missing) > 0 {
		return b, "请补充" + strings.Join(missing, "，以及") + "。", nil
	}
	if b.Total > 1 {
		return b, "请在下方选择这次维修对应的工单，再核对上报内容。", nil
	}
	return b, "已整理完工草稿。请打开工单核对并提交。", nil
}

func (s *Server) draftOrderScope(ctx context.Context, u User, input, fragment string, q OrderQuery) (OrderQuery, error) {
	if number := orderNumberPattern.FindString(input); number != "" {
		q.Number, q.Location = number, ""
	} else {
		// Prefer an exact stored location present in the user’s message over the model’s fragment.
		where, args := orderFilter(u)
		var location string
		stateFilter := " AND status=?"
		args = append(args, q.Status)
		if u.Role == "requester" {
			stateFilter = " AND status IN (?, 'closed')"
		}
		err := s.DB.QueryRowContext(ctx, "SELECT location FROM work_orders WHERE "+where+stateFilter+" AND LOCATE(location,?)>0 ORDER BY CHAR_LENGTH(location) DESC,id DESC LIMIT 1", append(args, input)...).Scan(&location)
		if err != nil && err != sql.ErrNoRows {
			return q, err
		}
		if location == "" && strings.Contains(input, fragment) {
			location = fragment
		}
		if location != "" {
			q.Location, q.Number = location, ""
		}
	}
	return q, nil
}

func sameDraftTarget(previous *BusinessResult, q OrderQuery) bool {
	if previous.Query.Number == "" && previous.Query.Location == "" || q.Number == previous.Query.Number && q.Location == previous.Query.Location {
		return true
	}
	for _, item := range previous.Items {
		if item.Version > 0 && (q.Number != "" && strings.EqualFold(q.Number, item.Number) || q.Location != "" && strings.Contains(item.Location, q.Location)) {
			return true
		}
	}
	return false
}
