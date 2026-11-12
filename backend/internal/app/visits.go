package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type Visit struct {
	ID           int64      `json:"id"`
	TechnicianID int64      `json:"technician_id"`
	Start        time.Time  `json:"start"`
	End          time.Time  `json:"end"`
	Stage        string     `json:"stage"`
	Departed     *time.Time `json:"departed_at"`
	Arrived      *time.Time `json:"arrived_at"`
	NoShow       *time.Time `json:"no_show_at"`
	EndReason    string     `json:"end_reason"`
}
type VisitContext struct {
	RevisitRequired   bool       `json:"revisit_required"`
	Start             *time.Time `json:"availability_start"`
	End               *time.Time `json:"availability_end"`
	ActiveID          *int64     `json:"active_visit_id"`
	Exception         string     `json:"exception"`
	Reason            string     `json:"reason"`
	Resume            *time.Time `json:"expected_resume_at"`
	NeedsAvailability bool       `json:"needs_availability"`
	Visit             *Visit     `json:"visit"`
}
type visitInput struct {
	Version int64      `json:"version"`
	Start   *time.Time `json:"start"`
	End     *time.Time `json:"end"`
	Reason  string     `json:"reason"`
	Resume  *time.Time `json:"expected_resume_at"`
}
type visitQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readVisit(ctx context.Context, q visitQuerier, id int64) (v VisitContext, err error) {
	err = q.QueryRowContext(ctx, "SELECT availability_start,availability_end,active_visit_id,exception,reason,expected_resume_at,needs_availability,revisit_required FROM work_order_visit_context WHERE work_order_id=?", id).Scan(&v.Start, &v.End, &v.ActiveID, &v.Exception, &v.Reason, &v.Resume, &v.NeedsAvailability, &v.RevisitRequired)
	if err == sql.ErrNoRows {
		return v, nil
	}
	if err != nil {
		return
	}
	if v.ActiveID != nil {
		x := new(Visit)
		err = q.QueryRowContext(ctx, "SELECT id,technician_id,starts_at,ends_at,stage,departed_at,arrived_at,no_show_at,end_reason FROM work_order_visits WHERE id=? AND work_order_id=?", *v.ActiveID, id).Scan(&x.ID, &x.TechnicianID, &x.Start, &x.End, &x.Stage, &x.Departed, &x.Arrived, &x.NoShow, &x.EndReason)
		if err == nil {
			v.Visit = x
		}
	}
	return
}
func validVisitWindow(start, end *time.Time, now time.Time) error {
	if start == nil || end == nil || !start.After(now) || start.After(now.Add(14*24*time.Hour)) || end.Sub(*start) < 30*time.Minute || end.Sub(*start) > 4*time.Hour {
		return bad("请选择未来14天内开始、时长30分钟至4小时的时间窗")
	}
	return nil
}
func visitActions(o Order, u User, v VisitContext, now time.Time) []string {
	a := []string{}
	if finished(o.Status) || o.Status == "awaiting_confirmation" {
		return a
	}
	if u.Role == "requester" && u.ID == o.RequesterID && (v.Visit == nil || v.Visit.Stage == "scheduled") {
		a = append(a, "availability")
	}
	if u.Role != "technician" || o.AssigneeID == nil || *o.AssigneeID != u.ID || o.Status != "in_progress" {
		return a
	}
	if v.Exception == "waiting_parts" {
		return append(a, "resume")
	}
	a = append(a, "wait-parts")
	if (v.Visit == nil || v.Visit.Stage == "scheduled") && !v.NeedsAvailability && v.Start != nil && v.End != nil && v.End.After(now.Add(30*time.Minute)) {
		a = append(a, "schedule")
	}
	if v.Visit != nil {
		switch v.Visit.Stage {
		case "scheduled":
			if !now.After(v.Visit.End) {
				a = append(a, "depart")
			}
		case "departed":
			a = append(a, "arrive")
		case "arrived":
			a = append(a, "no-show")
		}
	}
	return a
}
func containsAction(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
func validateVisitAction(o Order, u User, v VisitContext, action string, a visitInput, now time.Time) error {
	if a.Version != o.Version {
		return conflict()
	}
	if utf8.RuneCountInString(a.Reason) > 2000 {
		return bad("说明不得超过2000字")
	}
	if !containsAction(visitActions(o, u, v, now), action) {
		return conflict()
	}
	switch action {
	case "availability":
		if a.Start != nil || a.End != nil {
			if err := validVisitWindow(a.Start, a.End, now); err != nil {
				return err
			}
		}
		if v.Visit != nil && a.Reason == "" {
			return bad("调整已确认预约请填写原因")
		}
	case "schedule":
		if err := validVisitWindow(a.Start, a.End, now); err != nil {
			return err
		}
		if a.Start.Before(*v.Start) || a.End.After(*v.End) {
			return bad("预约必须位于报修人提供的时间窗内")
		}
		if v.Visit != nil && a.Reason == "" {
			return bad("调整已确认预约请填写原因")
		}
	case "no-show", "wait-parts", "resume":
		if a.Reason == "" {
			return bad("请填写原因或处理说明")
		}
		if action == "wait-parts" && (a.Resume == nil || !a.Resume.After(now)) {
			return bad("请填写未来预计恢复时间")
		}
	}
	return nil
}
func endVisit(ctx context.Context, tx *sql.Tx, id int64, result, reason string) error {
	if _, err := tx.ExecContext(ctx, "UPDATE work_order_visits v JOIN work_order_visit_context c ON c.active_visit_id=v.id SET v.stage=?,v.end_reason=? WHERE c.work_order_id=?", result, reason, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE work_order_visit_context SET active_visit_id=NULL WHERE work_order_id=?", id)
	return err
}

// Called while holding the same work_orders row lock used by visit writes.
func finishVisitForOrder(ctx context.Context, tx *sql.Tx, o Order, action, next string) error {
	v, err := readVisit(ctx, tx, o.ID)
	if err != nil {
		return err
	}
	if action == "complete" && v.Exception != "" {
		return &Problem{409, "visit_blocked", "请先处理上门异常后再提交维修结果"}
	}
	if action != "reassign" && action != "reopen" && action != "cancel" && action != "complete" && action != "confirm" && next != "rejected" {
		return nil
	}
	result := "voided"
	if action == "complete" || action == "confirm" {
		result = "finished"
	}
	if err = endVisit(ctx, tx, o.ID, result, action); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE work_order_visit_context SET exception='',reason='',expected_resume_at=NULL,needs_availability=FALSE,revisit_required=FALSE WHERE work_order_id=?", o.ID)
	return err
}
func (s *Server) visitGet(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
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
	q, args := orderFilter(current(r))
	args = append(args, id)
	o, err := scanOrder(tx.QueryRowContext(r.Context(), "SELECT "+orderColumns+" FROM work_orders WHERE "+q+" AND id=?", args...))
	if err != nil {
		s.fail(w, r, dbError(err))
		return
	}
	v, err := readVisit(r.Context(), tx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := time.Now().UTC()
	if err = tx.Commit(); err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"context": v, "version": o.Version, "allowed_actions": visitActions(o, current(r), v, now), "as_of": now})
}
func (s *Server) visitWrite(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	action := r.PathValue("action")
	if r.Method == "POST" && action == "availability" {
		s.fail(w, r, invisible())
		return
	}
	if r.Method == "PUT" {
		action = "availability"
	}
	role := "technician"
	if action == "availability" {
		role = "requester"
	}
	if !containsAction([]string{"availability", "schedule", "depart", "arrive", "no-show", "wait-parts", "resume"}, action) {
		s.fail(w, r, invisible())
		return
	}
	var raw json.RawMessage
	if err = decode(w, r, &raw); err != nil {
		s.fail(w, r, err)
		return
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var a visitInput
	if err = d.Decode(&a); err != nil || string(raw) == "null" {
		s.fail(w, r, bad("上门参数无效，请检查字段和带时区的时间"))
		return
	}
	if err = d.Decode(new(any)); err != io.EOF {
		s.fail(w, r, bad("参数无效"))
		return
	}
	a.Reason = strings.TrimSpace(a.Reason)
	u := current(r)
	ctx := r.Context()
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	q, args := orderFilter(u)
	args = append(args, id)
	o, err := scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM work_orders WHERE "+q+" AND id=? FOR UPDATE", args...))
	if err != nil {
		s.fail(w, r, dbError(err))
		return
	}
	if u.Role != role {
		s.fail(w, r, denied())
		return
	}
	v, err := readVisit(ctx, tx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := time.Now().UTC()
	if err = validateVisitAction(o, u, v, action, a, now); err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO work_order_visit_context(work_order_id) VALUES(?)", id); err != nil {
		s.fail(w, r, err)
		return
	}
	note := a.Reason
	switch action {
	case "availability":
		err = endVisit(ctx, tx, id, "voided", a.Reason)
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE work_order_visit_context SET availability_start=?,availability_end=?,needs_availability=? WHERE work_order_id=?", a.Start, a.End, a.Start == nil && v.Exception == "no_show", id)
		}
	case "schedule":
		err = endVisit(ctx, tx, id, "voided", a.Reason)
		if err == nil {
			var res sql.Result
			res, err = tx.ExecContext(ctx, "INSERT INTO work_order_visits(work_order_id,technician_id,starts_at,ends_at,stage) VALUES(?,?,?,?,'scheduled')", id, u.ID, a.Start, a.End)
			if err == nil {
				var vid int64
				vid, err = res.LastInsertId()
				if err == nil {
					_, err = tx.ExecContext(ctx, "UPDATE work_order_visit_context SET active_visit_id=? WHERE work_order_id=?", vid, id)
				}
			}
		}
	case "depart", "arrive":
		field, stage := "departed_at", "departed"
		if action == "arrive" {
			field, stage = "arrived_at", "arrived"
		}
		_, err = tx.ExecContext(ctx, "UPDATE work_order_visits SET stage=?,"+field+"=? WHERE id=?", stage, now, *v.ActiveID)
		if err == nil && action == "arrive" {
			_, err = tx.ExecContext(ctx, "UPDATE work_order_visit_context SET exception='',reason='',expected_resume_at=NULL,needs_availability=FALSE,revisit_required=FALSE WHERE work_order_id=?", id)
		}
	case "no-show":
		_, err = tx.ExecContext(ctx, "UPDATE work_order_visits SET stage='no_show',no_show_at=?,end_reason=? WHERE id=?", now, a.Reason, *v.ActiveID)
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE work_order_visit_context SET active_visit_id=NULL,exception='no_show',reason=?,needs_availability=TRUE,revisit_required=TRUE WHERE work_order_id=?", a.Reason, id)
		}
	case "wait-parts":
		err = endVisit(ctx, tx, id, "voided", a.Reason)
		if err == nil {
			_, err = tx.ExecContext(ctx, "UPDATE work_order_visit_context SET exception='waiting_parts',reason=?,expected_resume_at=? WHERE work_order_id=?", a.Reason, a.Resume, id)
		}
	case "resume":
		_, err = tx.ExecContext(ctx, "UPDATE work_order_visit_context SET exception=IF(revisit_required,'no_show',''),reason='',expected_resume_at=NULL WHERE work_order_id=?", id)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE work_orders SET version=version+1,updated_at=? WHERE id=?", now, id)
	}
	if a.Start != nil && a.End != nil {
		note = "北京时间 " + a.Start.In(businessTZ).Format("2006-01-02 15:04") + " 至 " + a.End.In(businessTZ).Format("2006-01-02 15:04") + " " + note
	}
	if a.Resume != nil {
		note += "；预计恢复 " + a.Resume.In(businessTZ).Format("2006-01-02 15:04")
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO work_order_events(work_order_id,actor_id,action,from_status,to_status,note,assignee_id,created_at) VALUES(?,?,?,?,?,?,?,?)", id, u.ID, "visit."+action, o.Status, o.Status, note, o.AssigneeID, now)
	}
	if err == nil {
		err = writeAudit(ctx, tx, requestID(r), &u, "work_order.visit."+action, id, "success", map[string]any{"version": o.Version + 1})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"version": o.Version + 1})
}
