package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type VectorGateway struct {
	URL, Key, Model, Qdrant, Collection string
	Dimension                           int
	Client                              *http.Client
}

func vectorConfig() VectorGateway {
	d, _ := strconv.Atoi(os.Getenv("EMBEDDING_DIMENSION"))
	q := strings.TrimRight(os.Getenv("QDRANT_URL"), "/")
	if q == "" {
		q = "http://qdrant:6333"
	}
	revision := os.Getenv("EMBEDDING_COLLECTION")
	if revision == "" {
		revision = "repair_v1"
	}
	h := sha256.Sum256([]byte(os.Getenv("EMBEDDING_MODEL") + ":" + strconv.Itoa(d)))
	// Model/dimension fingerprint prevents mixing spaces. Reindex publishes the new collection explicitly.
	return VectorGateway{strings.TrimRight(os.Getenv("EMBEDDING_BASE_URL"), "/"), os.Getenv("EMBEDDING_API_KEY"), os.Getenv("EMBEDDING_MODEL"), q, revision + "_" + hex.EncodeToString(h[:8]), d, &http.Client{Timeout: 20 * time.Second}}
}
func (v VectorGateway) configured() bool {
	return v.URL != "" && v.Key != "" && v.Model != "" && v.Dimension > 0 && v.Dimension <= 65536
}

var errVector = errors.New("vector dependency unavailable")

type vectorHTTPError struct{ Status int }

func (e *vectorHTTPError) Error() string { return "vector dependency unavailable" }

func (v VectorGateway) call(ctx context.Context, method, url, key string, in, out any) error {
	var b []byte
	var err error
	if in != nil {
		b, err = json.Marshal(in)
		if err != nil {
			return errVector
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(b))
	if err != nil {
		return errVector
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := v.Client.Do(req)
	if err != nil {
		return errVector
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &vectorHTTPError{res.StatusCode}
	}
	if out != nil {
		if err = json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out); err != nil {
			return errVector
		}
	}
	return nil
}
func (v VectorGateway) embed(ctx context.Context, texts []string) ([][]float64, error) {
	if !v.configured() {
		return nil, errVector
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := v.call(ctx, "POST", v.URL+"/embeddings", v.Key, map[string]any{"model": v.Model, "input": texts}, &out); err != nil {
		return nil, err
	}
	if len(out.Data) != len(texts) {
		return nil, errVector
	}
	result := make([][]float64, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(result) || result[d.Index] != nil || len(d.Embedding) != v.Dimension {
			return nil, errVector
		}
		norm := 0.0
		for _, f := range d.Embedding {
			if math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, errVector
			}
			norm += f * f
		}
		if norm == 0 {
			return nil, errVector
		}
		result[d.Index] = d.Embedding
	}
	return result, nil
}
func (v VectorGateway) ensure(ctx context.Context) error {
	url := v.Qdrant + "/collections/" + v.Collection
	var out struct {
		Result struct {
			Config struct {
				Params struct {
					Vectors struct {
						Size     int    `json:"size"`
						Distance string `json:"distance"`
					} `json:"vectors"`
				} `json:"params"`
			} `json:"config"`
		} `json:"result"`
	}
	if v.call(ctx, "GET", url, "", nil, &out) != nil {
		if err := v.call(ctx, "PUT", url, "", map[string]any{"vectors": map[string]any{"size": v.Dimension, "distance": "Cosine"}}, nil); err != nil {
			return err
		}
	}
	if err := v.call(ctx, "GET", url, "", nil, &out); err != nil || out.Result.Config.Params.Vectors.Size != v.Dimension || out.Result.Config.Params.Vectors.Distance != "Cosine" {
		return errVector
	}
	return nil
}
func (v VectorGateway) upsert(ctx context.Context, org, material, generation int64, ids []int64, texts []string) error {
	vectors, err := v.embed(ctx, texts)
	if err != nil {
		return err
	}
	points := []any{}
	for i, id := range ids {
		points = append(points, map[string]any{"id": id, "vector": vectors[i], "payload": map[string]any{"org_id": org, "material_id": material, "generation": generation}})
	}
	return v.call(ctx, "PUT", v.Qdrant+"/collections/"+v.Collection+"/points?wait=true", "", map[string]any{"points": points}, nil)
}
func (v VectorGateway) cleanup(ctx context.Context, material, generation int64, keep bool) error {
	filter := map[string]any{"must": []any{map[string]any{"key": "material_id", "match": map[string]any{"value": material}}}}
	match := map[string]any{"key": "generation", "match": map[string]any{"value": generation}}
	if keep {
		filter["must_not"] = []any{match}
	} else {
		filter["must"] = append(filter["must"].([]any), match)
	}
	err := v.call(ctx, "POST", v.Qdrant+"/collections/"+v.Collection+"/points/delete?wait=true", "", map[string]any{"filter": filter}, nil)
	var status *vectorHTTPError
	if errors.As(err, &status) && status.Status == 404 {
		return nil
	}
	return err
}
func (v VectorGateway) search(ctx context.Context, org int64, query string, materials []int64) ([]int64, error) {
	vectors, err := v.embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	must := []any{map[string]any{"key": "org_id", "match": map[string]any{"value": org}}}
	if materials != nil {
		if len(materials) == 0 {
			return []int64{}, nil
		}
		must = append(must, map[string]any{"key": "material_id", "match": map[string]any{"any": materials}})
	}
	var out struct {
		Result []struct {
			ID int64 `json:"id"`
		} `json:"result"`
	}
	err = v.call(ctx, "POST", fmt.Sprintf("%s/collections/%s/points/search", v.Qdrant, v.Collection), "", map[string]any{"vector": vectors[0], "limit": 100, "filter": map[string]any{"must": must}, "with_payload": false}, &out)
	ids := []int64{}
	for _, r := range out.Result {
		ids = append(ids, r.ID)
	}
	return ids, err
}
