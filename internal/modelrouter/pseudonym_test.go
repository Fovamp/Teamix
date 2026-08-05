package modelrouter

import (
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestPseudonymStableMapping(t *testing.T) {
	p := NewPseudonymizer()
	// 同一真实值多次出现 → 同一假名（稳定映射，模型才能做指代推理）
	a := p.Apply("张三 13800138000 张三")
	b := p.Apply("李四 13800138000")
	if strings.Contains(a, "13800138000") || strings.Contains(b, "13800138000") {
		t.Fatalf("phone not pseudonymized: %q / %q", a, b)
	}
	if !strings.Contains(a, "PH1") || !strings.Contains(b, "PH1") {
		t.Fatalf("same phone should map to same pseudonym: %q / %q", a, b)
	}
}

func TestPseudonymPreservesStructure(t *testing.T) {
	p := NewPseudonymizer()
	// 人名靠字典（正则无法识别人名——判定第二层：内容级 NER/字典）
	p.AddDict("张三", "客户甲")
	in := "客户张三在2026年1月5日支付了12345678元"
	out := p.Apply(in)
	// 句法结构保留（占位符保形），敏感值替换
	if strings.Contains(out, "张三") {
		t.Errorf("person leaked: %q", out)
	}
	if !strings.Contains(out, "客户甲") {
		t.Errorf("dict pseudonym not applied: %q", out)
	}
	if !strings.Contains(out, "DT1") {
		t.Errorf("date not pseudonymized: %q", out)
	}
	if !strings.Contains(out, "在") || !strings.Contains(out, "支付了") {
		t.Errorf("syntax broken: %q", out)
	}
}

func TestPseudonymAmountKeepsMagnitude(t *testing.T) {
	p := NewPseudonymizer()
	out := p.Apply("合同金额 12345678 元")
	if strings.Contains(out, "12345678") {
		t.Errorf("amount leaked: %q", out)
	}
	if !strings.Contains(out, "AMT") {
		t.Errorf("amount not pseudonymized: %q", out)
	}
}

func TestPseudonymRestore(t *testing.T) {
	p := NewPseudonymizer()
	in := "张三的号码 13800138000"
	fake := p.Apply(in)
	real, ok := p.RestoreWithCheck(fake)
	if !ok {
		t.Fatalf("restore check failed on %q", fake)
	}
	if real != in {
		t.Errorf("restore = %q, want %q", real, in)
	}
}

func TestPseudonymRestoreDetectsTamper(t *testing.T) {
	p := NewPseudonymizer()
	p.Apply("张三 13800138000")
	// 模型幻觉出映射表里不存在的假名（PH99）→ 还原不完整 → 校验失败
	hallucinated := "根据分析，号码是 PH99"
	_, ok := p.RestoreWithCheck(hallucinated)
	if ok {
		t.Errorf("hallucinated pseudonym should fail restore check: %q", hallucinated)
	}
}

func TestPseudonymKeepsMappingCount(t *testing.T) {
	p := NewPseudonymizer()
	p.Apply("13800138000")
	p.Apply("13900139000")
	if p.Len() != 2 {
		t.Errorf("mapping count = %d, want 2", p.Len())
	}
}

func TestPseudonymApplyMessages(t *testing.T) {
	p := NewPseudonymizer()
	msgs := []provider.Message{{
		Role:    provider.RoleUser,
		Content: "帮我查 13800138000 的账户",
	}}
	out := p.ApplyMessages(msgs)
	if out[0].Content == msgs[0].Content {
		t.Errorf("messages not pseudonymized")
	}
	if strings.Contains(out[0].Content, "13800138000") {
		t.Errorf("phone leaked in messages")
	}
	// 原消息不变
	if !strings.Contains(msgs[0].Content, "13800138000") {
		t.Errorf("original mutated")
	}
}
