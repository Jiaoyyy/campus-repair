package app

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const maxMaterial = 10 << 20

type Material struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Filename    string    `json:"filename"`
	Demo        bool      `json:"demo"`
	Status      string    `json:"status"`
	Error       string    `json:"error"`
	Generation  int64     `json:"generation"`
	VectorReady bool      `json:"vector_ready"`
	CreatedAt   time.Time `json:"created_at"`
	Text        string    `json:"text,omitempty"`
	storage     string
}

const materialColumns = "id,title,filename,demo,status,error_message,published_generation,vector_ready,created_at"

func scanMaterial(row scanner) (Material, error) {
	var m Material
	err := row.Scan(&m.ID, &m.Title, &m.Filename, &m.Demo, &m.Status, &m.Error, &m.Generation, &m.VectorReady, &m.CreatedAt)
	return m, err
}
func uploadDir() string {
	if p := os.Getenv("UPLOAD_DIR"); p != "" {
		return p
	}
	return "/data/uploads"
}
func validateMaterial(name string, b []byte) error {
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".txt" && ext != ".md" {
		return bad("仅支持 UTF-8 的 .txt 或 .md 文件")
	}
	if len(b) > maxMaterial {
		return &Problem{413, "too_large", "文件不能超过 10 MiB"}
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), '\x00') {
		return bad("文件必须是有效 UTF-8 文本")
	}
	if strings.TrimSpace(strings.ReplaceAll(string(b), "\ufeff", "")) == "" {
		return bad("文件正文不能为空")
	}
	return nil
}
func (s *Server) materials(w http.ResponseWriter, r *http.Request) {
	// Management endpoints remain administrator-only; assistant access uses bindings separately.
	if current(r).Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	page, size, err := pagination(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var total int
	if err = s.DB.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM knowledge_materials WHERE org_id=?", current(r).OrgID).Scan(&total); err != nil {
		s.fail(w, r, err)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), "SELECT "+materialColumns+" FROM knowledge_materials WHERE org_id=? ORDER BY id DESC LIMIT ? OFFSET ?", current(r).OrgID, size, (page-1)*size)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer rows.Close()
	items := []Material{}
	for rows.Next() {
		m, e := scanMaterial(rows)
		if e != nil {
			s.fail(w, r, e)
			return
		}
		items = append(items, m)
	}
	if err = rows.Err(); err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size})
}
func (s *Server) uploadMaterial(w http.ResponseWriter, r *http.Request) {
	u := current(r)
	if u.Role != "admin" {
		s.fail(w, r, denied())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMaterial+(64<<10))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			s.fail(w, r, &Problem{413, "too_large", "文件不能超过 10 MiB"})
		} else {
			s.fail(w, r, bad("上传内容无效"))
		}
		return
	}
	defer r.MultipartForm.RemoveAll()
	files := r.MultipartForm.File["file"]
	if len(files) != 1 || len(r.MultipartForm.File) != 1 {
		s.fail(w, r, bad("每次只能上传一个文件"))
		return
	}
	f, err := files[0].Open()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxMaterial+1))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name := filepath.Base(files[0].Filename)
	if err = validateMaterial(name, b); err != nil {
		s.fail(w, r, err)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" || utf8.RuneCountInString(title) > 200 || len(name) > 240 {
		s.fail(w, r, bad("标题须为 1 至 200 字，文件名不能超过 240 字节"))
		return
	}
	demo := r.FormValue("demo")
	if demo != "true" && demo != "false" {
		s.fail(w, r, bad("请选择材料是否为演示用途"))
		return
	}
	if err = os.MkdirAll(uploadDir(), 0700); err != nil {
		s.fail(w, r, err)
		return
	}
	storage := randomHex(24)
	path := filepath.Join(uploadDir(), storage)
	f2, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	_, err = f2.Write(b)
	if err == nil {
		err = f2.Sync()
	}
	closeErr := f2.Close()
	if err == nil {
		err = closeErr
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(path)
		}
	}()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(r.Context(), "INSERT INTO knowledge_materials(org_id,uploader_id,title,filename,storage_name,body,demo,created_at) VALUES(?,?,?,?,?,?,?,UTC_TIMESTAMP(6))", u.OrgID, u.ID, title, name, storage, string(b), demo == "true")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	id, err := res.LastInsertId()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO index_jobs(material_id,generation,status,created_at) VALUES(?,1,'pending',UTC_TIMESTAMP(6))", id); err == nil {
		err = writeAudit(r.Context(), tx, requestID(r), &u, "knowledge.upload", id, "success", map[string]any{"demo": demo == "true", "bytes": len(b)})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	committed = true
	send(w, 201, map[string]any{"id": id, "status": "pending"})
}
func (s *Server) getMaterial(r *http.Request) (Material, error) {
	var m Material
	if current(r).Role != "admin" {
		return m, denied()
	}
	id, err := parseID(r)
	if err != nil {
		return m, err
	}
	m, err = scanMaterial(s.DB.QueryRowContext(r.Context(), "SELECT "+materialColumns+" FROM knowledge_materials WHERE id=? AND org_id=?", id, current(r).OrgID))
	if err != nil {
		return m, dbError(err)
	}
	err = s.DB.QueryRowContext(r.Context(), "SELECT body,storage_name FROM knowledge_materials WHERE id=? AND org_id=?", id, current(r).OrgID).Scan(&m.Text, &m.storage)
	return m, err
}
func (s *Server) materialDetail(w http.ResponseWriter, r *http.Request) {
	m, err := s.getMaterial(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 200, map[string]any{"material": m})
}
func (s *Server) materialFile(w http.ResponseWriter, r *http.Request) {
	m, err := s.getMaterial(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	f, err := os.Open(filepath.Join(uploadDir(), m.storage))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": m.Filename}))
	http.ServeContent(w, r, m.Filename, st.ModTime(), f)
}
func (s *Server) reindexMaterial(w http.ResponseWriter, r *http.Request) {
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
	tx, err := s.DB.BeginTx(r.Context(), nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer tx.Rollback()
	var generation int64
	var status string
	err = tx.QueryRowContext(r.Context(), "SELECT next_generation,status FROM knowledge_materials WHERE id=? AND org_id=? FOR UPDATE", id, u.OrgID).Scan(&generation, &status)
	if err != nil {
		s.fail(w, r, dbError(err))
		return
	}
	if status == "pending" || status == "processing" {
		s.fail(w, r, &Problem{409, "conflict", "索引任务已在排队或处理中，请稍后刷新"})
		return
	}
	generation++
	_, err = tx.ExecContext(r.Context(), "UPDATE knowledge_materials SET next_generation=?,status='pending',error_message='' WHERE id=?", generation, id)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), "INSERT INTO index_jobs(material_id,generation,status,created_at) VALUES(?,?,'pending',UTC_TIMESTAMP(6))", id, generation)
	}
	if err == nil {
		err = writeAudit(r.Context(), tx, requestID(r), &u, "knowledge.reindex", id, "success", map[string]any{"generation": generation})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	send(w, 202, map[string]any{"id": id, "status": "pending", "generation": generation})
}

type Chunk struct {
	Sequence int    `json:"sequence"`
	Start    int    `json:"start"`
	End      int    `json:"end"`
	Text     string `json:"text"`
}

// Half-open, zero-based Unicode character offsets, including original whitespace/BOM.
func splitMaterial(text string) []Chunk {
	chars := []rune(text)
	chunks := []Chunk{}
	for start := 0; start < len(chars); start += 720 {
		end := start + 800
		if end > len(chars) {
			end = len(chars)
		}
		chunks = append(chunks, Chunk{len(chunks) + 1, start, end, string(chars[start:end])})
		if end == len(chars) {
			break
		}
	}
	return chunks
}
