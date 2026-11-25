package app

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var auditActionPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]*$`)
var auditOutcomePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func auditFilters(org int64, query url.Values) (string, []any, error) {
	where := "a.org_id=?"
	args := []any{org}
	for _, key := range []string{"actor_id", "resource_id"} {
		if value := query.Get(key); value != "" {
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id < 1 {
				return "", nil, bad("操作人和资源编号须为正整数")
			}
			where += " AND a." + key + "=?"
			args = append(args, id)
		}
	}
	if action := query.Get("action"); action != "" {
		name, prefix := strings.CutSuffix(action, ".*")
		if len(action) > 80 || !auditActionPattern.MatchString(name) {
			return "", nil, bad("操作筛选格式无效")
		}
		if prefix {
			where += " AND LOCATE(?,a.action)=1"
			args = append(args, name+".")
		} else {
			where += " AND a.action=?"
			args = append(args, name)
		}
	}
	if outcome := query.Get("outcome"); outcome != "" {
		if len(outcome) > 20 || !auditOutcomePattern.MatchString(outcome) {
			return "", nil, bad("结果筛选格式无效")
		}
		where += " AND a.outcome=?"
		args = append(args, outcome)
	}
	if request := strings.TrimSpace(query.Get("request_id")); request != "" {
		if len(request) > 64 {
			return "", nil, bad("请求标识过长")
		}
		where += " AND a.request_id=?"
		args = append(args, request)
	}
	var start, end time.Time
	for _, key := range []string{"date_from", "date_to"} {
		if value := query.Get(key); value != "" {
			date, err := time.ParseInLocation("2006-01-02", value, businessTZ)
			if err != nil || date.Format("2006-01-02") != value {
				return "", nil, bad("日期须为 YYYY-MM-DD")
			}
			if key == "date_from" {
				start = date
				where += " AND a.created_at>=?"
				args = append(args, start.UTC())
			} else {
				end = date
				where += " AND a.created_at<?"
				args = append(args, end.AddDate(0, 0, 1).UTC())
			}
		}
	}
	if !start.IsZero() && !end.IsZero() && end.Before(start) {
		return "", nil, bad("结束日期不能早于开始日期")
	}
	return where, args, nil
}
