package ragflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockRAGFlow 返回一个模拟 RAGFlow 的 httptest 服务：dataset 列表/创建、
// 文档上传、语义检索。chunk 数可通过开关控制（模拟"可用"与"空结果"）。
func mockRAGFlow(t *testing.T, chunks []Chunk) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/datasets", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-key" {
			writeMockJSON(w, apiResp{Code: 401, Message: "unauthorized"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeMockJSON(w, apiResp{Code: 0, Data: mustJSON(t, []Dataset{{ID: "ds-1", Name: "teamix-docs", DocCount: 2, ChunkCount: 10}})})
		case http.MethodPost:
			var body struct{ Name string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeMockJSON(w, apiResp{Code: 0, Data: mustJSON(t, Dataset{ID: "ds-new", Name: body.Name})})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/datasets/ds-1/documents", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !strings.Contains(r.Header.Get("Content-Type"), "multipart/form-data") {
			writeMockJSON(w, apiResp{Code: 100, Message: "expect multipart"})
			return
		}
		writeMockJSON(w, apiResp{Code: 0, Data: mustJSON(t, []map[string]any{{"id": "doc-1", "name": "notes.md"}})})
	})
	mux.HandleFunc("/api/v1/retrieval", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Question    string   `json:"question"`
			DatasetIDs  []string `json:"dataset_ids"`
			TopK        int      `json:"top_k"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Question == "" || len(body.DatasetIDs) == 0 {
			writeMockJSON(w, apiResp{Code: 102, Message: "`dataset_ids` is required."})
			return
		}
		writeMockJSON(w, apiResp{Code: 0, Data: mustJSON(t, struct {
			Chunks []Chunk `json:"chunks"`
		}{Chunks: chunks})})
	})
	return httptest.NewServer(mux)
}

func writeMockJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestClientEndToEnd(t *testing.T) {
	srv := mockRAGFlow(t, []Chunk{
		{Content: "Teamix is a multi-user AI platform.", Similarity: 0.91, Document: "teamix-doc.txt"},
		{Content: "Qwen runs internally.", Similarity: 0.85, Document: "teamix-doc.txt"},
	})
	defer srv.Close()
	c := NewClient(srv.URL, "test-key")
	ctx := context.Background()

	// List
	ds, err := c.ListDatasets(ctx)
	if err != nil {
		t.Fatalf("ListDatasets: %v", err)
	}
	if len(ds) != 1 || ds[0].Name != "teamix-docs" {
		t.Fatalf("datasets = %+v", ds)
	}

	// EnsureDataset：存在 → 复用；不存在 → 创建
	got, err := c.EnsureDataset(ctx, "teamix-docs")
	if err != nil || got.ID != "ds-1" {
		t.Fatalf("EnsureDataset existing = %+v, err=%v", got, err)
	}
	created, err := c.EnsureDataset(ctx, "brand-new")
	if err != nil || created.ID != "ds-new" {
		t.Fatalf("EnsureDataset new = %+v, err=%v", created, err)
	}

	// UploadDocument（multipart）
	if err := c.UploadDocument(ctx, "ds-1", "notes.md", []byte("hello teamix")); err != nil {
		t.Fatalf("UploadDocument: %v", err)
	}

	// Retrieve：模拟"embedding 可用"返回片段
	chunks, err := c.Retrieve(ctx, "what is teamix?", []string{"ds-1"}, 2)
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(chunks))
	}
	if chunks[0].Content == "" || chunks[0].Document != "teamix-doc.txt" {
		t.Errorf("chunk[0] = %+v", chunks[0])
	}
}

func TestClientErrorPropagation(t *testing.T) {
	// dataset_ids 为空 → RAGFlow 返回 102 错误，应透传
	srv := mockRAGFlow(t, nil)
	defer srv.Close()
	c := NewClient(srv.URL, "test-key")
	_, err := c.Retrieve(context.Background(), "x", nil, 1)
	if err == nil || !strings.Contains(err.Error(), "dataset_ids") {
		t.Fatalf("want dataset_ids error propagated, got %v", err)
	}
}

func TestClientAuthRejected(t *testing.T) {
	srv := mockRAGFlow(t, nil)
	defer srv.Close()
	// 错误 key → mock 返回 401 code
	c := NewClient(srv.URL, "wrong-key")
	if _, err := c.ListDatasets(context.Background()); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("want unauthorized error, got %v", err)
	}
}

func TestClientRequiresBaseURL(t *testing.T) {
	c := NewClient("", "test-key")
	if _, err := c.ListDatasets(context.Background()); err == nil || !strings.Contains(err.Error(), "RAGFLOW_BASE_URL") {
		t.Fatalf("want base-url error, got %v", err)
	}
}
