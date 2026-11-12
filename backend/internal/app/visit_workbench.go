package app

import (
	"database/sql"
	"net/http"
	"time"
)

var businessTZ = time.FixedZone("Asia/Shanghai", 8*60*60)

func visitQueue(role, view string, now time.Time) (string, []any, error) {
	active := "EXISTS (SELECT 1 FROM work_order_visit_context c JOIN work_order_visits v ON v.id=c.active_visit_id WHERE c.work_order_id=work_orders.id"
	exception := func(name string) string {
		return "EXISTS (SELECT 1 FROM work_order_visit_context c WHERE c.work_order_id=work_orders.id AND c.exception='" + name + "')"
	}
	if role == "technician" {
		switch view {
		case "pending":
			return "status='assigned'", nil, nil
		case "unscheduled":
			return "status='in_progress' AND NOT " + active + ") AND NOT " + exception("waiting_parts"), nil, nil
		case "today":
			day := now.In(businessTZ)
			start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, businessTZ).UTC()
			return active + " AND v.starts_at>=? AND v.starts_at<?)", []any{start, start.Add(24 * time.Hour)}, nil
		case "processing":
			return "status='in_progress'", nil, nil
		}
	}
	if role == "admin" {
		switch view {
		case "overdue":
			return active + " AND v.stage IN ('scheduled','departed') AND v.ends_at<?)", []any{now}, nil
		case "reschedule":
			return "status='in_progress' AND " + exception("no_show") + " AND NOT " + active + ")", nil, nil
		case "parts":
			return "status='in_progress' AND " + exception("waiting_parts"), nil, nil
		}
	}
	return "", nil, bad("工作台队列无效")
}
func (s *Server) visitWorkbench(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	views := []string{"pending", "unscheduled", "today", "processing"}
	if u.Role == "admin" {
		views = []string{"overdue", "reschedule", "parts"}
	} else if u.Role != "technician" {
		s.fail(w, r, denied())
		return
	}
	view := r.URL.Query().Get("view")
	if view == "" {
		view = views[0]
	}
	now := time.Now().UTC()
	filter, values, err := visitQueue(u.Role, view, now)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	page, size, err := pagination(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	q, args := orderFilter(u)
	counts := map[string]int{}
	for _, key := range views {
		f, v, _ := visitQueue(u.Role, key, now)
		a := append(append([]any{}, args...), v...)
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM work_orders WHERE "+q+" AND "+f, a...).Scan(&n); err != nil {
			s.fail(w, r, err)
			return
		}
		counts[key] = n
	}
	order := "updated_at DESC,id DESC"
	if view == "today" {
		order = "(SELECT starts_at FROM work_order_visits WHERE id=(SELECT active_visit_id FROM work_order_visit_context WHERE work_order_id=work_orders.id)),id"
	}
	if view == "overdue" {
		order = "(SELECT ends_at FROM work_order_visits WHERE id=(SELECT active_visit_id FROM work_order_visit_context WHERE work_order_id=work_orders.id)),id"
	}
	a := append(append([]any{}, args...), values...)
	a = append(a, size, (page-1)*size)
	rows, err := tx.QueryContext(ctx, "SELECT "+orderColumns+" FROM work_orders WHERE "+q+" AND "+filter+" ORDER BY "+order+" LIMIT ? OFFSET ?", a...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	orders := []Order{}
	for rows.Next() {
		o, e := scanOrder(rows)
		if e != nil {
			rows.Close()
			s.fail(w, r, e)
			return
		}
		orders = append(orders, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	type item struct {
		Order   Order        `json:"order"`
		Context VisitContext `json:"context"`
		Actions []string     `json:"allowed_actions"`
	}
	items := []item{}
	for _, o := range orders {
		v, e := readVisit(ctx, tx, o.ID)
		if e != nil {
			s.fail(w, r, e)
			return
		}
		items = append(items, item{o, v, visitActions(o, u, v, now)})
	}
	if err = tx.Commit(); err != nil {
		s.fail(w, r, err)
		return
	}
	for i := range items {
		s.enrich(ctx, &items[i].Order)
	}
	send(w, 200, map[string]any{"items": items, "counts": counts, "total": counts[view], "view": view, "page": page, "page_size": size, "as_of": now})
}
func visitMetricRange(from, to string, now time.Time) (time.Time, time.Time, error) {
	day := now.In(businessTZ)
	end := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, businessTZ)
	start := end.AddDate(0, 0, -6)
	if from != "" || to != "" {
		var err error
		start, err = time.ParseInLocation("2006-01-02", from, businessTZ)
		if err != nil {
			return start, end, bad("请提供有效的起止日期")
		}
		end, err = time.ParseInLocation("2006-01-02", to, businessTZ)
		if err != nil {
			return start, end, bad("请提供有效的起止日期")
		}
	}
	if end.Before(start) || end.Sub(start) > 365*24*time.Hour {
		return start, end, bad("日期范围须为1至366天")
	}
	return start.UTC(), end.AddDate(0, 0, 1).UTC(), nil
}
func (s *Server) visitMetrics(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	if u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	now := time.Now().UTC()
	start, end, err := visitMetricRange(r.URL.Query().Get("date_from"), r.URL.Query().Get("date_to"), now)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	counts := map[string]int{}
	for _, key := range []string{"overdue", "parts"} {
		f, a, _ := visitQueue("admin", key, now)
		args := append([]any{u.OrgID}, a...)
		var n int
		err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM work_orders WHERE org_id=? AND "+f, args...).Scan(&n)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		counts[key] = n
	}
	var n int
	err = tx.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM work_order_visits v JOIN work_orders o ON o.id=v.work_order_id WHERE o.org_id=? AND v.no_show_at>=? AND v.no_show_at<?", u.OrgID, start, end).Scan(&n)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	counts["no_show"] = n
	if err = tx.Commit(); err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"counts": counts, "date_from": start.In(businessTZ).Format("2006-01-02"), "date_to": end.Add(-time.Nanosecond).In(businessTZ).Format("2006-01-02"), "as_of": now})
}
