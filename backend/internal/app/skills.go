package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
)

type SkillDefinition struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	DefinitionVersion int      `json:"definition_version"`
	Roles             []string `json:"roles"`
	Purpose           string   `json:"purpose"`
	Trigger           string   `json:"trigger"`
	RequiredFields    []string `json:"required_fields"`
	AllowedTools      []string `json:"allowed_tools"`
	Instruction       string   `json:"instruction"`
	Output            string   `json:"output_contract"`
}

type SkillConfig struct {
	SkillDefinition
	Enabled bool  `json:"enabled"`
	Version int64 `json:"version"`
}

type SkillBinding struct {
	SkillID string `json:"skill_id"`
	Enabled bool   `json:"enabled"`
}

var skillDefinitions = []SkillDefinition{
	{"repair_intake", "故障信息补全", 2, []string{"requester"}, "整理报修草稿供本人确认", "用户请求报修或补充当前有效草稿", []string{"category", "location", "description"}, []string{}, "结合本人有效上下文补全报修信息，每轮只追问一至两个缺失字段，已知信息不重复询问。结合授权材料给出相关安全提醒，从用户原文择取真实现象并整理成简洁草稿。", "可编辑草稿或必要追问，创建工单须本人点击确认"},
	{"safe_self_check", "安全自查建议", 3, []string{"requester", "technician", "admin"}, "根据授权材料解释安全提醒", "用户询问安全自查或安全资料", []string{}, []string{"search_repair_knowledge"}, "根据具体风险和本轮授权资料解释可观察的现象、何时停止使用以及如何人工求助。按问题决定表达方式。危险动作的禁止提醒用独立简单句，使用或转述材料时在对应句末标注实际来源。禁止提供拆电路、接线及带电维修步骤。", "简短安全建议及实际引用，无依据明确未核实"},
	{"dispatch_advice", "派单辅助", 2, []string{"admin"}, "根据类别和当前负载提供人工安排依据", "管理员请求人选或逐单分流建议", []string{"category"}, []string{MCPDispatch}, "查某类人员且缺类别时追问；逐单建议使用工单自身类别和已核实的候选负载。分流时引用报修线索、缺失信息和下一步，未知出勤不推断，管理员可在派单建议卡确认。", "真实候选、理由及人工工单入口"},
}

func skillDefinition(id string) (SkillDefinition, bool) {
	for _, definition := range skillDefinitions {
		if definition.ID == id {
			return definition, true
		}
	}
	return SkillDefinition{}, false
}

func skillRoleAllowed(id, role string) bool {
	definition, ok := skillDefinition(id)
	if !ok {
		return false
	}
	return queryContains(definition.Roles, role)
}

// Seed bindings are installed in the same transaction as the migration marker.
// Subsequent startups preserve administrator removals and disablement.
func initializeSkills(ctx context.Context, tx *sql.Tx) error {
	var migrated int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=9").Scan(&migrated); err != nil {
		return err
	}
	if migrated != 0 {
		return nil
	}
	for _, definition := range skillDefinitions {
		if _, err := tx.ExecContext(ctx, "INSERT INTO skills(org_id,skill_id,enabled,version) SELECT id,?,TRUE,1 FROM organizations", definition.ID); err != nil {
			return err
		}
		for _, role := range definition.Roles {
			if _, err := tx.ExecContext(ctx, "INSERT INTO agent_skills(agent_id,skill_id,enabled) SELECT id,?,TRUE FROM agents WHERE seed_key=role AND role=?", definition.ID, role); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE agents SET version=version+1 WHERE seed_key=role"); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(9,UTC_TIMESTAMP(6))")
	return err
}

type skillQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadSkillBindings(ctx context.Context, q skillQuerier, agentID int64) ([]SkillBinding, error) {
	rows, err := q.QueryContext(ctx, "SELECT skill_id,enabled FROM agent_skills WHERE agent_id=? ORDER BY skill_id", agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bindings := []SkillBinding{}
	for rows.Next() {
		var binding SkillBinding
		if err := rows.Scan(&binding.SkillID, &binding.Enabled); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}

func (s *Server) skills(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	if u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT skill_id,enabled,version FROM skills WHERE org_id=? ORDER BY skill_id", u.OrgID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []SkillConfig{}
	for rows.Next() {
		var id string
		var enabled bool
		var version int64
		if err := rows.Scan(&id, &enabled, &version); err != nil {
			s.fail(w, r, err)
			return
		}
		if definition, ok := skillDefinition(id); ok {
			items = append(items, SkillConfig{definition, enabled, version})
		}
	}
	if err := rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"items": items})
}

func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	if u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	id := r.PathValue("skill")
	definition, ok := skillDefinition(id)
	if !ok {
		s.fail(w, r, invisible())
		return
	}
	var input struct {
		Enabled bool  `json:"enabled"`
		Version int64 `json:"version"`
	}
	if err := decode(w, r, &input); err != nil {
		s.fail(w, r, err)
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	var version int64
	err = tx.QueryRowContext(r.Context(), "SELECT version FROM skills WHERE org_id=? AND skill_id=? FOR UPDATE", u.OrgID, id).Scan(&version)
	if err != nil {
		s.fail(w, r, dbError(err))
		return
	}
	if version != input.Version {
		s.fail(w, r, &Problem{409, "conflict", "技能配置已变化，请刷新后重试"})
		return
	}
	_, err = tx.ExecContext(r.Context(), "UPDATE skills SET enabled=?,version=version+1 WHERE org_id=? AND skill_id=?", input.Enabled, u.OrgID, id)
	if err == nil {
		err = writeAudit(r.Context(), tx, requestID(r), &u, "skill.update", nil, "success", map[string]any{"skill": id, "version": version + 1, "enabled": input.Enabled})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"skill": SkillConfig{definition, input.Enabled, version + 1}})
}

type skillContextKey struct{}

type SkillRun struct {
	ID                string `json:"skill_id"`
	DefinitionVersion int    `json:"skill_version"`
	ConfigVersion     int64  `json:"config_version"`
}

func (s *Server) selectSkill(ctx context.Context, u User, agent Agent, input string, previous *RepairDraft, toolName string, requested ...string) (*SkillRun, error) {
	ids := []string{}
	if (toolName == "" || draftIntent(u.Role, input, false)) && draftIntent(u.Role, input, previous != nil) {
		ids = append(ids, "repair_intake")
	} else if (toolName == "" || toolName == MCPKnowledge || toolName == MCPDispatch) && containsAny(input, "安全", "自查", "带电", "拆电路", "拆卸", "接线", "漏电", "冒烟", "触电", "危险") {
		ids = append(ids, "safe_self_check")
	}
	if u.Role == "admin" && (toolName == MCPDispatch || dispatchQuestion(input)) {
		ids = append(ids, "dispatch_advice")
	}
	if len(requested) > 0 && requested[0] != "" {
		if !skillRoleAllowed(requested[0], u.Role) {
			return nil, denied()
		}
		ids = []string{requested[0]}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 1 {
		names := []string{}
		for _, id := range ids {
			definition, _ := skillDefinition(id)
			names = append(names, definition.Name)
		}
		return nil, &Problem{409, "skill_clarification", "你想先处理哪一项？可以选择" + strings.Join(names, "或") + "。"}
	}
	id := ids[0]
	var enabled, bound bool
	var version int64
	err := s.DB.QueryRowContext(ctx, "SELECT s.enabled,b.enabled,s.version FROM skills s JOIN agent_skills b ON b.skill_id=s.skill_id JOIN agents a ON a.id=b.agent_id AND a.org_id=s.org_id WHERE s.org_id=? AND s.skill_id=? AND a.id=?", u.OrgID, id, agent.ID).Scan(&enabled, &bound, &version)
	if err == sql.ErrNoRows || err == nil && (!enabled || !bound || !skillRoleAllowed(id, u.Role)) {
		return nil, &Problem{409, "skill_disabled", "所需技能未启用或未绑定，请在工单页面继续操作"}
	}
	if err != nil {
		return nil, err
	}
	definition, _ := skillDefinition(id)
	for _, tool := range definition.AllowedTools {
		if err := mcpAllowed(ctx, s.DB, mcpClaims{AgentID: agent.ID}, u, tool); err != nil {
			return nil, &Problem{409, "skill_dependency_unavailable", "所需技能缺少已启用的工具绑定，请联系管理员或使用工单页面"}
		}
	}
	return &SkillRun{id, definition.DefinitionVersion, version}, nil
}

func skillPrompt(run *SkillRun) string {
	if run == nil {
		return ""
	}
	definition, ok := skillDefinition(run.ID)
	if !ok {
		return ""
	}
	return fmt.Sprintf("\n本轮业务技能：%s，定义版本%d，配置版本%d。", definition.Name, run.DefinitionVersion, run.ConfigVersion) + definition.Instruction + "\n输出约定：" + definition.Output
}
