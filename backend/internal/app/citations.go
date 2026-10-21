package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type CitationRef struct {
	Number     int   `json:"number"`
	ChunkID    int64 `json:"chunk_id"`
	Generation int64 `json:"generation"`
}
type Citation struct {
	Number int `json:"number"`
	Evidence
}

var citationPattern = regexp.MustCompile(`\[(\d+)\]`)

func (s *Server) authorizedEvidence(ctx context.Context, u User, agent, id int64) (Evidence, error) {
	scope, args := knowledgeScope(u, agent)
	var e Evidence
	err := s.DB.QueryRowContext(ctx, "SELECT c.id,m.id,c.generation,m.title,m.demo,c.sequence_no,c.start_char,c.end_char,c.body FROM knowledge_chunks c JOIN knowledge_materials m ON m.id=c.material_id AND m.published_generation=c.generation WHERE c.id=? AND "+scope, append([]any{id}, args...)...).Scan(&e.ID, &e.MaterialID, &e.Generation, &e.Title, &e.Demo, &e.Sequence, &e.Start, &e.End, &e.Text)
	return e, err
}
func (s *Server) verifyReferences(ctx context.Context, u User, agent int64, text string, refs []CitationRef) (string, []Citation, string, error) {
	available := map[int]Evidence{}
	for _, ref := range refs {
		e, err := s.authorizedEvidence(ctx, u, agent, ref.ChunkID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", nil, "", err
		}
		if e.Generation == ref.Generation {
			available[ref.Number] = e
		}
	}
	used := map[int]bool{}
	citations := []Citation{}
	invalid := false
	text = citationPattern.ReplaceAllStringFunc(text, func(marker string) string {
		n, _ := strconv.Atoi(marker[1 : len(marker)-1])
		e, ok := available[n]
		if !ok {
			invalid = true
			return ""
		}
		if !used[n] {
			used[n] = true
			citations = append(citations, Citation{n, e})
		}
		return marker
	})
	warning := ""
	if invalid {
		warning = "回答的部分引用无法验证，已移除无效来源；请勿将其视为已核实依据。"
	}
	return strings.TrimSpace(text), citations, warning, nil
}
func candidateRefs(hits []Evidence) []CitationRef {
	refs := []CitationRef{}
	for i, e := range hits {
		refs = append(refs, CitationRef{i + 1, e.ID, e.Generation})
	}
	return refs
}
func citationRefs(citations []Citation) []CitationRef {
	refs := []CitationRef{}
	for _, c := range citations {
		refs = append(refs, CitationRef{c.Number, c.ID, c.Generation})
	}
	return refs
}
func (s *Server) citationSource(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	id, err := parseID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.ownConversation(r.Context(), u, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	mid, e1 := strconv.ParseInt(r.PathValue("message"), 10, 64)
	chunk, e2 := strconv.ParseInt(r.PathValue("chunk"), 10, 64)
	if e1 != nil || e2 != nil || mid < 1 || chunk < 1 {
		s.fail(w, r, invisible())
		return
	}
	m, err := s.loadMessage(r.Context(), id, mid)
	if err != nil {
		s.fail(w, r, dbError(err))
		return
	}
	found := false
	var gen int64
	for _, ref := range m.Refs {
		if ref.ChunkID == chunk {
			found = true
			gen = ref.Generation
			break
		}
	}
	if !found || m.Status != "complete" {
		s.fail(w, r, invisible())
		return
	}
	e, err := s.authorizedEvidence(r.Context(), u, c.AgentID, chunk)
	if errors.Is(err, sql.ErrNoRows) || err == nil && e.Generation != gen {
		s.fail(w, r, invisible())
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var text string
	if err = s.DB.QueryRowContext(r.Context(), "SELECT body FROM knowledge_materials WHERE id=? AND org_id=?", e.MaterialID, u.OrgID).Scan(&text); err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"evidence": e, "text": text})
}
