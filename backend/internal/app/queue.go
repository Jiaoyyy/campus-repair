package app

import (
	"net/http"
	"strings"
)

func actionStates(role string) []string {
	switch role {
	case "requester":
		return []string{"needs_info", "awaiting_confirmation"}
	case "admin":
		return []string{"submitted", "unassigned"}
	case "technician":
		return []string{"assigned", "in_progress"}
	}
	return nil
}
func finished(status string) bool {
	return status == "closed" || status == "rejected" || status == "cancelled"
}
func actionable(role, status string) bool {
	for _, s := range actionStates(role) {
		if s == status {
			return true
		}
	}
	return false
}
func queueFilter(role, queue string) (string, []any, error) {
	switch queue {
	case "", "all":
		return "", nil, nil
	case "action":
		states := actionStates(role)
		if len(states) == 0 {
			return " AND 1=0", nil, nil
		}
		args := make([]any, len(states))
		for i, v := range states {
			args[i] = v
		}
		return " AND status IN (" + strings.TrimSuffix(strings.Repeat("?,", len(states)), ",") + ")", args, nil
	case "active":
		return " AND status NOT IN ('closed','rejected','cancelled')", nil, nil
	case "finished":
		return " AND status IN ('closed','rejected','cancelled')", nil, nil
	default:
		return "", nil, bad("工单队列无效")
	}
}
func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	q, args := orderFilter(u)
	rows, err := s.DB.QueryContext(r.Context(), "SELECT status,COUNT(*) FROM work_orders WHERE "+q+" GROUP BY status", args...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	counts := map[string]int{}
	total, action, active, ended := 0, 0, 0, 0
	for rows.Next() {
		var status string
		var n int
		if err = rows.Scan(&status, &n); err != nil {
			s.fail(w, r, err)
			return
		}
		counts[status] = n
		total += n
		if actionable(u.Role, status) {
			action += n
		}
		if finished(status) {
			ended += n
		} else {
			active += n
		}
	}
	if err = rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"total": total, "action": action, "active": active, "finished": ended, "statuses": counts})
}
