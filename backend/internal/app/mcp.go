package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	MCPMyOrders       = "get_my_work_orders"
	MCPAssignedOrders = "get_assigned_work_orders"
	MCPDispatch       = "list_dispatch_candidates"
	mcpAudience       = "campus-repair-mcp"
)

var mcpRoles = map[string]string{
	MCPMyOrders: "requester", MCPAssignedOrders: "technician", MCPDispatch: "admin", MCPQueryOrders: "all", MCPOrderDetail: "all", MCPOrderVisit: "all", MCPKnowledge: "all",
}

func mcpRoleAllowed(name, role string) bool {
	allowed := mcpRoles[name]
	return allowed != "" && (allowed == role || (allowed == "all" && (role == "requester" || role == "technician" || role == "admin")))
}

var errMCPToolLimit = errors.New("MCP turn tool limit reached")

type mcpClaims struct {
	Skill             string `json:"skill_id,omitempty"`
	SkillVersion      int64  `json:"skill_config_version,omitempty"`
	DefinitionVersion int    `json:"skill_definition_version,omitempty"`
	Session           string `json:"session_id"`
	AgentID           int64  `json:"agent_id"`
	Request           string `json:"request_id"`
	Audience          string `json:"audience"`
	Issued            int64  `json:"iat"`
	Expires           int64  `json:"exp"`
}

// MCP credentials are produced by the API, never by a model or browser.
func SignMCPCall(secret, sessionHash string, agentID int64, requestID string, skill ...*SkillRun) string {
	c := mcpClaims{Session: sessionHash, AgentID: agentID, Request: requestID, Audience: mcpAudience, Issued: time.Now().Unix(), Expires: time.Now().Add(60 * time.Second).Unix()}
	if len(skill) > 0 && skill[0] != nil {
		c.Skill = skill[0].ID
		c.SkillVersion = skill[0].ConfigVersion
		c.DefinitionVersion = skill[0].DefinitionVersion
	}
	b, _ := json.Marshal(c)
	payload := base64.RawURLEncoding.EncodeToString(b)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyMCPCall(secret, token string) (mcpClaims, error) {
	var c mcpClaims
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(token) > 2048 || len(secret) < 32 {
		return c, errors.New("invalid MCP credential")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return c, errors.New("invalid MCP credential")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return c, errors.New("invalid MCP credential")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, errors.New("invalid MCP credential")
	}
	now := time.Now().Unix()
	if c.Audience != mcpAudience || len(c.Session) != 64 || len(c.Request) != 32 || c.AgentID < 1 || c.Issued > now+5 || c.Expires <= now || c.Expires-c.Issued > 60 {
		return c, errors.New("expired or invalid MCP credential")
	}
	if c.Skill != "" {
		definition, ok := skillDefinition(c.Skill)
		if !ok || c.SkillVersion < 1 || c.DefinitionVersion != definition.DefinitionVersion {
			return c, errors.New("invalid skill credential")
		}
	}
	return c, nil
}

func mcpIdentity(ctx context.Context, db *sql.DB, c mcpClaims) (User, error) {
	var u User
	err := db.QueryRowContext(ctx, `SELECT u.id,u.org_id,u.username,u.display_name,u.role
		FROM sessions s JOIN users u ON u.id=s.user_id
		JOIN agents a ON a.id=? AND a.org_id=u.org_id AND a.role=u.role AND a.enabled=TRUE
		WHERE s.token_hash=? AND s.expires_at>UTC_TIMESTAMP(6) AND u.enabled=TRUE`, c.AgentID, c.Session).
		Scan(&u.ID, &u.OrgID, &u.Username, &u.Name, &u.Role)
	if err != nil {
		return u, err
	}
	return u, nil
}

func reserveMCPCall(ctx context.Context, db *sql.DB, c mcpClaims) error {
	if _, err := db.ExecContext(ctx, "DELETE FROM mcp_call_counts WHERE expires_at<DATE_SUB(UTC_TIMESTAMP(6),INTERVAL 1 DAY)"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, "INSERT IGNORE INTO mcp_call_counts(request_id,expires_at) VALUES(?,FROM_UNIXTIME(?))", c.Request, c.Expires); err != nil {
		return err
	}
	result, err := db.ExecContext(ctx, "UPDATE mcp_call_counts SET used=used+1 WHERE request_id=? AND expires_at>UTC_TIMESTAMP(6) AND used<3", c.Request)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errMCPToolLimit
	}
	return nil
}

func mcpAllowed(ctx context.Context, db *sql.DB, c mcpClaims, u User, name string) error {
	if !mcpRoleAllowed(name, u.Role) {
		return errors.New("tool is not allowed for this role")
	}
	var exists int
	err := db.QueryRowContext(ctx, `SELECT 1 FROM agent_tools t JOIN agents a ON a.id=t.agent_id
		WHERE t.agent_id=? AND t.tool_name=? AND t.enabled=TRUE AND a.org_id=? AND a.role=? AND a.enabled=TRUE`, c.AgentID, name, u.OrgID, u.Role).Scan(&exists)
	if err == sql.ErrNoRows {
		return errors.New("tool is not bound or is disabled")
	}
	if err != nil {
		return err
	}
	if c.Skill != "" {
		definition, ok := skillDefinition(c.Skill)
		if !ok || !skillRoleAllowed(c.Skill, u.Role) || !queryContains(definition.AllowedTools, name) || definition.DefinitionVersion != c.DefinitionVersion {
			return errors.New("tool not allowed for skill")
		}
		err = db.QueryRowContext(ctx, "SELECT 1 FROM skills sk JOIN agent_skills b ON b.skill_id=sk.skill_id JOIN agents a ON a.id=b.agent_id AND a.org_id=sk.org_id WHERE sk.org_id=? AND sk.skill_id=? AND sk.enabled=TRUE AND sk.version=? AND b.agent_id=? AND b.enabled=TRUE", u.OrgID, c.Skill, c.SkillVersion, c.AgentID).Scan(&exists)
	}
	return err
}

func validMCPArguments(raw json.RawMessage, name string) bool {
	if name == MCPDispatch {
		if fields, ok := mcpObjectFields(raw); ok && fields["scope"] != nil {
			var mode string
			var q OrderQuery
			return len(fields) == 2 && json.Unmarshal(fields["worklist"], &mode) == nil && mode == "pre_review" && validMCPArguments(fields["scope"], MCPQueryOrders) && json.Unmarshal(fields["scope"], &q) == nil && q.Status == "submitted" && q.Analysis == "" && q.Metric == ""
		}
	}
	if name == MCPOrderDetail || name == MCPOrderVisit || name == MCPKnowledge {
		return validExtendedMCPArguments(raw, name)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return name != MCPDispatch && name != MCPQueryOrders
	}
	if name == MCPQueryOrders {
		fields, ok := mcpObjectFields(raw)
		if !ok {
			return false
		}
		if value, exists := fields["include_next_steps"]; exists {
			var include bool
			if json.Unmarshal(value, &include) != nil {
				return false
			}
			delete(fields, "include_next_steps")
			raw, _ = json.Marshal(fields)
		}
	}
	fields, ok := mcpStringFields(raw)
	if !ok {
		return false
	}
	if name == MCPQueryOrders {
		for key := range fields {
			if key != "date_basis" && key != "metric" && key != "metric_cursor" && key != "analysis" && key != "queue" && key != "status" && key != "category" && key != "number" && key != "period" && key != "location" && key != "keyword" && key != "date_from" && key != "date_to" {
				return false
			}
		}
		var q OrderQuery
		return json.Unmarshal(raw, &q) == nil && q.valid()
	}
	if name != MCPDispatch {
		return len(fields) == 0
	}
	if mode, ok := fields["worklist"]; ok {
		return len(fields) == 1 && validWorklist(mode)
	}
	_, hasCategory := fields["category"]
	return hasCategory && len(fields) == 1 && categoryOK(fields["category"])
}

func mcpStringFields(raw json.RawMessage) (map[string]string, bool) {
	fields := map[string]string{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, false
		}
		key, ok := token.(string)
		if !ok {
			return nil, false
		}
		if _, exists := fields[key]; exists {
			return nil, false
		}
		var value any
		if decoder.Decode(&value) != nil {
			return nil, false
		}
		text, ok := value.(string)
		if !ok {
			return nil, false
		}
		fields[key] = text
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, false
	}

	return fields, true
}

type mcpCategoryInput struct {
	Scope    *OrderQuery `json:"scope,omitempty"`
	Category string      `json:"category,omitempty"`
	Worklist string      `json:"worklist,omitempty"`
}

func mcpQuery(ctx context.Context, db *sql.DB, u User, name, category string) (map[string]any, error) {
	switch name {
	case MCPMyOrders, MCPAssignedOrders:
		column := "requester_id"
		if name == MCPAssignedOrders {
			column = "assignee_id"
		}
		rows, err := db.QueryContext(ctx, "SELECT id,number,status,category,location,created_at,updated_at FROM work_orders WHERE org_id=? AND "+column+"=? ORDER BY id DESC LIMIT 20", u.OrgID, u.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id int64
			var number, status, cat, location string
			var created, updated time.Time
			if err = rows.Scan(&id, &number, &status, &cat, &location, &created, &updated); err != nil {
				return nil, err
			}
			items = append(items, map[string]any{"id": id, "number": number, "status": status, "category": cat, "location": location, "created_at": created, "updated_at": updated})
		}
		return map[string]any{"items": items}, rows.Err()
	case MCPDispatch:
		if !categoryOK(category) {
			return nil, errors.New("invalid category")
		}
		items, err := dispatchCandidates(ctx, db, u, category)
		return map[string]any{"items": items}, err
	}
	return nil, errors.New("unknown tool")
}

func dispatchCandidates(ctx context.Context, db *sql.DB, u User, category string) ([]BusinessCandidate, error) {
	rows, err := db.QueryContext(ctx, `SELECT u.id,u.display_name,
			COALESCE(SUM(CASE WHEN w.status='assigned' THEN 1 ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN w.status='in_progress' THEN 1 ELSE 0 END),0)
			FROM users u JOIN technician_categories tc ON tc.user_id=u.id AND tc.category=?
			LEFT JOIN work_orders w ON w.assignee_id=u.id AND w.org_id=u.org_id AND w.status IN ('assigned','in_progress')
			WHERE u.org_id=? AND u.role='technician' AND u.enabled=TRUE
			GROUP BY u.id,u.display_name ORDER BY COUNT(w.id),u.id`, category, u.OrgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BusinessCandidate{}
	for rows.Next() {
		var id int64
		var display string
		var pending, progress int
		if err = rows.Scan(&id, &display, &pending, &progress); err != nil {
			return nil, err
		}
		items = append(items, BusinessCandidate{ID: id, Name: display, Categories: []string{category}, Pending: pending, Progress: progress})
	}
	return items, rows.Err()
}

// MCPHandler is mounted only on the internal Compose network. A signed API
// credential is required even for initialize and tools/list.
func MCPHandler(db *sql.DB, secret string) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "campus-repair", Version: "1.0.0"}, nil)
	tool := func(ctx context.Context, req *mcp.CallToolRequest, name, category string) (*mcp.CallToolResult, map[string]any, error) {
		credential, _ := req.Params.Meta["campus_repair_auth"].(string)
		c, err := verifyMCPCall(secret, credential)
		if err != nil {
			return nil, nil, errors.New("MCP caller authentication failed")
		}
		started := time.Now()
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		u, err := mcpIdentity(ctx, db, c)
		outcome := "denied"
		if err != nil && err != sql.ErrNoRows {
			outcome = "failed"
		}
		if err == nil {
			err = reserveMCPCall(ctx, db, c)
			if err != nil && !errors.Is(err, errMCPToolLimit) {
				outcome = "failed"
			}
		}
		if err == nil && !validMCPArguments(req.Params.Arguments, name) {
			err = errors.New("invalid tool arguments")
		}
		if err == nil {
			err = mcpAllowed(ctx, db, c, u, name)
		}
		var output map[string]any
		if err == nil {
			if name == MCPQueryOrders {
				var q OrderQuery
				err = json.Unmarshal(req.Params.Arguments, &q)
				if err == nil {
					var result BusinessResult
					result, err = queryBusinessOrders(ctx, db, u, q)
					if err == nil {
						var raw []byte
						raw, err = json.Marshal(result)
						if err == nil {
							err = json.Unmarshal(raw, &output)
						}
					}
				}
			} else if name == MCPDispatch && category == "" {
				var args mcpCategoryInput
				err = json.Unmarshal(req.Params.Arguments, &args)
				if err == nil && c.Skill != "dispatch_advice" {
					err = denied()
				}
				if err == nil {
					// The compound result also reads orders. Require that separate agent binding.
					err = mcpAllowed(ctx, db, mcpClaims{AgentID: c.AgentID}, u, MCPQueryOrders)
				}
				if err == nil {
					var result BusinessResult
					result, err = adminWorklist(ctx, db, u, args.Worklist, args.Scope)
					output = map[string]any{"worklist": result}
				}
			} else if name == MCPOrderDetail || name == MCPOrderVisit || name == MCPKnowledge {
				output, err = extendedMCPQuery(ctx, db, u, c, name, req.Params.Arguments)
			} else {
				output, err = mcpQuery(ctx, db, u, name, category)
			}
			if err != nil {
				outcome = "failed"
			}
		}
		if err == nil {
			outcome = "success"
		}
		if ctx.Err() != nil {
			outcome = "failed"
		}
		meta := map[string]any{"tool": name, "duration_ms": time.Since(started).Milliseconds()}
		if name == MCPQueryOrders && validMCPArguments(req.Params.Arguments, name) {
			var q OrderQuery
			if json.Unmarshal(req.Params.Arguments, &q) == nil {
				meta["query"] = map[string]any{"analysis": q.Analysis, "queue": q.Queue, "status": q.Status, "category": q.Category, "period": q.Period, "has_location": q.Location != "", "has_keyword": q.Keyword != "", "has_number": q.Number != "", "has_dates": q.DateFrom != "", "has_cursor": q.MetricCursor != ""}
			}
		}
		if category != "" {
			meta["category"] = category
		}
		var actor *User
		if u.ID > 0 {
			actor = &u
		}
		auditCtx := ctx
		if ctx.Err() != nil {
			var stop context.CancelFunc
			auditCtx, stop = context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
		}
		if e := writeAudit(auditCtx, db, c.Request, actor, "mcp.call", nil, outcome, meta); e != nil && err == nil {
			err = errors.New("tool audit unavailable")
		}
		if err != nil {
			return nil, nil, errors.New("tool unavailable or not authorized")
		}
		return nil, output, nil
	}
	mcp.AddTool(server, &mcp.Tool{Name: MCPMyOrders, Description: "查看当前报修人自己的工单进度"}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		return tool(ctx, req, MCPMyOrders, "")
	})
	mcp.AddTool(server, &mcp.Tool{Name: MCPAssignedOrders, Description: "查看当前维修人员已分配工单"}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		return tool(ctx, req, MCPAssignedOrders, "")
	})
	mcp.AddTool(server, &mcp.Tool{Name: MCPDispatch, Description: "管理员查看指定类别的可派维修人员及负载"}, func(ctx context.Context, req *mcp.CallToolRequest, in mcpCategoryInput) (*mcp.CallToolResult, map[string]any, error) {
		return tool(ctx, req, MCPDispatch, in.Category)
	})
	mcp.AddTool(server, &mcp.Tool{Name: MCPQueryOrders, Description: "查询授权工单，或汇总高发问题、积压和真实上门异常；仅限当前登录角色范围"}, func(ctx context.Context, req *mcp.CallToolRequest, _ OrderQuery) (*mcp.CallToolResult, map[string]any, error) {
		return tool(ctx, req, MCPQueryOrders, "")
	})
	mcp.AddTool(server, &mcp.Tool{Name: MCPOrderDetail, Description: "读取当前授权工单详情及按需分页处理时间线"}, func(ctx context.Context, req *mcp.CallToolRequest, _ DetailInput) (*mcp.CallToolResult, map[string]any, error) {
		return tool(ctx, req, MCPOrderDetail, "")
	})
	mcp.AddTool(server, &mcp.Tool{Name: MCPOrderVisit, Description: "读取当前授权工单方便时间、预约和上门记录"}, func(ctx context.Context, req *mcp.CallToolRequest, _ VisitInput) (*mcp.CallToolResult, map[string]any, error) {
		return tool(ctx, req, MCPOrderVisit, "")
	})
	mcp.AddTool(server, &mcp.Tool{Name: MCPKnowledge, Description: "检索本助手当前绑定的授权知识材料"}, func(ctx context.Context, req *mcp.CallToolRequest, _ KnowledgeInput) (*mcp.CallToolResult, map[string]any, error) {
		return tool(ctx, req, MCPKnowledge, "")
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{JSONResponse: true, SessionTimeout: time.Minute})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		c, err := verifyMCPCall(secret, parts[1])
		var u User
		if err == nil {
			u, err = mcpIdentity(r.Context(), db, c)
		}
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			body, readErr := io.ReadAll(io.LimitReader(r.Body, 64<<10))
			if readErr != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var message struct {
				Method string `json:"method"`
				Params struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"params"`
			}
			if json.Unmarshal(body, &message) == nil && message.Method == "tools/call" && (mcpRoles[message.Params.Name] == "" || !validMCPArguments(message.Params.Arguments, message.Params.Name)) {
				_ = writeAudit(r.Context(), db, c.Request, &u, "mcp.call", nil, "denied", map[string]any{"tool": message.Params.Name, "reason": "unknown_tool_or_invalid_arguments"})
			}
		}
		handler.ServeHTTP(w, r)
	})
}
