package app

import (
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
	// Direct search is a management tool. Future assistant calls must supply a server-validated binding scope.
	u := current(r)
	if u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "keyword"
	}
	if q == "" || utf8.RuneCountInString(q) > 500 {
		s.fail(w, r, bad("检索词须为 1 至 500 字"))
		return
	}
	if mode != "keyword" && mode != "vector" && mode != "hybrid" {
		s.fail(w, r, bad("检索模式无效"))
		return
	}
	// Do not accept an unvalidated assistant ID or a fabricated binding scope.
	if r.URL.Query().Get("agent_id") != "" {
		s.fail(w, r, bad("助手检索尚未开放，请使用管理材料检索"))
		return
	}
	keyword, vector := []int64{}, []int64{}
	if mode != "vector" {
		rows, err := s.DB.QueryContext(r.Context(), "SELECT c.id FROM knowledge_chunks c JOIN knowledge_materials m ON m.id=c.material_id AND m.published_generation=c.generation WHERE m.org_id=? AND MATCH(c.body) AGAINST(? IN NATURAL LANGUAGE MODE) ORDER BY MATCH(c.body) AGAINST(? IN NATURAL LANGUAGE MODE) DESC,c.id LIMIT 100", u.OrgID, q, q)
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
			keyword = append(keyword, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if mode != "keyword" {
		v := vectorConfig()
		if !v.configured() {
			s.fail(w, r, &Problem{503, "vector_unavailable", "向量检索未配置，可改用关键词检索"})
			return
		}
		rows, err := s.DB.QueryContext(r.Context(), "SELECT id,vector_ready,vector_collection FROM knowledge_materials WHERE org_id=? AND published_generation>0", u.OrgID)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		materials := []int64{}
		incomplete := false
		for rows.Next() {
			var id int64
			var ready bool
			var collection string
			if err = rows.Scan(&id, &ready, &collection); err != nil {
				rows.Close()
				s.fail(w, r, err)
				return
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
			s.fail(w, r, err)
			return
		}
		if incomplete {
			s.fail(w, r, &Problem{503, "vector_unavailable", "部分材料尚无当前模型的向量索引，请重建或使用关键词检索"})
			return
		}
		// Check the dependency even when no materials exist; an outage is not an empty result.
		if err = v.ensure(r.Context()); err == nil {
			vector, err = v.search(r.Context(), u.OrgID, q, materials)
		}
		if err != nil {
			s.fail(w, r, &Problem{503, "vector_unavailable", "向量服务暂不可用，可改用关键词检索"})
			return
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
			err := s.DB.QueryRowContext(r.Context(), "SELECT c.id,m.id,c.generation,m.title,m.demo,c.sequence_no,c.start_char,c.end_char,c.body FROM knowledge_chunks c JOIN knowledge_materials m ON m.id=c.material_id AND m.published_generation=c.generation WHERE c.id=? AND m.org_id=?", id, u.OrgID).Scan(&e.ID, &e.MaterialID, &e.Generation, &e.Title, &e.Demo, &e.Sequence, &e.Start, &e.End, &e.Text)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				s.fail(w, r, err)
				return
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
	if err := writeAudit(r.Context(), s.DB, requestID(r), &u, "knowledge.search", nil, "success", map[string]any{"mode": mode, "hits": len(hits)}); err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"items": hits, "mode": mode, "scope": "organization_management"})
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
	send(w, 200, map[string]any{"work_orders": true, "knowledge": true, "keyword": true, "vector": state, "assistant": false, "mcp": false, "message": "材料管理与检索已开放；助手和 MCP 待接入。向量 configured 仅表示参数和集合可用，不代表网关调用已验证。"})
}
