package app

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type User struct {
	ID       int64  `json:"id"`
	OrgID    int64  `json:"org_id"`
	Username string `json:"username"`
	Name     string `json:"display_name"`
	Role     string `json:"role"`
}
type Order struct {
	ID            int64     `json:"id"`
	Number        string    `json:"number"`
	OrgID         int64     `json:"org_id"`
	RequesterID   int64     `json:"requester_id"`
	Category      string    `json:"category"`
	Location      string    `json:"location"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	AssigneeID    *int64    `json:"assignee_id"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	RequesterName string    `json:"requester_name"`
	AssigneeName  string    `json:"assignee_name"`
}
type Input struct {
	Category    string `json:"category"`
	Location    string `json:"location"`
	Description string `json:"description"`
}
type Action struct {
	Version      int64  `json:"version"`
	Decision     string `json:"decision"`
	Reason       string `json:"reason"`
	TechnicianID int64  `json:"technician_id"`
	Measures     string `json:"measures"`
	Result       string `json:"result"`
	Input
}
type Problem struct {
	Status        int
	Code, Message string
}

func (e *Problem) Error() string { return e.Message }
func bad(s string) error         { return &Problem{400, "invalid_input", s} }
func denied() error              { return &Problem{403, "forbidden", "没有执行此操作的权限"} }
func invisible() error           { return &Problem{404, "not_found", "资源不存在"} }
func conflict() error {
	return &Problem{409, "conflict", "工单状态或版本已变化，请刷新后重试"}
}
func categoryOK(v string) bool { return v == "lighting" || v == "electrical" || v == "plumbing" }
func (i *Input) normalize() error {
	i.Location = strings.TrimSpace(i.Location)
	i.Description = strings.TrimSpace(i.Description)
	if !categoryOK(i.Category) {
		return bad("请选择照明、电器或给排水类别")
	}
	if i.Location == "" || utf8.RuneCountInString(i.Location) > 200 {
		return bad("位置不能为空且不得超过200字")
	}
	if i.Description == "" || utf8.RuneCountInString(i.Description) > 2000 {
		return bad("故障描述不能为空且不得超过2000字")
	}
	return nil
}
func visible(u User, o Order) bool {
	return u.OrgID == o.OrgID && (u.Role == "admin" || (u.Role == "requester" && u.ID == o.RequesterID) || (u.Role == "technician" && o.AssigneeID != nil && *o.AssigneeID == u.ID))
}
func actionRole(action string) string {
	switch action {
	case "review", "assign", "reassign":
		return "admin"
	case "resubmit", "confirm", "reopen", "cancel":
		return "requester"
	case "accept", "records", "complete":
		return "technician"
	}
	return ""
}
func transition(o Order, u User, action string, a *Action) (string, error) {
	if actionRole(action) == "" {
		return "", invisible()
	}
	if u.Role != actionRole(action) {
		return "", denied()
	}
	if !visible(u, o) {
		return "", invisible()
	}
	if a.Version != o.Version {
		return "", conflict()
	}
	a.Reason = strings.TrimSpace(a.Reason)
	a.Measures = strings.TrimSpace(a.Measures)
	a.Result = strings.TrimSpace(a.Result)
	if utf8.RuneCountInString(a.Reason) > 2000 || utf8.RuneCountInString(a.Measures) > 2000 || utf8.RuneCountInString(a.Result) > 2000 {
		return "", bad("说明不得超过2000字")
	}
	next := ""
	switch action {
	case "review":
		if o.Status == "submitted" {
			switch a.Decision {
			case "approve":
				next = "unassigned"
			case "request_info":
				next = "needs_info"
			case "reject":
				next = "rejected"
			default:
				return "", bad("审核决定无效")
			}
			if a.Decision != "approve" && a.Reason == "" {
				return "", bad("请填写补充要求或驳回原因")
			}
		}
	case "resubmit":
		if o.Status == "needs_info" {
			if err := a.Input.normalize(); err != nil {
				return "", err
			}
			next = "submitted"
		}
	case "assign":
		if o.Status == "unassigned" {
			next = "assigned"
		}
	case "reassign":
		if o.Status == "assigned" {
			if a.Reason == "" {
				return "", bad("请填写改派原因")
			}
			next = "assigned"
		}
	case "accept":
		if o.Status == "assigned" {
			next = "in_progress"
		}
	case "records", "complete":
		if o.Status == "in_progress" {
			if a.Measures == "" || a.Result == "" {
				return "", bad("请填写处理措施和处理结果")
			}
			next = "in_progress"
			if action == "complete" {
				next = "awaiting_confirmation"
			}
		}
	case "confirm":
		if o.Status == "awaiting_confirmation" {
			next = "closed"
		}
	case "reopen":
		if o.Status == "awaiting_confirmation" {
			if a.Reason == "" {
				return "", bad("请填写未解决原因")
			}
			next = "unassigned"
		}
	case "cancel":
		if o.Status == "submitted" || o.Status == "needs_info" || o.Status == "unassigned" || o.Status == "assigned" {
			if a.Reason == "" {
				return "", bad("请填写取消原因")
			}
			next = "cancelled"
		}
	}
	if next == "" {
		return "", conflict()
	}
	return next, nil
}

const orderColumns = "id,number,org_id,requester_id,category,location,description,status,assignee_id,version,created_at,updated_at"

type scanner interface{ Scan(...any) error }

func scanOrder(r scanner) (Order, error) {
	var o Order
	err := r.Scan(&o.ID, &o.Number, &o.OrgID, &o.RequesterID, &o.Category, &o.Location, &o.Description, &o.Status, &o.AssigneeID, &o.Version, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}
func orderFilter(u User) (string, []any) {
	q := "org_id=?"
	v := []any{u.OrgID}
	if u.Role == "requester" {
		q += " AND requester_id=?"
		v = append(v, u.ID)
	} else if u.Role == "technician" {
		q += " AND assignee_id=?"
		v = append(v, u.ID)
	}
	return q, v
}
func dbError(err error) error {
	if err == sql.ErrNoRows {
		return invisible()
	}
	return fmt.Errorf("database operation failed: %w", err)
}
