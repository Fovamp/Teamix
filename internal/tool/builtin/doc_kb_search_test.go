package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockKBServer 模拟"embedding 已配置"的 RAGFlow：retrieval 正常返回片段。
func mockKBServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/datasets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":[{"id":"ds-1","name":"teamix-docs","document_count":3,"chunk_count":42,"token_num":1000}]}`))
	})
	mux.HandleFunc("/api/v1/retrieval", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"chunks":[
			{"content":"Teamix deployment: run teamix.exe serve --project <path>.","similarity":0.93,"document_keyword":"deploy.md"},
			{"content":"The knowledge base stores team docs for semantic retrieval.","similarity":0.88,"document_keyword":"kb.md"}
		]}}`))
	})
	return httptest.NewServer(mux)
}

func TestDocKBSearchExecute(t *testing.T) {
	srv := mockKBServer(t)
	defer srv.Close()
	t.Setenv("RAGFLOW_API_KEY", "test-key")
	t.Setenv("RAGFLOW_BASE_URL", srv.URL)

	out, err := docKBSearch{}.Execute(context.Background(), json.RawMessage(`{"question":"how to deploy teamix?"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "[1]") || !strings.Contains(out, "deploy.md") {
		t.Errorf("output missing chunk header/source:\n%s", out)
	}
	if !strings.Contains(out, "teamix.exe serve") {
		t.Errorf("output missing chunk content:\n%s", out)
	}
	if !strings.Contains(out, "[2]") {
		t.Errorf("output missing second chunk:\n%s", out)
	}
}

func TestDocKBSearchDatasetFilter(t *testing.T) {
	srv := mockKBServer(t)
	defer srv.Close()
	t.Setenv("RAGFLOW_API_KEY", "test-key")
	t.Setenv("RAGFLOW_BASE_URL", srv.URL)

	// 指定不存在的 dataset → 明确错误
	out, err := docKBSearch{}.Execute(context.Background(), json.RawMessage(`{"question":"x","dataset":"no-such"}`))
	if err == nil || !strings.Contains(err.Error(), "no-such") {
		t.Fatalf("want unknown-dataset error, got out=%q err=%v", out, err)
	}
}

func TestDocKBSearchMissingKey(t *testing.T) {
	t.Setenv("RAGFLOW_API_KEY", "")
	t.Setenv("RAGFLOW_BASE_URL", "")
	if _, err := (docKBSearch{}).Execute(context.Background(), json.RawMessage(`{"question":"x"}`)); err == nil {
		t.Fatal("want error when RAGFLOW_API_KEY unset")
	}
}

func TestDocKBSearchBadArgs(t *testing.T) {
	if _, err := (docKBSearch{}).Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("want error for missing question")
	}
}
