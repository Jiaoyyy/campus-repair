package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"
)

type IndexJob struct {
	ID, Material, Generation, Org int64
	Token, Text                   string
}

func claimJob(ctx context.Context, db *sql.DB) (IndexJob, error) {
	var j IndexJob
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return j, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, "SELECT j.id,j.material_id,j.generation FROM index_jobs j WHERE j.status='pending' OR (j.status='processing' AND j.lease_until<UTC_TIMESTAMP(6)) ORDER BY j.id LIMIT 1 FOR UPDATE SKIP LOCKED").Scan(&j.ID, &j.Material, &j.Generation)
	if err != nil {
		return j, err
	}
	j.Token = randomHex(24)
	_, err = tx.ExecContext(ctx, "UPDATE index_jobs SET status='processing',lease_token=?,lease_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND),attempts=attempts+1 WHERE id=?", j.Token, j.ID)
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE knowledge_materials SET status='processing' WHERE id=?", j.Material)
	}
	if err == nil {
		err = tx.QueryRowContext(ctx, "SELECT org_id,body FROM knowledge_materials WHERE id=?", j.Material).Scan(&j.Org, &j.Text)
	}
	if err == nil {
		err = tx.Commit()
	}
	return j, err
}
func jobOwned(ctx context.Context, tx *sql.Tx, j IndexJob) error {
	var id int64
	return tx.QueryRowContext(ctx, "SELECT id FROM index_jobs WHERE id=? AND lease_token=? AND status='processing' AND lease_until>UTC_TIMESTAMP(6) FOR UPDATE", j.ID, j.Token).Scan(&id)
}
func stageChunks(ctx context.Context, db *sql.DB, j IndexJob) ([]int64, []Chunk, error) {
	chunks := splitMaterial(j.Text)
	ids := []int64{}
	for start := 0; start < len(chunks); start += 64 {
		end := start + 64
		if end > len(chunks) {
			end = len(chunks)
		}
		err := func() error {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return err
			}
			defer tx.Rollback()
			if err = jobOwned(ctx, tx, j); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO knowledge_generations(material_id,generation,vector_collection) VALUES(?,?,?) ON DUPLICATE KEY UPDATE vector_collection=VALUES(vector_collection)", j.Material, j.Generation, vectorConfig().Collection)
			if err != nil {
				return err
			}
			for _, c := range chunks[start:end] {
				res, e := tx.ExecContext(ctx, "INSERT INTO knowledge_chunks(material_id,generation,sequence_no,start_char,end_char,body) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)", j.Material, j.Generation, c.Sequence, c.Start, c.End, c.Text)
				if e != nil {
					return e
				}
				id, e := res.LastInsertId()
				if e != nil {
					return e
				}
				ids = append(ids, id)
			}
			return tx.Commit()
		}()
		if err != nil {
			return nil, nil, err
		}
	}
	return ids, chunks, nil
}

func finishJob(ctx context.Context, db *sql.DB, j IndexJob, keyword, vector bool, collection string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = jobOwned(ctx, tx, j); err != nil {
		return err
	}
	status, message := "ready", ""
	if !vector {
		status = "failed"
		message = "向量索引不可用，请检查嵌入配置和向量服务后重试"
	}
	if !keyword {
		status = "failed"
		message = "关键词索引失败，请重试"
	}
	var published int64
	if err = tx.QueryRowContext(ctx, "SELECT published_generation FROM knowledge_materials WHERE id=? FOR UPDATE", j.Material).Scan(&published); err != nil {
		return err
	}
	// A first keyword-only build may publish. Failed rebuilds retain the previous generation.
	if keyword && (vector || published == 0) {
		_, err = tx.ExecContext(ctx, "UPDATE knowledge_materials SET published_generation=?,vector_ready=?,vector_collection=?,status=?,error_message=? WHERE id=?", j.Generation, vector, collection, status, message, j.Material)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE knowledge_materials SET status=?,error_message=? WHERE id=?", status, message, j.Material)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE index_jobs SET status=?,error_message=?,lease_until=NULL WHERE id=? AND lease_token=?", status, message, j.ID, j.Token)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO audit_events(org_id,request_id,action,resource_id,outcome,metadata,created_at) VALUES(?,?,'knowledge.index',?,?,JSON_OBJECT('generation',?),UTC_TIMESTAMP(6))", j.Org, fmt.Sprintf("index-%d-%d", j.ID, j.Generation), j.Material, map[bool]string{true: "success", false: "failed"}[vector], j.Generation)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT IGNORE INTO vector_cleanup_jobs(material_id,generation,vector_collection) SELECT g.material_id,g.generation,g.vector_collection FROM knowledge_generations g JOIN knowledge_materials m ON m.id=g.material_id WHERE g.material_id=? AND (g.generation<>m.published_generation OR m.vector_ready=FALSE)", j.Material)
	}
	if err == nil {
		err = tx.Commit()
	}
	return err
}
func processJob(parent context.Context, db *sql.DB, j IndexJob) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	stopped := make(chan struct{})
	defer func() { cancel(); <-stopped }()
	go func() {
		defer close(stopped)
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				renew, stop := context.WithTimeout(ctx, 3*time.Second)
				res, err := db.ExecContext(renew, "UPDATE index_jobs SET lease_until=DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 30 SECOND) WHERE id=? AND lease_token=? AND status='processing' AND lease_until>UTC_TIMESTAMP(6)", j.ID, j.Token)
				stop()
				if err != nil {
					cancel()
					return
				}
				n, e := res.RowsAffected()
				if e != nil || n != 1 {
					cancel()
					return
				}
			}
		}
	}()
	ids, chunks, err := stageChunks(ctx, db, j)
	keyword := err == nil
	v := vectorConfig()
	vector := keyword && v.configured()
	if vector {
		vector = v.ensure(ctx) == nil
	}
	for start := 0; vector && start < len(chunks); start += 16 {
		end := start + 16
		if end > len(chunks) {
			end = len(chunks)
		}
		texts := []string{}
		for _, c := range chunks[start:end] {
			texts = append(texts, c.Text)
		}
		vector = v.upsert(ctx, j.Org, j.Material, j.Generation, ids[start:end], texts) == nil
	}
	if ctx.Err() != nil {
		return
	}
	if err = finishJob(ctx, db, j, keyword, vector, v.Collection); err != nil {
		log.Print("knowledge index publication failed; lease will recover")
		return
	}
	// Never delete vectors during publication. Cleanup only follows a committed success.
	cleanupVectors(ctx, db)
}
func RunIndexer(ctx context.Context, db *sql.DB) {
	log.Print("knowledge index worker ready")
	for ctx.Err() == nil {
		cleanupVectors(ctx, db)
		j, err := claimJob(ctx, db)
		if err == nil {
			processJob(ctx, db, j)
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
			log.Print("knowledge task store unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// Cleanup is durable and retryable. Publication never waits for vector deletion.
func cleanupVectors(ctx context.Context, db *sql.DB) {
	var material, generation int64
	var collection string
	err := db.QueryRowContext(ctx, "SELECT material_id,generation,vector_collection FROM vector_cleanup_jobs ORDER BY material_id,generation LIMIT 1").Scan(&material, &generation, &collection)
	if err != nil {
		return
	}
	v := vectorConfig()
	v.Collection = collection
	cleanupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if v.cleanup(cleanupCtx, material, generation, false) == nil {
		_, _ = db.ExecContext(ctx, "DELETE FROM vector_cleanup_jobs WHERE material_id=? AND generation=? AND vector_collection=?", material, generation, collection)
	}
}
