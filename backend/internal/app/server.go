package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"golang.org/x/crypto/bcrypt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	DB        *sql.DB
	Secret    string
	limit     limiter
	dummyHash string
}

func NewServer(db *sql.DB, secret string) *Server {
	hash, _ := bcrypt.GenerateFromPassword([]byte(randomHex(16)), bcrypt.DefaultCost)
	return &Server{DB: db, Secret: secret, limit: limiter{entries: map[string]limitEntry{}}, dummyHash: string(hash)}
}

type requestKey struct{}

func requestID(r *http.Request) string { v, _ := r.Context().Value(requestKey{}).(string); return v }
func send(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	if err := d.Decode(v); err != nil {
		return bad("请求内容不是有效JSON或超过限制")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return bad("请求只能包含一个JSON对象")
	}
	return nil
}
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	p := &Problem{503, "unavailable", "服务暂不可用，请稍后重试"}
	var supplied *Problem
	if errors.As(err, &supplied) {
		p = supplied
	} else {
		log.Printf("request=%s outcome=dependency_failure", requestID(r))
	}
	if p.Status == 403 || p.Status == 404 {
		var u *User
		if v, ok := r.Context().Value(userKey{}).(User); ok {
			u = &v
		}
		if e := writeAudit(r.Context(), s.DB, requestID(r), u, "access.denied", nil, "denied", map[string]any{"path": r.URL.Path, "code": p.Code}); e != nil {
			log.Printf("request=%s outcome=audit_unavailable", requestID(r))
		}
	}
	send(w, p.Status, map[string]any{"error": map[string]any{"code": p.Code, "message": p.Message, "request_id": requestID(r)}})
}
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { send(w, 200, map[string]string{"status": "alive"}) })
	m.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := s.DB.PingContext(ctx); err != nil {
			s.fail(w, r, err)
			return
		}
		send(w, 200, map[string]string{"status": "ready"})
	})
	m.HandleFunc("POST /api/login", s.login)
	m.HandleFunc("POST /api/logout", s.protect(s.logout))
	m.HandleFunc("GET /api/me", s.protect(func(w http.ResponseWriter, r *http.Request) { send(w, 200, map[string]any{"user": current(r)}) }))
	m.HandleFunc("GET /api/agents", s.protect(s.agents))
	m.HandleFunc("GET /api/mcp/tools", s.protect(s.mcpDiscover))
	m.HandleFunc("PUT /api/agents/{id}", s.protect(s.updateAgent))
	m.HandleFunc("GET /api/conversations", s.protect(s.conversations))
	m.HandleFunc("POST /api/conversations", s.protect(s.createConversation))
	m.HandleFunc("GET /api/conversations/{id}/messages", s.protect(s.messages))
	m.HandleFunc("POST /api/conversations/{id}/messages", s.protect(s.messageStream))
	m.HandleFunc("GET /api/conversations/{id}/citations/{message}/{chunk}", s.protect(s.citationSource))
	m.HandleFunc("GET /api/capabilities", s.protect(s.capabilities))
	m.HandleFunc("GET /api/retrieval", s.protect(s.retrieval))
	m.HandleFunc("GET /api/materials", s.protect(s.materials))
	m.HandleFunc("POST /api/materials", s.protect(s.uploadMaterial))
	m.HandleFunc("GET /api/materials/{id}", s.protect(s.materialDetail))
	m.HandleFunc("GET /api/materials/{id}/file", s.protect(s.materialFile))
	m.HandleFunc("POST /api/materials/{id}/reindex", s.protect(s.reindexMaterial))
	m.HandleFunc("GET /api/work-orders/summary", s.protect(s.summary))
	m.HandleFunc("GET /api/work-orders", s.protect(s.list))
	m.HandleFunc("POST /api/work-orders", s.protect(s.createHTTP))
	m.HandleFunc("GET /api/work-orders/{id}", s.protect(s.detail))
	m.HandleFunc("POST /api/work-orders/{id}/{action}", s.protect(s.actionHTTP))
	m.HandleFunc("GET /api/technicians", s.protect(s.technicians))
	m.HandleFunc("GET /api/audit", s.protect(s.audit))
	m.HandleFunc("/api/", s.protect(func(w http.ResponseWriter, r *http.Request) { s.fail(w, r, invisible()) }))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := randomHex(16)
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		timeout := 10 * time.Second
		if r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/api/conversations/") && strings.HasSuffix(r.URL.Path, "/messages") {
			timeout = 125 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), requestKey{}, id), timeout)
		defer cancel()
		m.ServeHTTP(w, r.WithContext(ctx))
	})
}
func parseID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, invisible()
	}
	return id, nil
}
func pagination(r *http.Request) (int, int, error) {
	page, size := 1, 20
	var err error
	if v := r.URL.Query().Get("page"); v != "" {
		page, err = strconv.Atoi(v)
		if err != nil || page < 1 || page > 100000 {
			return 0, 0, bad("页码无效")
		}
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		size, err = strconv.Atoi(v)
		if err != nil || size < 1 || size > 100 {
			return 0, 0, bad("每页数量须为1至100")
		}
	}
	return page, size, nil
}
func (s *Server) createHTTP(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	if u.Role != "requester" {
		s.fail(w, r, denied())
		return
	}
	var input Input
	if err := decode(w, r, &input); err != nil {
		s.fail(w, r, err)
		return
	}
	o, replay, err := s.create(r.Context(), u, input, r.Header.Get("Idempotency-Key"), requestID(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status := 201
	if replay {
		status = 200
	}
	send(w, status, map[string]any{"order": o, "replayed": replay})
}
func (s *Server) actionHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var a Action
	if err = decode(w, r, &a); err != nil {
		s.fail(w, r, err)
		return
	}
	o, err := s.update(r.Context(), current(r), id, r.PathValue("action"), a, requestID(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"order": o})
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	q, args := orderFilter(u)
	query := r.URL.Query()
	queue, queueArgs, queueErr := queueFilter(u.Role, query.Get("queue"))
	if queueErr != nil {
		s.fail(w, r, queueErr)
		return
	}
	q += queue
	args = append(args, queueArgs...)
	if v := query.Get("status"); v != "" {
		q += " AND status=?"
		args = append(args, v)
	}
	if v := query.Get("category"); v != "" {
		if !categoryOK(v) {
			s.fail(w, r, bad("类别无效"))
			return
		}
		q += " AND category=?"
		args = append(args, v)
	}
	if v := query.Get("location"); v != "" {
		q += " AND location LIKE ?"
		args = append(args, "%"+v+"%")
	}
	page, size, err := pagination(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var count int
	if err = s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM work_orders WHERE "+q, args...).Scan(&count); err != nil {
		s.fail(w, r, err)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT "+orderColumns+" FROM work_orders WHERE "+q+" ORDER BY id DESC LIMIT ? OFFSET ?", append(args, size, (page-1)*size)...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []Order{}
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		items = append(items, o)
	}
	if err = rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	rows.Close()
	for i := range items {
		s.enrich(r.Context(), &items[i])
	}
	send(w, 200, map[string]any{"items": items, "total": count, "page": page, "page_size": size})
}
func (s *Server) detail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	o, err := s.getOrder(r.Context(), current(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT e.id,e.action,e.from_status,e.to_status,e.note,e.assignee_id,e.created_at,u.display_name FROM work_order_events e JOIN users u ON u.id=e.actor_id WHERE e.work_order_id=? ORDER BY e.id", id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	events := []map[string]any{}
	for rows.Next() {
		var eid int64
		var action, from, to, note, name string
		var assignee *int64
		var at time.Time
		if err = rows.Scan(&eid, &action, &from, &to, &note, &assignee, &at, &name); err != nil {
			rows.Close()
			s.fail(w, r, err)
			return
		}
		events = append(events, map[string]any{"id": eid, "action": action, "from_status": from, "to_status": to, "note": note, "assignee_id": assignee, "created_at": at, "actor_name": name})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rows, err = s.DB.QueryContext(r.Context(), "SELECT p.id,p.measures,p.result,p.created_at,u.display_name FROM repair_records p JOIN users u ON u.id=p.actor_id WHERE p.work_order_id=? ORDER BY p.id", id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	records := []map[string]any{}
	for rows.Next() {
		var rid int64
		var measures, result, name string
		var at time.Time
		if err = rows.Scan(&rid, &measures, &result, &at, &name); err != nil {
			rows.Close()
			s.fail(w, r, err)
			return
		}
		records = append(records, map[string]any{"id": rid, "measures": measures, "result": result, "created_at": at, "actor_name": name})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"order": o, "events": events, "records": records})
}
func (s *Server) technicians(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	if u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	cat := r.URL.Query().Get("category")
	if !categoryOK(cat) {
		s.fail(w, r, bad("类别无效"))
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT u.id,u.display_name,COUNT(w.id) FROM users u JOIN technician_categories c ON c.user_id=u.id AND c.category=? LEFT JOIN work_orders w ON w.assignee_id=u.id AND w.org_id=u.org_id AND w.status IN ('assigned','in_progress') WHERE u.org_id=? AND u.enabled=TRUE AND u.role='technician' GROUP BY u.id,u.display_name ORDER BY COUNT(w.id),u.id", cat, u.OrgID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var name string
		var count int
		if err = rows.Scan(&id, &name, &count); err != nil {
			s.fail(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "display_name": name, "category": cat, "pending_count": count})
	}
	if err = rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"items": items})
}
func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	if u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	page, size, err := pagination(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	q := "a.org_id=?"
	args := []any{u.OrgID}
	if v := r.URL.Query().Get("resource_id"); v != "" {
		q += " AND a.resource_id=?"
		args = append(args, v)
	}
	var count int
	if err = s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM audit_events a WHERE "+q, args...).Scan(&count); err != nil {
		s.fail(w, r, err)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT a.id,a.request_id,a.actor_id,a.action,a.resource_id,a.outcome,a.metadata,a.created_at,COALESCE(u.display_name,''),COALESCE(o.number,'') FROM audit_events a LEFT JOIN users u ON u.id=a.actor_id AND u.org_id=a.org_id LEFT JOIN work_orders o ON o.id=a.resource_id AND o.org_id=a.org_id AND a.action LIKE 'work_order.%' WHERE "+q+" ORDER BY a.id DESC LIMIT ? OFFSET ?", append(args, size, (page-1)*size)...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var actor, resource *int64
		var req, action, outcome, actorName, orderNumber string
		var meta json.RawMessage
		var at time.Time
		if err = rows.Scan(&id, &req, &actor, &action, &resource, &outcome, &meta, &at, &actorName, &orderNumber); err != nil {
			s.fail(w, r, err)
			return
		}
		items = append(items, map[string]any{"id": id, "request_id": req, "actor_id": actor, "action": action, "resource_id": resource, "outcome": outcome, "metadata": meta, "created_at": at, "actor_name": actorName, "order_number": orderNumber})
	}
	if err = rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"items": items, "total": count, "page": page, "page_size": size})
}
