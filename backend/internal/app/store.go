package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"strings"
	"time"
)

//go:embed schema.sql
var schema string

func Initialize(db *sql.DB, c Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, statement := range strings.Split(schema, ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("schema initialization failed: %w", err)
			}
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT IGNORE INTO schema_migrations VALUES(1,UTC_TIMESTAMP(6)),(2,UTC_TIMESTAMP(6))"); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO organizations VALUES(1,'演示校区'),(2,'隔离测试组织')"); err != nil {
		return err
	}
	seeds := []struct {
		Username, Name, Role, Password string
		Org                            int64
	}{
		{"admin", "后勤管理员", "admin", "SEED_ADMIN_PASSWORD", 1}, {"requester", "报修人一", "requester", "SEED_REQUESTER_PASSWORD", 1}, {"requester2", "报修人二", "requester", "SEED_REQUESTER2_PASSWORD", 1}, {"tech", "维修人员一", "technician", "SEED_TECH_PASSWORD", 1}, {"tech2", "维修人员二", "technician", "SEED_TECH2_PASSWORD", 1}, {"other", "隔离报修人", "requester", "SEED_OTHER_PASSWORD", 2}, {"other_admin", "隔离管理员", "admin", "SEED_OTHER_PASSWORD", 2},
	}
	for _, seed := range seeds {
		hash, err := bcrypt.GenerateFromPassword([]byte(c.Passwords[seed.Password]), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO users(org_id,username,display_name,role,password_hash) VALUES(?,?,?,?,?)", seed.Org, seed.Username, seed.Name, seed.Role, string(hash)); err != nil {
			return err
		}
	}
	for _, name := range []string{"tech", "tech2"} {
		for _, cat := range []string{"lighting", "electrical", "plumbing"} {
			if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO technician_categories(user_id,category) SELECT id,? FROM users WHERE username=?", cat, name); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func writeAudit(ctx context.Context, e execer, requestID string, u *User, action string, resource any, outcome string, meta any) error {
	var org, actor any
	if u != nil {
		org = u.OrgID
		actor = u.ID
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = e.ExecContext(ctx, "INSERT INTO audit_events(org_id,actor_id,request_id,action,resource_id,outcome,metadata,created_at) VALUES(?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", org, actor, requestID, action, resource, outcome, string(b))
	return err
}
func (s *Server) enrich(ctx context.Context, o *Order) {
	_ = s.DB.QueryRowContext(ctx, "SELECT display_name FROM users WHERE id=? AND org_id=?", o.RequesterID, o.OrgID).Scan(&o.RequesterName)
	if o.AssigneeID != nil {
		_ = s.DB.QueryRowContext(ctx, "SELECT display_name FROM users WHERE id=? AND org_id=?", *o.AssigneeID, o.OrgID).Scan(&o.AssigneeName)
	}
}
func (s *Server) getOrder(ctx context.Context, u User, id int64) (Order, error) {
	q, args := orderFilter(u)
	args = append(args, id)
	o, err := scanOrder(s.DB.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM work_orders WHERE "+q+" AND id=?", args...))
	if err != nil {
		return o, dbError(err)
	}
	s.enrich(ctx, &o)
	return o, nil
}
func (s *Server) create(ctx context.Context, u User, i Input, key, requestID string) (Order, bool, error) {
	var o Order
	if u.Role != "requester" {
		return o, false, denied()
	}
	if err := i.normalize(); err != nil {
		return o, false, err
	}
	if len(key) < 8 || len(key) > 100 || strings.TrimSpace(key) != key {
		return o, false, bad("提交需要8至100字符的幂等键")
	}
	body, _ := json.Marshal(i)
	digest := sha256.Sum256(body)
	hash := hex.EncodeToString(digest[:])
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return o, false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM idempotency_records WHERE org_id=? AND user_id=? AND idem_key=? AND expires_at<=UTC_TIMESTAMP(6)", u.OrgID, u.ID, key); err != nil {
		return o, false, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT IGNORE INTO idempotency_records(org_id,user_id,idem_key,request_hash,expires_at) VALUES(?,?,?,?,DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 24 HOUR))", u.OrgID, u.ID, key, hash); err != nil {
		return o, false, err
	}
	var existingHash string
	var existing sql.NullInt64
	if err = tx.QueryRowContext(ctx, "SELECT request_hash,work_order_id FROM idempotency_records WHERE org_id=? AND user_id=? AND idem_key=? FOR UPDATE", u.OrgID, u.ID, key).Scan(&existingHash, &existing); err != nil {
		return o, false, err
	}
	if existingHash != hash {
		return o, false, &Problem{409, "idempotency_conflict", "同一提交标识的内容已变化"}
	}
	if existing.Valid {
		o, err = scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM work_orders WHERE id=? AND org_id=? AND requester_id=?", existing.Int64, u.OrgID, u.ID))
		if err != nil {
			return o, false, err
		}
		if err = tx.Commit(); err != nil {
			return o, false, err
		}
		s.enrich(ctx, &o)
		return o, true, nil
	}
	number := "RP" + time.Now().UTC().Format("20060102") + randomHex(8)
	res, err := tx.ExecContext(ctx, "INSERT INTO work_orders(number,org_id,requester_id,category,location,description,status,created_at,updated_at) VALUES(?,?,?,?,?,?,'submitted',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", number, u.OrgID, u.ID, i.Category, i.Location, i.Description)
	if err != nil {
		return o, false, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return o, false, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE idempotency_records SET work_order_id=? WHERE org_id=? AND user_id=? AND idem_key=?", id, u.OrgID, u.ID, key); err != nil {
		return o, false, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO work_order_events(work_order_id,actor_id,action,from_status,to_status,note,created_at) VALUES(?,?,'create','','submitted','',UTC_TIMESTAMP(6))", id, u.ID); err != nil {
		return o, false, err
	}
	if err = writeAudit(ctx, tx, requestID, &u, "work_order.create", id, "success", map[string]any{"to": "submitted"}); err != nil {
		return o, false, err
	}
	o, err = scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM work_orders WHERE id=?", id))
	if err != nil {
		return o, false, err
	}
	if err = tx.Commit(); err != nil {
		return o, false, err
	}
	s.enrich(ctx, &o)
	return o, false, nil
}
func (s *Server) update(ctx context.Context, u User, id int64, action string, a Action, requestID string) (Order, error) {
	var o Order
	if actionRole(action) == "" {
		return o, invisible()
	}
	if u.Role != actionRole(action) {
		return o, denied()
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return o, err
	}
	defer tx.Rollback()
	q, args := orderFilter(u)
	args = append(args, id)
	o, err = scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM work_orders WHERE "+q+" AND id=? FOR UPDATE", args...))
	if err != nil {
		return o, dbError(err)
	}
	next, err := transition(o, u, action, &a)
	if err != nil {
		return o, err
	}
	assignee := o.AssigneeID
	if action == "assign" || action == "reassign" {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users u JOIN technician_categories c ON c.user_id=u.id WHERE u.id=? AND u.org_id=? AND u.enabled=TRUE AND u.role='technician' AND c.category=?", a.TechnicianID, u.OrgID, o.Category).Scan(&count); err != nil {
			return o, err
		}
		if count != 1 {
			return o, bad("维修人员不可用或类别不匹配")
		}
		if assignee != nil && *assignee == a.TechnicianID && action == "reassign" {
			return o, bad("请选择另一名维修人员")
		}
		assignee = &a.TechnicianID
	}
	if action == "reopen" {
		assignee = nil
	}
	if action == "resubmit" {
		o.Category = a.Category
		o.Location = a.Location
		o.Description = a.Description
	}
	result, err := tx.ExecContext(ctx, "UPDATE work_orders SET category=?,location=?,description=?,status=?,assignee_id=?,version=version+1,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND org_id=? AND version=?", o.Category, o.Location, o.Description, next, assignee, id, u.OrgID, a.Version)
	if err != nil {
		return o, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return o, err
	}
	if n != 1 {
		return o, conflict()
	}
	note := a.Reason
	if action == "records" || action == "complete" {
		if _, err = tx.ExecContext(ctx, "INSERT INTO repair_records(work_order_id,actor_id,measures,result,created_at) VALUES(?,?,?,?,UTC_TIMESTAMP(6))", id, u.ID, a.Measures, a.Result); err != nil {
			return o, err
		}
		note = a.Result
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO work_order_events(work_order_id,actor_id,action,from_status,to_status,note,assignee_id,created_at) VALUES(?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", id, u.ID, action, o.Status, next, note, assignee); err != nil {
		return o, err
	}
	if err = writeAudit(ctx, tx, requestID, &u, "work_order."+action, id, "success", map[string]any{"from": o.Status, "to": next, "assignee_id": assignee, "version": a.Version + 1}); err != nil {
		return o, err
	}
	o, err = scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM work_orders WHERE id=?", id))
	if err != nil {
		return o, err
	}
	if err = tx.Commit(); err != nil {
		return o, err
	}
	s.enrich(ctx, &o)
	return o, nil
}
