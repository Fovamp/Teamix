// Package rag 提供本地 BM25 检索（RAG），用于机密大文档（招标书等）的
// 逐段分析：全文不出内网，按问题检索相关片段，片段经工具结果进入上下文后
// 由数据域钉住机制自动强制走内部 Qwen。
//
// 复用 internal/retrieval 的 BM25（含 CJK 单字切分，中文友好），无 embedding
// 模型依赖——纯本地、离线可用。
package rag

import (
	"sort"
	"strings"

	"reasonix/internal/retrieval"
)

// chunkSize / chunkOverlap 分块参数：控制送入模型的片段大小（中文 1 字符
// ≈0.75 token，2KB 块约 1.5K token，Qwen 窗口内安全）。
const (
	chunkSize    = 2000
	chunkOverlap = 200
)

// Chunk 是索引中的一个片段。
type Chunk struct {
	Text   string
	counts map[string]int
	length int
}

// Hit 是检索结果。
type Hit struct {
	Score float64
	Text  string // 命中片段（trimmed）
}

// Index 是内存中的本地文档索引（随会话/进程生命周期）。
type Index struct {
	chunks    []Chunk
	df        map[string]int
	totalDocs int
	avgLen    float64
}

func New() *Index {
	return &Index{df: map[string]int{}}
}

// Add 把一份文档分块后加入索引（本地内存，全文不出内网）。
func (ix *Index) Add(content string) {
	for _, text := range chunkText(content) {
		terms := retrieval.Tokens(text)
		ix.chunks = append(ix.chunks, Chunk{
			Text:   text,
			counts: retrieval.Counts(terms),
			length: len(terms),
		})
	}
	ix.rebuildStats()
}

// AddFile 便捷方法：Add(string(content))。文件读取由调用方负责
// （读取本身走文件工具，敏感标记由数据域钉住覆盖）。
func (ix *Index) AddFile(content string) { ix.Add(content) }

// Len 返回分块数。
func (ix *Index) Len() int { return len(ix.chunks) }

// Search 按查询返回最相关的 topN 个片段（BM25 打分 + 相对阈值裁剪）。
func (ix *Index) Search(query string, topN int) []Hit {
	terms, err := retrieval.QueryTerms(query)
	if err != nil || len(ix.chunks) == 0 {
		return nil
	}
	type scored struct {
		hit  Hit
		text string
	}
	all := make([]scored, 0, len(ix.chunks))
	for i := range ix.chunks {
		c := &ix.chunks[i]
		score := retrieval.BM25Score(c.counts, c.length, terms, ix.df, ix.totalDocs, ix.avgLen)
		if score <= 0 {
			continue
		}
		all = append(all, scored{hit: Hit{Score: score}, text: c.Text})
	}
	if len(all) == 0 {
		return nil
	}
	sort.Slice(all, func(i, j int) bool { return all[i].hit.Score > all[j].hit.Score })
	// 相对阈值裁剪：保留最佳片段 + 分数不低于 top*0.15 的（过滤噪音）
	kept := retrieval.KeepTopRelativeScore(all, 0.15, func(s scored) float64 { return s.hit.Score })
	if len(kept) > topN {
		kept = kept[:topN]
	}
	out := make([]Hit, 0, len(kept))
	for _, s := range kept {
		s.hit.Text = retrieval.CompactWhitespace(strings.TrimSpace(s.text))
		out = append(out, s.hit)
	}
	return out
}

// chunkText 把长文本切成有重叠的分块（块尾断句处保持完整）。
func chunkText(text string) []string {
	runes := []rune(text)
	if len(runes) <= chunkSize {
		return []string{text}
	}
	var out []string
	start := 0
	for start < len(runes) {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		} else {
			// 在块尾找最近的句子边界（。！？\n），避免切断语义
			for i := end; i > start+chunkSize/2; i-- {
				switch runes[i-1] {
				case '。', '！', '？', '；', '\n':
					end = i
					goto cut
				}
			}
		}
	cut:
		out = append(out, string(runes[start:end]))
		if end >= len(runes) {
			break
		}
		next := end - chunkOverlap
		if next <= start {
			next = start + 1
		}
		start = next
	}
	return out
}

// rebuildStats 重建 df/totalDocs/avgLen（分块集合变化后调用）。
func (ix *Index) rebuildStats() {
	countsList := make([]map[string]int, len(ix.chunks))
	totalTerms := 0
	for i := range ix.chunks {
		countsList[i] = ix.chunks[i].counts
		totalTerms += ix.chunks[i].length
	}
	ix.df = retrieval.DocumentFrequency(countsList)
	ix.totalDocs = len(ix.chunks)
	if ix.totalDocs > 0 {
		ix.avgLen = float64(totalTerms) / float64(ix.totalDocs)
	}
}
