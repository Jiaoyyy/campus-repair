package app

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const partsDraftKind = "prepare_wait_parts"

type partsDraft struct {
	Reason   string     `json:"reason"`
	ResumeAt *time.Time `json:"resume_at,omitempty"`
}

var partsResumePattern = regexp.MustCompile(`^(今天|明天|后天|[0-9]{4}[-/年][0-9]{1,2}[-/月][0-9]{1,2}日?)\s*(凌晨|早上|上午|中午|下午|晚上)?\s*([0-9]{1,2}|[一二两三四五六七八九十]{1,3})(?:[:：]([0-9]{2})|[点时](半|[0-9]{1,2}分?)?)$`)
var partsDateSeparator = regexp.MustCompile(`[-/年月日]+`)

func partsResumeTime(text string, now time.Time) (time.Time, bool) {
	match := partsResumePattern.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return time.Time{}, false
	}
	current := now.In(businessTZ)
	y, m, d := current.Date()
	if offset, relative := map[string]int{"今天": 0, "明天": 1, "后天": 2}[match[1]]; relative {
		day := current.AddDate(0, 0, offset)
		y, m, d = day.Date()
	} else {
		date := strings.FieldsFunc(match[1], func(r rune) bool { return partsDateSeparator.MatchString(string(r)) })
		if len(date) != 3 {
			return time.Time{}, false
		}
		year, _ := strconv.Atoi(date[0])
		month, _ := strconv.Atoi(date[1])
		day, _ := strconv.Atoi(date[2])
		y, m, d = year, time.Month(month), day
	}
	hour, err := strconv.Atoi(match[3])
	if err != nil {
		numbers := map[string]int{"一": 1, "二": 2, "两": 2, "三": 3, "四": 4, "五": 5, "六": 6, "七": 7, "八": 8, "九": 9}
		if before, after, tens := strings.Cut(match[3], "十"); tens {
			hour = 10
			if before != "" {
				n, ok := numbers[before]
				if !ok {
					return time.Time{}, false
				}
				hour = n * 10
			}
			if after != "" {
				n, ok := numbers[after]
				if !ok {
					return time.Time{}, false
				}
				hour += n
			}
		} else {
			n, ok := numbers[match[3]]
			if !ok {
				return time.Time{}, false
			}
			hour = n
		}
	}
	switch match[2] {
	case "下午", "晚上":
		if hour >= 1 && hour <= 11 {
			hour += 12
		}
	case "中午":
		if hour == 1 {
			hour = 13
		} else if hour != 12 {
			return time.Time{}, false
		}
	case "上午", "早上":
		if hour >= 12 {
			return time.Time{}, false
		}
	case "凌晨":
		if hour > 12 {
			return time.Time{}, false
		}
		if hour == 12 {
			hour = 0
		}
	}
	minute, _ := strconv.Atoi(match[4])
	if match[5] == "半" {
		minute = 30
	} else if match[5] != "" {
		minute, _ = strconv.Atoi(strings.TrimSuffix(match[5], "分"))
	}
	if hour > 23 || minute > 59 {
		return time.Time{}, false
	}
	value := time.Date(y, m, d, hour, minute, 0, 0, businessTZ)
	return value, value.Year() == y && value.Month() == m && value.Day() == d && value.After(now)
}

func (s *Server) prepareParts(ctx context.Context, u User, input string, raw json.RawMessage, previous *BusinessResult) (BusinessResult, string, error) {
	fields, ok := mcpStringFields(raw)
	if u.Role != "technician" || !ok {
		return BusinessResult{}, "", denied()
	}
	now := time.Now()
	q := OrderQuery{Status: "in_progress", Period: "all", Queue: "all"}
	draft := partsDraft{}
	if previous != nil && previous.Kind == partsDraftKind && previous.Parts != nil && (fields["resume"] == "yes" || previous.Parts.Reason == "" || previous.Parts.ResumeAt == nil) {
		q = previous.Query
		draft = *previous.Parts
	}
	q, err := s.draftOrderScope(ctx, u, input, fields["location"], q)
	if err != nil {
		return BusinessResult{}, "", err
	}
	if previous != nil && previous.Kind == partsDraftKind && !sameDraftTarget(previous, q) {
		draft = partsDraft{}
	}
	if reason := strings.TrimSpace(fields["reason"]); reason != "" && strings.Contains(input, reason) {
		draft.Reason = reason
	}
	if timeText := strings.TrimSpace(fields["time_text"]); timeText != "" && strings.Contains(input, timeText) {
		draft.ResumeAt = nil
		if parsed, valid := partsResumeTime(timeText, now); valid {
			draft.ResumeAt = &parsed
		}
	}
	if draft.ResumeAt != nil && !draft.ResumeAt.After(now) {
		draft.ResumeAt = nil
	}
	b, err := queryBusinessOrders(ctx, s.DB, u, q)
	if err != nil {
		return b, "", err
	}
	b.Kind = partsDraftKind
	b.Parts = &draft
	if b.Total <= 5 {
		eligible := b.Items[:0]
		for _, item := range b.Items {
			visit, e := readVisit(ctx, s.DB, item.ID)
			if e != nil {
				return b, "", e
			}
			if visit.Exception != "waiting_parts" {
				eligible = append(eligible, item)
			}
		}
		b.Items = eligible
		b.Total = len(eligible)
		b.Statuses["in_progress"] = b.Total
	}
	switch {
	case b.Total == 0:
		return b, "没找到可记录等待备件的工单。请核对位置、编号或工单状态。", nil
	case b.Total > 5:
		return b, "请补充具体位置或工单编号，方便定位要记录等待备件的工单。", nil
	case draft.Reason == "" && draft.ResumeAt == nil:
		return b, "请补充缺什么备件，以及预计哪天几点恢复处理（北京时间）。", nil
	case draft.Reason == "":
		return b, "请补充等待备件的原因，例如缺少哪种备件。", nil
	case draft.ResumeAt == nil:
		return b, "请补充未来的预计恢复时间，例如“明天下午三点”（北京时间）。", nil
	}
	return b, "已整理待备件说明。请选择对应工单，在上门安排里核对并保存。", nil
}
