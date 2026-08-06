package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

// SearchTool 是本地文档检索工具（RAG）：对已索引的机密文档按问题检索
// 相关片段。片段作为工具结果进入上下文后，由数据域钉住机制自动强制
// 走内部 Qwen——全文不出内网。
type SearchTool struct {
	Index *Index
}

func (t *SearchTool) Name() string { return "rag_search" }

func (t *SearchTool) Description() string {
	return "在本地已索引的机密文档（招标书/合同/内部资料）中按问题检索相关片段。检索在本地进行，内容不出内网；返回最相关的段落供分析。索引未配置时返回空。"
}

func (t *SearchTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {"type": "string", "description": "检索问题，如：评标标准是什么"},
			"top_n": {"type": "integer", "description": "返回片段数（默认 3，最大 8）"}
		},
		"required": ["query"]
	}`)
}

// ReadOnly：本地只读检索，无副作用（可并行）。
func (t *SearchTool) ReadOnly() bool { return true }

func (t *SearchTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
		TopN  int    `json:"top_n"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("rag_search: bad args: %w", err)
	}
	if strings.TrimSpace(in.Query) == "" {
		return "", fmt.Errorf("rag_search: query is required")
	}
	if t.Index == nil || t.Index.Len() == 0 {
		return "（本地文档索引为空：请先由架构师索引机密文档）", nil
	}
	topN := in.TopN
	if topN <= 0 {
		topN = 3
	}
	if topN > 8 {
		topN = 8
	}
	hits := t.Index.Search(in.Query, topN)
	if len(hits) == 0 {
		return "（本地文档中未检索到相关内容）", nil
	}
	var sb strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&sb, "【片段 %d】\n%s\n\n", i+1, h.Text)
	}
	return strings.TrimSpace(sb.String()), nil
}

var _ tool.Tool = (*SearchTool)(nil)
