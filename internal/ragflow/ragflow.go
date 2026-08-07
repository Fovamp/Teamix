// Package ragflow 封装 RAGFlow 文档知识库 HTTP API（直调 /api/v1/*）。
//
// 用途：Teamix 的文档知识库后端——建 dataset、上传文档、语义检索。
// 鉴权用 Bearer token（RAGFLOW_API_KEY）；地址 RAGFLOW_BASE_URL（必须显式配置，
// 不设默认地址，避免静默连到作者内网）。检索依赖服务端已配置 embedding 模型；
// 未配置时 RAGFlow 返回错误，原样透传。
package ragflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

// Client 是 RAGFlow API 客户端。
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewClient 构造客户端。baseURL 为空时不设默认值（调用时返回"未配置"错误）。
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// requireBase 校验地址已配置，未配置返回明确错误（不静默连默认内网）。
func (c *Client) requireBase() error {
	if c.BaseURL == "" {
		return fmt.Errorf("ragflow: RAGFLOW_BASE_URL 未配置（项目 .env 中添加后重启）")
	}
	return nil
}

// Dataset 是 RAGFlow dataset 的轻量视图。
type Dataset struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	DocCount   int    `json:"document_count"`
	ChunkCount int    `json:"chunk_count"`
	TokenNum   int    `json:"token_num"`
}

// Chunk 是一条检索结果片段。
type Chunk struct {
	Content    string  `json:"content"`
	Similarity float64 `json:"similarity,omitempty"`
	Document   string  `json:"document_keyword,omitempty"`
}

// apiResp 是 RAGFlow 统一响应外壳（code 0 = 成功）。
type apiResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// do 执行请求并解析统一响应；code != 0 返回错误。
func (c *Client) do(ctx context.Context, method, path string, body io.Reader, contentType string) (apiResp, error) {
	var resp apiResp
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return resp, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	r, err := c.HTTP.Do(req)
	if err != nil {
		return resp, err
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return resp, err
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return resp, fmt.Errorf("ragflow: bad response %s %s: %s", method, path, strings.TrimSpace(string(raw)))
	}
	if resp.Code != 0 {
		return resp, fmt.Errorf("ragflow: %s %s: %s", method, path, resp.Message)
	}
	return resp, nil
}

// ListDatasets 列出全部 dataset。
func (c *Client) ListDatasets(ctx context.Context) ([]Dataset, error) {
	if err := c.requireBase(); err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, http.MethodGet, "/api/v1/datasets", nil, "")
	if err != nil {
		return nil, err
	}
	var out []Dataset
	if len(resp.Data) > 0 && string(resp.Data) != "null" {
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			return nil, fmt.Errorf("ragflow: parse datasets: %w", err)
		}
	}
	return out, nil
}

// CreateDataset 新建 dataset（name 不能重复）。
func (c *Client) CreateDataset(ctx context.Context, name string) (*Dataset, error) {
	if err := c.requireBase(); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(map[string]string{"name": name})
	resp, err := c.do(ctx, http.MethodPost, "/api/v1/datasets", bytes.NewReader(payload), "application/json")
	if err != nil {
		return nil, err
	}
	var d Dataset
	if err := json.Unmarshal(resp.Data, &d); err != nil {
		return nil, fmt.Errorf("ragflow: parse dataset: %w", err)
	}
	return &d, nil
}

// EnsureDataset 按名字查找 dataset，不存在则创建。返回现有或新建的 dataset。
func (c *Client) EnsureDataset(ctx context.Context, name string) (*Dataset, error) {
	list, err := c.ListDatasets(ctx)
	if err != nil {
		return nil, err
	}
	for _, d := range list {
		if d.Name == name {
			return &d, nil
		}
	}
	return c.CreateDataset(ctx, name)
}

// UploadDocument 向 dataset 上传一个文档（multipart/file），RAGFlow 异步解析。
func (c *Client) UploadDocument(ctx context.Context, datasetID, filename string, content []byte) error {
	if err := c.requireBase(); err != nil {
		return err
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return err
	}
	if _, err := fw.Write(content); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, "/api/v1/datasets/"+url.PathEscape(datasetID)+"/documents", &buf, mw.FormDataContentType())
	return err
}

// Retrieve 语义检索。datasetIDs 为空时检索全部 dataset。
// 返回按相似度降序的片段列表。RAGFlow 服务端未配置 embedding 模型时返回错误（透传）。
func (c *Client) Retrieve(ctx context.Context, question string, datasetIDs []string, topK int) ([]Chunk, error) {
	if err := c.requireBase(); err != nil {
		return nil, err
	}
	if topK <= 0 {
		topK = 5
	}
	payload, _ := json.Marshal(map[string]any{
		"question":    question,
		"dataset_ids": datasetIDs,
		"top_k":       topK,
	})
	resp, err := c.do(ctx, http.MethodPost, "/api/v1/retrieval", bytes.NewReader(payload), "application/json")
	if err != nil {
		return nil, err
	}
	var data struct {
		Chunks []Chunk `json:"chunks"`
	}
	if len(resp.Data) > 0 {
		if err := json.Unmarshal(resp.Data, &data); err != nil {
			return nil, fmt.Errorf("ragflow: parse retrieval: %w", err)
		}
	}
	return data.Chunks, nil
}
