package rag

import (
	"strings"
	"testing"
)

func TestIndexSearchRelevantChunk(t *testing.T) {
	ix := New()
	ix.Add(`
招标公告：某银行核心系统升级项目，预算 1.2 亿元。
评标标准：技术方案占 40%，商务报价占 30%，售后服务占 20%，企业资质占 10%。
投标截止时间 2026 年 9 月 30 日。
    `)
	ix.Add(`
公司介绍：本司成立于 2010 年，拥有 CMMI5 认证，服务过 30 余家银行客户。
项目团队 80 人，含架构师 10 名。
    `)

	hits := ix.Search("技术方案 评标 标准", 3)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	top := hits[0]
	if !strings.Contains(top.Text, "评标标准") && !strings.Contains(top.Text, "技术方案") {
		t.Errorf("top hit not relevant: %q", top.Text)
	}
	if !strings.Contains(top.Text, "40%") {
		t.Errorf("top hit missing detail: %q", top.Text)
	}
}

func TestIndexSearchEmpty(t *testing.T) {
	ix := New()
	if hits := ix.Search("任何查询", 3); len(hits) != 0 {
		t.Errorf("empty index should return nothing, got %d", len(hits))
	}
}

func TestIndexSearchNoise(t *testing.T) {
	ix := New()
	ix.Add("本项目投标人须满足以下条件：营业执照、税务登记、组织机构代码证。")
	ix.Add("一般的日常闲聊内容，与招标完全无关。")

	hits := ix.Search("营业执照 招标 条件", 3)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if !strings.Contains(hits[0].Text, "营业执照") {
		t.Errorf("top hit should contain 营业执照: %q", hits[0].Text)
	}
}

func TestChunkingLongDoc(t *testing.T) {
	// 超长文档应被切块（大招标书场景）
	long := strings.Repeat("这是招标文档的关键内容之一。", 500) // ~2.2 万字符
	ix := New()
	ix.Add(long)
	if ix.Len() <= 1 {
		t.Fatalf("long doc should be chunked, got %d chunks", ix.Len())
	}
	hits := ix.Search("招标 文档 关键", 5)
	if len(hits) == 0 {
		t.Fatal("chunked doc should still be searchable")
	}
}
