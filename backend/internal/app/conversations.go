package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

type Conversation struct {
	ID        int64     `json:"id"`
	AgentID   int64     `json:"agent_id"`
	Title     string    `json:"title"`
	AgentName string    `json:"agent_name"`
	Updated   time.Time `json:"updated_at"`
}
type Message struct {
	ID        int64           `json:"id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Status    string          `json:"status"`
	Warning   string          `json:"warning"`
	Citations []Citation      `json:"citations"`
	Created   time.Time       `json:"created_at"`
	Refs      []CitationRef   `json:"-"`
	Draft     *RepairDraft    `json:"draft,omitempty"`
	Business  *BusinessResult `json:"business,omitempty"`
	Skill     *SkillRun       `json:"skill,omitempty"`
}

func (s *Server) ownConversation(ctx context.Context, u User, id int64) (Conversation, error) {
	var c Conversation
	err := s.DB.QueryRowContext(ctx, "SELECT c.id,c.agent_id,c.title,a.name,c.updated_at FROM conversations c JOIN agents a ON a.id=c.agent_id AND a.org_id=c.org_id WHERE c.id=? AND c.org_id=? AND c.owner_id=?", id, u.OrgID, u.ID).Scan(&c.ID, &c.AgentID, &c.Title, &c.AgentName, &c.Updated)
	if err != nil {
		return c, dbError(err)
	}
	return c, nil
}
func (s *Server) conversations(w http.ResponseWriter, r *http.Request) {
	if err := s.recoverExpired(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	page, size, err := pagination(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u := current(r)
	keyword := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(keyword) > 100 {
		s.fail(w, r, bad("搜索词最多 100 个字"))
		return
	}
	where := " WHERE c.org_id=? AND c.owner_id=?"
	args := []any{u.OrgID, u.ID}
	if keyword != "" {
		where += " AND LOCATE(?,c.title)>0"
		args = append(args, keyword)
	}
	var total int
	if err = s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM conversations c"+where, args...).Scan(&total); err != nil {
		s.fail(w, r, err)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT c.id,c.agent_id,c.title,a.name,c.updated_at FROM conversations c JOIN agents a ON a.id=c.agent_id AND a.org_id=c.org_id"+where+" ORDER BY c.updated_at DESC,c.id DESC LIMIT ? OFFSET ?", append(args, size, (page-1)*size)...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []Conversation{}
	for rows.Next() {
		var c Conversation
		if err = rows.Scan(&c.ID, &c.AgentID, &c.Title, &c.AgentName, &c.Updated); err != nil {
			s.fail(w, r, err)
			return
		}
		items = append(items, c)
	}
	if err = rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"items": items, "total": total, "page": page})
}
func (s *Server) createConversation(w http.ResponseWriter, r *http.Request) {
	var input struct {
		AgentID int64 `json:"agent_id"`
	}
	if err := decode(w, r, &input); err != nil {
		s.fail(w, r, err)
		return
	}
	u := current(r)
	// Serialize creation with configuration updates, including disabling and changing audience.
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	a, err := scanAgent(tx.QueryRowContext(r.Context(), "SELECT "+agentColumns+" FROM agents WHERE id=? AND org_id=? FOR SHARE", input.AgentID, u.OrgID))
	if err != nil {
		s.fail(w, r, dbError(err))
		return
	}
	if a.Role != u.Role {
		s.fail(w, r, invisible())
		return
	}
	if !a.Enabled {
		s.fail(w, r, &Problem{409, "agent_disabled", "该助手已停用"})
		return
	}
	res, err := tx.ExecContext(r.Context(), "INSERT INTO conversations(org_id,owner_id,agent_id,created_at,updated_at) VALUES(?,?,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))", u.OrgID, u.ID, a.ID)
	var id int64
	if err == nil {
		id, err = res.LastInsertId()
	}
	if err == nil {
		err = writeAudit(r.Context(), tx, requestID(r), &u, "conversation.create", id, "success", map[string]any{"agent_id": a.ID})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 201, map[string]any{"conversation": Conversation{ID: id, AgentID: a.ID, Title: "新对话", AgentName: a.Name}})
}
func scanMessage(row scanner) (Message, error) {
	var m Message
	var raw []byte
	err := row.Scan(&m.ID, &m.Role, &m.Content, &m.Status, &m.Warning, &raw, &m.Created)
	if err == nil {
		err = json.Unmarshal(raw, &m.Refs)
	}
	m.Citations = []Citation{}
	return m, err
}

const messageColumns = "id,role,content,status,warning,citations,created_at"

func (s *Server) loadMessage(ctx context.Context, cid, mid int64) (Message, error) {
	return scanMessage(s.DB.QueryRowContext(ctx, "SELECT "+messageColumns+" FROM messages WHERE conversation_id=? AND id=?", cid, mid))
}
func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u := current(r)
	c, err := s.ownConversation(r.Context(), u, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err = s.recoverExpired(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	page, size, err := pagination(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var total int
	if err = s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM messages WHERE conversation_id=?", id).Scan(&total); err != nil {
		s.fail(w, r, err)
		return
	}
	// Most recent page, in chronological order inside that page.
	rows, err := s.DB.QueryContext(r.Context(), "SELECT "+messageColumns+" FROM messages WHERE conversation_id=? ORDER BY id DESC LIMIT ? OFFSET ?", id, size, (page-1)*size)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items := []Message{}
	for rows.Next() {
		m, e := scanMessage(rows)
		if e != nil {
			rows.Close()
			s.fail(w, r, e)
			return
		}
		items = append(items, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for i := range items {
		m := &items[i]
		if m.Role == "assistant" && m.Status == "complete" {
			var run SkillRun
			skillErr := s.DB.QueryRowContext(r.Context(), "SELECT skill_id,definition_version,config_version FROM message_skill_runs WHERE message_id=?", m.ID).Scan(&run.ID, &run.DefinitionVersion, &run.ConfigVersion)
			if skillErr != nil && skillErr != sql.ErrNoRows {
				s.fail(w, r, skillErr)
				return
			}
			if skillErr == nil {
				m.Skill = &run
			}

			m.Business, err = s.businessHistory(r.Context(), u, id, m.ID, c.AgentID)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			m.Draft, err = s.messageDraft(r.Context(), id, m.ID)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			content, citations, warning, e := s.verifyReferences(r.Context(), u, c.AgentID, m.Content, m.Refs)
			if e != nil {
				s.fail(w, r, e)
				return
			}
			if m.Business != nil && m.Business.Unavailable {
				content = "这张工单当前不可查看。"
				citations = []Citation{}
			}
			if u.Role == "technician" && m.Business != nil && m.Business.Kind == "orders" &&
				(m.Business.Warning == partialExplanationNotice || m.Business.Warning == unverifiedExplanationNotice) {
				content = technicianOrderSummary(*m.Business)
				m.Business.Warning = ""
				m.Business.Natural = false
			}
			m.Content = content
			m.Citations = citations
			if warning != "" {
				m.Warning = warning
			}
		}
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	send(w, 200, map[string]any{"conversation": c, "items": items, "total": total, "page": page})
}
func (s *Server) recoverExpired(ctx context.Context) error {
	// Lock conversations in the same order as beginTurn and finishTurn. Concurrent
	// readers should not upgrade shared locks over every healthy conversation.
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id,generation_token FROM conversations WHERE generation_token<>'' AND generation_until<UTC_TIMESTAMP(6) ORDER BY id FOR UPDATE")
	if err != nil {
		return err
	}
	type lease struct {
		id    int64
		token string
	}
	var expired []lease
	for rows.Next() {
		var l lease
		if err = rows.Scan(&l.id, &l.token); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, l)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, l := range expired {
		_, err = tx.ExecContext(ctx, "UPDATE messages SET status='failed',warning='上次生成已中断，请重新提问' WHERE conversation_id=? AND turn_token=? AND status IN ('pending','generating')", l.id, l.token)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE conversations SET generation_token='',generation_until=NULL WHERE id=?", l.id)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Server) RecoverConversations(ctx context.Context) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_ = s.recoverExpired(ctx)
		}
	}
}

type chatMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	ToolCalls  []gatewayToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}
type Turn struct {
	ConversationID, MessageID int64
	Token                     string
	SessionHash               string
	Agent                     Agent
	History                   []chatMessage
	Draft                     *Input
	Business                  *BusinessResult
	Skill                     *SkillRun
	Performance               *turnPerformance
}

func (s *Server) beginTurn(ctx context.Context, u User, c Conversation, input string, request string, reviewSource, reviewOrder int64) (Turn, error) {
	var turn Turn
	turn.ConversationID = c.ID
	turn.Token = randomHex(16)
	if err := s.recoverExpired(ctx); err != nil {
		return turn, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return turn, err
	}
	defer tx.Rollback()
	var locked string
	var updated time.Time
	err = tx.QueryRowContext(ctx, "SELECT generation_token,updated_at FROM conversations WHERE id=? AND org_id=? AND owner_id=? FOR UPDATE", c.ID, u.OrgID, u.ID).Scan(&locked, &updated)
	if err != nil {
		return turn, dbError(err)
	}
	if locked != "" {
		return turn, &Problem{409, "generating", "这段对话正在生成回答，请等待完成或停止生成"}
	}
	if reviewSource != 0 && !updated.Equal(c.Updated) {
		return turn, &Problem{409, "conversation_changed", "这段对话刚有新消息，请重新读取后追问"}
	}
	turn.Agent, err = scanAgent(tx.QueryRowContext(ctx, "SELECT "+agentColumns+" FROM agents WHERE id=? AND org_id=? FOR SHARE", c.AgentID, u.OrgID))
	if err != nil {
		return turn, dbError(err)
	}
	if turn.Agent.Role != u.Role {
		return turn, invisible()
	}
	if !turn.Agent.Enabled {
		return turn, &Problem{409, "agent_disabled", "该助手已停用，请返回人工报修"}
	}
	rows, err := tx.QueryContext(ctx, "SELECT m.role,CASE WHEN b.message_id IS NULL OR (m.id=? AND ?=0) OR (JSON_UNQUOTE(JSON_EXTRACT(b.result,'$.kind'))='discuss_pre_review' AND JSON_EXTRACT(b.result,'$.review_source')=? AND (?=0 OR JSON_EXTRACT(b.result,'$.review_order')=?)) THEN m.content ELSE '历史业务结果须按当前权限重新查询，不能作为本轮事实。' END FROM messages m LEFT JOIN assistant_business_results b ON b.message_id=m.id WHERE m.conversation_id=? AND m.status='complete' ORDER BY m.id DESC LIMIT 10", reviewSource, reviewOrder, reviewSource, reviewOrder, reviewOrder, c.ID)
	if err != nil {
		return turn, err
	}
	for rows.Next() {
		var m chatMessage
		if err = rows.Scan(&m.Role, &m.Content); err != nil {
			rows.Close()
			return turn, err
		}
		turn.History = append(turn.History, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return turn, err
	}
	for i, j := 0, len(turn.History)-1; i < j; i, j = i+1, j-1 {
		turn.History[i], turn.History[j] = turn.History[j], turn.History[i]
	}
	title := []rune(strings.TrimSpace(input))
	if len(title) > 40 {
		title = title[:40]
	}
	_, err = tx.ExecContext(ctx, "UPDATE conversations SET generation_token=?,generation_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 130 SECOND),updated_at=UTC_TIMESTAMP(6),title=IF(title='新对话',?,title) WHERE id=?", turn.Token, string(title), c.ID)
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO messages(conversation_id,turn_token,role,content,status,citations,agent_version,created_at) VALUES(?,?,'user',?,'pending','[]',?,UTC_TIMESTAMP(6))", c.ID, turn.Token, input, turn.Agent.Version)
	}
	if err == nil {
		var res sql.Result
		res, err = tx.ExecContext(ctx, "INSERT INTO messages(conversation_id,turn_token,role,content,status,citations,agent_version,created_at) VALUES(?,?,'assistant','','generating','[]',?,UTC_TIMESTAMP(6))", c.ID, turn.Token, turn.Agent.Version)
		if err == nil {
			turn.MessageID, err = res.LastInsertId()
		}
	}
	if err == nil {
		err = writeAudit(ctx, tx, request, &u, "conversation.generate", c.ID, "started", map[string]any{"agent_id": c.AgentID, "version": turn.Agent.Version})
	}
	if err == nil {
		err = tx.Commit()
	}
	return turn, err
}
func validQuestion(text string) bool {
	return strings.TrimSpace(text) != "" && utf8.ValidString(text) && utf8.RuneCountInString(text) <= 2000
}
func (s *Server) savePartial(ctx context.Context, t Turn, text string) error {
	res, err := s.DB.ExecContext(ctx, "UPDATE messages m JOIN conversations c ON c.id=m.conversation_id SET m.content=? WHERE m.id=? AND m.turn_token=? AND m.status='generating' AND c.generation_token=? AND c.generation_until>UTC_TIMESTAMP(6)", text, t.MessageID, t.Token, t.Token)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return &Problem{409, "generation_lost", "本轮生成已失效，请重新提问"}
	}
	return nil
}
func (s *Server) finishTurn(t Turn, u User, status, text, warning, request string, refs []CitationRef) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer recordStage(context.WithValue(ctx, performanceKey{}, t.Performance), "finalize", time.Now())
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var token string
	err = tx.QueryRowContext(ctx, "SELECT generation_token FROM conversations WHERE id=? AND generation_until>UTC_TIMESTAMP(6) FOR UPDATE", t.ConversationID).Scan(&token)
	if err != nil || token != t.Token {
		return &Problem{409, "generation_lost", "本轮生成已中断，请重新读取历史"}
	}
	if status == "complete" {
		var version int64
		var enabled bool
		var role string
		err = tx.QueryRowContext(ctx, "SELECT version,enabled,role FROM agents WHERE id=? AND org_id=? FOR SHARE", t.Agent.ID, u.OrgID).Scan(&version, &enabled, &role)
		if err != nil || !enabled || role != u.Role || version != t.Agent.Version {
			return &Problem{409, "configuration_changed", "助手配置已变化，请重新提问"}
		}
		if t.Skill != nil {
			var enabled, bound bool
			var skillVersion int64
			err = tx.QueryRowContext(ctx, "SELECT sk.enabled,b.enabled,sk.version FROM skills sk JOIN agent_skills b ON b.skill_id=sk.skill_id JOIN agents a ON a.id=b.agent_id AND a.org_id=sk.org_id WHERE sk.org_id=? AND sk.skill_id=? AND a.id=? FOR SHARE", u.OrgID, t.Skill.ID, t.Agent.ID).Scan(&enabled, &bound, &skillVersion)
			definition, ok := skillDefinition(t.Skill.ID)
			if err != nil || !enabled || !bound || !ok || !skillRoleAllowed(t.Skill.ID, u.Role) || skillVersion != t.Skill.ConfigVersion || definition.DefinitionVersion != t.Skill.DefinitionVersion {
				return &Problem{409, "configuration_changed", "技能配置已变化，本轮结果未确认"}
			}
		}
		var live int64
		err = tx.QueryRowContext(ctx, "SELECT u.id FROM sessions se JOIN users u ON u.id=se.user_id WHERE se.token_hash=? AND se.expires_at>UTC_TIMESTAMP(6) AND u.id=? AND u.org_id=? AND u.role=? AND u.enabled=TRUE FOR SHARE", t.SessionHash, u.ID, u.OrgID, u.Role).Scan(&live)
		if err != nil {
			return &Problem{409, "identity_changed", "登录身份已失效，本轮回答未确认"}
		}
		if t.Business != nil {
			for _, item := range t.Business.evidenceOrders() {
				order, e := scanOrder(tx.QueryRowContext(ctx, "SELECT "+orderColumns+" FROM work_orders WHERE id=? FOR SHARE", item.ID))
				if e != nil || !visible(u, order) {
					return &Problem{409, "identity_changed", "工单归属已变化，本轮结果未确认"}
				}
				if item.Version > 0 && order.Version != item.Version {
					return conflict()
				}
				if item.Next != nil && item.Next.EvidenceID != fmt.Sprintf("order:%d:v%d", order.ID, order.Version) {
					return conflict()
				}
			}
		}
		scope, args := knowledgeScope(u, t.Agent.ID)
		for _, ref := range refs {
			var id int64
			err = tx.QueryRowContext(ctx, "SELECT c.id FROM knowledge_chunks c JOIN knowledge_materials m ON m.id=c.material_id AND m.published_generation=c.generation WHERE c.id=? AND c.generation=? AND "+scope+" FOR SHARE", append([]any{ref.ChunkID, ref.Generation}, args...)...).Scan(&id)
			if err != nil {
				return &Problem{409, "citation_changed", "材料来源已变化，请重新提问"}
			}
		}
	}
	if status == "complete" && t.Skill != nil {
		evidence := []any{}
		for _, ref := range refs {
			evidence = append(evidence, map[string]any{"chunk_id": ref.ChunkID, "generation": ref.Generation})
		}
		if t.Business != nil {
			for _, item := range t.Business.evidenceOrders() {
				evidence = append(evidence, map[string]any{"order_id": item.ID})
			}
			for _, candidate := range t.Business.Candidates {
				evidence = append(evidence, map[string]any{"candidate_id": candidate.ID})
			}
		}
		evidenceJSON, e := json.Marshal(evidence)
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO message_skill_runs(message_id,skill_id,definition_version,config_version,agent_version,evidence) VALUES(?,?,?,?,?,?)", t.MessageID, t.Skill.ID, t.Skill.DefinitionVersion, t.Skill.ConfigVersion, t.Agent.Version, string(evidenceJSON)); err != nil {
			return err
		}
		if err = writeAudit(ctx, tx, request, &u, "skill.execute", t.MessageID, "success", map[string]any{"skill": t.Skill.ID, "definition_version": t.Skill.DefinitionVersion, "config_version": t.Skill.ConfigVersion, "agent_version": t.Agent.Version}); err != nil {
			return err
		}
	}
	if status == "complete" && t.Business != nil {
		raw, e := json.Marshal(worklistSnapshot{BusinessResult: *t.Business, Rows: t.Business.worklistRows})
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO assistant_business_results(message_id,result) VALUES(?,?)", t.MessageID, string(raw)); e != nil {
			return e
		}
	}
	if status == "complete" && t.Draft != nil {
		if u.Role != "requester" {
			return denied()
		}
		_, err = tx.ExecContext(ctx, "UPDATE repair_drafts SET status='superseded',version=version+1 WHERE conversation_id=? AND status='active'", t.ConversationID)
		if err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO repair_drafts(message_id,conversation_id,category,location,description,idem_key) VALUES(?,?,?,?,?,?)", t.MessageID, t.ConversationID, t.Draft.Category, t.Draft.Location, t.Draft.Description, randomHex(24))
		}
		if err == nil {
			err = writeAudit(ctx, tx, request, &u, "draft.generate", t.MessageID, "success", map[string]any{"conversation_id": t.ConversationID})
		}
		if err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(refs)
	if refs == nil {
		raw = []byte("[]")
	}
	_, err = tx.ExecContext(ctx, "UPDATE messages SET status=?,content=?,warning=?,citations=? WHERE id=? AND turn_token=?", status, text, warning, string(raw), t.MessageID, t.Token)
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE messages SET status=? WHERE conversation_id=? AND turn_token=? AND role='user'", status, t.ConversationID, t.Token)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE conversations SET generation_token='',generation_until=NULL,updated_at=UTC_TIMESTAMP(6) WHERE id=?", t.ConversationID)
	}
	if err == nil {
		err = writeAudit(ctx, tx, request, &u, "conversation.finish", t.ConversationID, map[bool]string{true: "success", false: status}[status == "complete"], map[string]any{"message_id": t.MessageID, "citations": len(refs)})
	}
	if err == nil {
		err = tx.Commit()
	}
	return err
}
