package app

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

type Agent struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Role      string  `json:"role"`
	Prompt    string  `json:"prompt,omitempty"`
	Enabled   bool    `json:"enabled"`
	Version   int64   `json:"version"`
	Materials []int64 `json:"material_ids"`
}

const agentColumns = "id,name,role,prompt,enabled,version"

func scanAgent(row scanner) (Agent, error) {
	var a Agent
	err := row.Scan(&a.ID, &a.Name, &a.Role, &a.Prompt, &a.Enabled, &a.Version)
	return a, err
}
func (s *Server) agent(ctx context.Context, u User, id int64, management bool) (Agent, error) {
	a, err := scanAgent(s.DB.QueryRowContext(ctx, "SELECT "+agentColumns+" FROM agents WHERE id=? AND org_id=?", id, u.OrgID))
	if err != nil {
		return a, dbError(err)
	}
	if !management {
		if a.Role != u.Role {
			return a, invisible()
		}
		if !a.Enabled {
			return a, &Problem{409, "agent_disabled", "该助手已停用，请选择其他助手或继续人工报修"}
		}
	}
	return a, nil
}
func (s *Server) agents(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	manage := r.URL.Query().Get("manage") == "1"
	if manage && u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	q := "org_id=?"
	args := []any{u.OrgID}
	if !manage {
		q += " AND role=? AND enabled=TRUE"
		args = append(args, u.Role)
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT "+agentColumns+" FROM agents WHERE "+q+" ORDER BY id", args...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items := []Agent{}
	for rows.Next() {
		a, e := scanAgent(rows)
		if e != nil {
			rows.Close()
			s.fail(w, r, e)
			return
		}
		if !manage {
			a.Prompt = ""
		}
		a.Materials = []int64{}
		items = append(items, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if manage {
		for i := range items {
			rows, err = s.DB.QueryContext(r.Context(), "SELECT am.material_id FROM agent_materials am JOIN knowledge_materials m ON m.id=am.material_id WHERE am.agent_id=? AND m.org_id=? ORDER BY am.material_id", items[i].ID, u.OrgID)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			for rows.Next() {
				var id int64
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					s.fail(w, r, err)
					return
				}
				items[i].Materials = append(items[i].Materials, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				s.fail(w, r, err)
				return
			}
		}
	}
	send(w, 200, map[string]any{"items": items})
}
func (s *Server) updateAgent(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	if u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	id, err := parseID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var a Agent
	if err = decode(w, r, &a); err != nil {
		s.fail(w, r, err)
		return
	}
	a.Name = strings.TrimSpace(a.Name)
	a.Prompt = strings.TrimSpace(a.Prompt)
	if a.Name == "" || utf8.RuneCountInString(a.Name) > 100 || a.Prompt == "" || utf8.RuneCountInString(a.Prompt) > 4000 || (a.Role != "requester" && a.Role != "technician" && a.Role != "admin") || len(a.Materials) > 100 {
		s.fail(w, r, bad("请填写名称（100字内）、提示词（4000字内）、角色及最多100份材料"))
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	var version int64
	err = tx.QueryRowContext(r.Context(), "SELECT version FROM agents WHERE id=? AND org_id=? FOR UPDATE", id, u.OrgID).Scan(&version)
	if err != nil {
		s.fail(w, r, dbError(err))
		return
	}
	if version != a.Version {
		s.fail(w, r, &Problem{409, "conflict", "助手配置已变化，请刷新后重试"})
		return
	}
	seen := map[int64]bool{}
	for _, mid := range a.Materials {
		var exists int64
		if mid < 1 || seen[mid] {
			s.fail(w, r, bad("材料编号无效或重复"))
			return
		}
		seen[mid] = true
		err = tx.QueryRowContext(r.Context(), "SELECT id FROM knowledge_materials WHERE id=? AND org_id=?", mid, u.OrgID).Scan(&exists)
		if err == sql.ErrNoRows {
			s.fail(w, r, bad("所选材料不存在或不可用"))
			return
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
	}
	_, err = tx.ExecContext(r.Context(), "UPDATE agents SET name=?,role=?,prompt=?,enabled=?,version=version+1 WHERE id=?", a.Name, a.Role, a.Prompt, a.Enabled, id)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), "DELETE FROM agent_materials WHERE agent_id=?", id)
	}
	for _, mid := range a.Materials {
		if err != nil {
			break
		}
		_, err = tx.ExecContext(r.Context(), "INSERT INTO agent_materials VALUES(?,?)", id, mid)
	}
	if err == nil {
		err = writeAudit(r.Context(), tx, requestID(r), &u, "agent.update", id, "success", map[string]any{"version": version + 1, "material_count": len(a.Materials), "enabled": a.Enabled})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	a.ID = id
	a.Version = version + 1
	send(w, 200, map[string]any{"agent": a})
}
func agentParam(r *http.Request) (int64, error) {
	v := r.URL.Query().Get("agent_id")
	if v == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id < 1 {
		return 0, bad("助手编号无效")
	}
	return id, nil
}

// This SQL scope is reused for retrieval, final citations and original text access.
func knowledgeScope(u User, agent int64) (string, []any) {
	q := "m.org_id=?"
	args := []any{u.OrgID}
	if agent > 0 {
		q += " AND EXISTS(SELECT 1 FROM agent_materials am JOIN agents a ON a.id=am.agent_id WHERE am.material_id=m.id AND a.id=? AND a.org_id=m.org_id AND a.role=? AND a.enabled=TRUE)"
		args = append(args, agent, u.Role)
	}
	return q, args
}
