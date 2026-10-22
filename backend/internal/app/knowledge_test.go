package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaterialBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name string
		body []byte
		ok   bool
	}{{"a.md", []byte("中文\n💡"), true}, {"a.TXT", []byte("text"), true}, {"a.exe", []byte("text"), false}, {"a.txt", []byte(" \n\ufeff"), false}, {"a.txt", []byte{255}, false}, {"a.md", []byte("\x00"), false}, {"a.txt", []byte(strings.Repeat("x", maxMaterial)), true}, {"a.txt", []byte(strings.Repeat("x", maxMaterial+1)), false}} {
		if (validateMaterial(tt.name, tt.body) == nil) != tt.ok {
			t.Errorf("boundary %s size %d", tt.name, len(tt.body))
		}
	}
}
func TestUnicodeChunks(t *testing.T) {
	for _, n := range []int{1, 799, 800, 801, 1520, 1521, 1600, 10000} {
		text := strings.Repeat("灯💡水\n", n)
		chars := []rune(text)
		chunks := splitMaterial(text)
		for i, c := range chunks {
			if c.Text != string(chars[c.Start:c.End]) || c.End-c.Start > 800 || c.Sequence != i+1 {
				t.Fatal("invalid original range")
			}
			if i > 0 && chunks[i-1].End-c.Start != 80 {
				t.Fatal("overlap")
			}
		}
		if chunks[len(chunks)-1].End != len(chars) {
			t.Fatal("lost tail")
		}
	}
}
func TestEmbeddingValidation(t *testing.T) {
	for _, response := range []string{`{"data":[{"index":0,"embedding":[1,2]}]}`, `{"data":[{"index":0,"embedding":[1]}]}`, `{"data":[{"index":1,"embedding":[1,2]}]}`, `{"data":[{"index":0,"embedding":[0,0]}]}`, `{"data":[]}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(response)) }))
		v := VectorGateway{URL: srv.URL, Key: "test-only", Model: "mock", Dimension: 2, Client: srv.Client()}
		_, err := v.embed(context.Background(), []string{"演示"})
		if (err == nil) != (response == `{"data":[{"index":0,"embedding":[1,2]}]}`) {
			t.Fatal("invalid embedding accepted")
		}
		srv.Close()
	}
}

func TestCollectionDimensionGuard(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"result":{"config":{"params":{"vectors":{"size":3,"distance":"Cosine"}}}}}`))
	}))
	defer srv.Close()
	v := VectorGateway{Qdrant: srv.URL, Collection: "wrong", Dimension: 4, Client: srv.Client()}
	if v.ensure(context.Background()) == nil {
		t.Fatal("incompatible collection accepted")
	}
	t.Setenv("EMBEDDING_MODEL", "model-a")
	t.Setenv("EMBEDDING_DIMENSION", "4")
	first := vectorConfig().Collection
	t.Setenv("EMBEDDING_DIMENSION", "5")
	if first == vectorConfig().Collection {
		t.Fatal("different dimensions share collection")
	}
	t.Setenv("EMBEDDING_MODEL", "model-b")
	t.Setenv("EMBEDDING_DIMENSION", "4")
	if first == vectorConfig().Collection {
		t.Fatal("different model shares collection")
	}
}

func TestVectorCleanupMissingCollection(t *testing.T) {
	for _, status := range []int{404, 503} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
		v := VectorGateway{Qdrant: srv.URL, Collection: "missing", Client: srv.Client()}
		err := v.cleanup(context.Background(), 1, 1, false)
		if (err == nil) != (status == 404) {
			t.Fatal("missing collection must be clean; outage must retry")
		}
		srv.Close()
	}
}
