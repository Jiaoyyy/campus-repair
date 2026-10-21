package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"
)

type Evidence struct {
	ID         int64  `json:"chunk_id"`
	MaterialID int64  `json:"material_id"`
	Generation int64  `json:"generation"`
	Title      string `json:"title"`
	Demo       bool   `json:"demo"`
	Chunk
	Score float64 `json:"score"`
}

func (s *Server) retrieval(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	agentID, err := agentParam(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if agentID > 0 {
		if _, err = s.agent(r.Context(), u, agentID, false); err != nil {
			s.fail(w, r, err)
			return
		}
	} else if u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "keyword"
	}
	hits, err := s.searchKnowledge(r.Context(), u, agentID, q, mode)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err = writeAudit(r.Context(), s.DB, requestID(r), &u, "knowledge.search", nil, "success", map[string]any{"mode": mode, "hits": len(hits), "agent_id": agentID}); err != nil {
		s.fail(w, r, err)
		return
	}
	scope := "organization_management"
	if agentID > 0 {
		scope = "agent_binding"
	}
	send(w, 200, map[string]any{"items": hits, "mode": mode, "scope": scope})
}
func (s *Server) searchKnowledge(ctx context.Context, u User, agentID int64, q, mode string) ([]Evidence, error) {
	if q == "" || utf8.RuneCountInString(q) > 500 {
		return nil, bad("检索词须为 1 至 500 字")
	}
	if mode != "keyword" && mode != "vector" && mode != "hybrid" {
		return nil, bad("检索模式无效")
	}
	scope, args := knowledgeScope(u, agentID)
	keyword, vector := []int64{}, []int64{}
	if mode != "vector" {
		rows, err := s.DB.QueryContext(ctx, "SELECT c.id FROM knowledge_chunks c JOIN knowledge_materials m ON m.id=c.material_id AND m.published_generation=c.generation WHERE "+scope+" AND MATCH(c.body) AGAINST(? IN NATURAL LANGUAGE MODE) ORDER BY MATCH(c.body) AGAINST(? IN NATURAL LANGUAGE MODE) DESC,c.id LIMIT 100", append(append([]any{}, args...), q, q)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			keyword = append(keyword, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if mode != "keyword" {
		v := vectorConfig()
		if !v.configured() {
			return nil, &Problem{503, "vector_unavailable", "向量检索未配置，可改用关键词检索"}
		}
		rows, err := s.DB.QueryContext(ctx, "SELECT m.id,m.vector_ready,m.vector_collection FROM knowledge_materials m WHERE "+scope+" AND m.published_generation>0", args...)
		if err != nil {
			return nil, err
		}
		materials := []int64{}
		incomplete := false
		for rows.Next() {
			var id int64
			var ready bool
			var collection string
			if err = rows.Scan(&id, &ready, &collection); err != nil {
				rows.Close()
				return nil, err
			}
			if !ready || collection != v.Collection {
				incomplete = true
			} else {
				materials = append(materials, id)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if incomplete {
			return nil, &Problem{503, "vector_unavailable", "部分材料尚无当前模型的向量索引，请重建或使用关键词检索"}
		}
		// Check the dependency even when no materials exist; an outage is not an empty result.
		if err = v.ensure(ctx); err == nil {
			vector, err = v.search(ctx, u.OrgID, q, materials)
		}
		if err != nil {
			return nil, &Problem{503, "vector_unavailable", "向量服务暂不可用，可改用关键词检索"}
		}
	}
	// Recheck authorization/current generation BEFORE calculating each route's RRF ranks.
	scores := map[int64]float64{}
	evidence := map[int64]Evidence{}
	for _, rank := range [][]int64{keyword, vector} {
		position := 0
		seen := map[int64]bool{}
		for _, id := range rank {
			if seen[id] {
				continue
			}
			seen[id] = true
			var e Evidence
			err := s.DB.QueryRowContext(ctx, "SELECT c.id,m.id,c.generation,m.title,m.demo,c.sequence_no,c.start_char,c.end_char,c.body FROM knowledge_chunks c JOIN knowledge_materials m ON m.id=c.material_id AND m.published_generation=c.generation WHERE c.id=? AND "+scope, append([]any{id}, args...)...).Scan(&e.ID, &e.MaterialID, &e.Generation, &e.Title, &e.Demo, &e.Sequence, &e.Start, &e.End, &e.Text)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			position++
			scores[id] += 1 / float64(60+position)
			evidence[id] = e
		}
	}
	hits := []Evidence{}
	for id, score := range scores {
		e := evidence[id]
		e.Score = score
		hits = append(hits, e)
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].ID < hits[j].ID
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > 4 {
		hits = hits[:4]
	}
	return hits, nil
}

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	v := vectorConfig()
	state := "unconfigured"
	if v.configured() {
		state = "configured"
		if v.ensure(r.Context()) != nil {
			state = "unavailable"
		}
	}
	send(w, 200, map[string]any{"work_orders": true, "knowledge": true, "keyword": true, "vector": state, "assistant": chatConfig().configured(), "mcp": false, "chat": map[bool]string{true: "configured", false: "unconfigured"}[chatConfig().configured()], "message": "助手按角色及材料绑定查询。configured 表示服务端参数已配置，实际调用仍可能失败。"})
}
