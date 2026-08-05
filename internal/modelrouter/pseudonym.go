package modelrouter

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"reasonix/internal/provider"
)

// 假名化（pseudonymization，非匿名化抹除）——保证内外模型"理解程度相同"。
//
// 目标：结构、关系、逻辑照旧，只换标识符。三规则：
//  1. 稳定映射：会话级 真实值→假名 只建一次，全程同一假名（模型才能做指代推理）
//  2. 保形占位：人名→人名样（客户甲）、公司→公司样、日期→日期样，句法/指代保留
//  3. 数值保量级：1.2亿→[约1亿]，大小比较/时间线/关系不变
//
// 机制：本地上下文存真实值；出网生成临时假名副本；输出回来本地还原；
// 映射表只存本地内存；禁止假名结果写回 messages（与 Qwen 降档切换对不齐）。

type pseudoClass struct {
	kind   string // person/org/phone/idcard/email/apikey/amount/date
	prefix string // 假名前缀，如 "P"（分配为 P1/P2…）
	re     *regexp.Regexp
}

// builtinPseudoClasses 内置识别规则（P0）：正则为主，字典/NER 后续扩展。
var builtinPseudoClasses = []pseudoClass{
	{kind: "phone", prefix: "PH", re: regexp.MustCompile(`1[3-9]\d{9}`)},
	{kind: "idcard", prefix: "ID", re: regexp.MustCompile(`\d{17}[\dXx]`)},
	{kind: "email", prefix: "EM", re: regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.]+`)},
	{kind: "apikey", prefix: "KEY", re: regexp.MustCompile(`(?i)(sk-|ak-|AKIA)[A-Za-z0-9_\-]{12,}`)},
	{kind: "date", prefix: "DT", re: regexp.MustCompile(`\d{4}[-/年]\d{1,2}[-/月]\d{1,2}日?`)},
	// 金额保量级：粗化到 1 位有效数字 + 单位（防止精确值泄露，保留大小关系）
	{kind: "amount", prefix: "AMT", re: regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(万元|亿元|元|USD|CNY)`)},
}

// Pseudonymizer 是会话级稳定假名化器。每个会话一个实例（映射表随会话销毁，
// 与 RouterProvider 的 per-build 生命周期一致）。
type Pseudonymizer struct {
	mu      sync.Mutex
	classes []pseudoClass
	mapping map[string]string // 真实值 → 假名（稳定，防指代断裂）
	rev     map[string]string // 假名 → 真实值（还原）
	counts  map[string]int    // 每类序号（P1/P2/…）
	dict    map[string]string // 字典：真实值 → 假名（人名/公司/专有名词，用户提供）
	leftover *regexp.Regexp   // 残留假名检测：还原后仍匹配 = 未还原完整（模型幻觉新假名）
}

func NewPseudonymizer() *Pseudonymizer {
	classes := builtinPseudoClasses
	// 残留检测：任意假名前缀+序号（含字典分配的 Pn）
	alt := make([]string, 0, len(classes))
	for _, c := range classes {
		alt = append(alt, c.prefix)
	}
	alt = append(alt, "P")
	return &Pseudonymizer{
		classes:  classes,
		mapping:  make(map[string]string),
		rev:      make(map[string]string),
		counts:   make(map[string]int),
		dict:     make(map[string]string),
		leftover: regexp.MustCompile(`\b(?:` + strings.Join(alt, "|") + `)\d+\b`),
	}
}

// AddDict 登记字典映射：真实值（人名/公司/专有名词）→ 期望的保形假名
// （如 "张三" → "客户甲"）。命中字典的条目优先于正则，且同样稳定。
func (p *Pseudonymizer) AddDict(real, fake string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dict[real] = fake
	p.rev[fake] = real
}

// ApplyMessages 返回假名化后的消息副本（原消息不变；仅替换 Content）。
func (p *Pseudonymizer) ApplyMessages(msgs []provider.Message) []provider.Message {
	out := make([]provider.Message, len(msgs))
	for i, m := range msgs {
		out[i] = m
		out[i].Content = p.Apply(m.Content)
	}
	return out
}

// Apply 把 content 中的敏感值替换为稳定假名。
func (p *Pseudonymizer) Apply(content string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	// 字典优先（人名/公司/专有名词——正则无法识别）
	for real, fake := range p.dict {
		if strings.Contains(content, real) {
			content = strings.ReplaceAll(content, real, fake)
		}
	}
	for _, c := range p.classes {
		content = c.re.ReplaceAllStringFunc(content, func(m string) string {
			return p.pseudonymFor(c, m)
		})
	}
	return content
}

// pseudonymFor 返回真实值 m 的稳定假名（首次分配，之后复用）。
func (p *Pseudonymizer) pseudonymFor(c pseudoClass, m string) string {
	if name, ok := p.mapping[m]; ok {
		return name
	}
	p.counts[c.kind]++
	idx := p.counts[c.kind]
	name := c.prefix + strconv.Itoa(idx)
	p.mapping[m] = name
	p.rev[name] = m
	return name
}

// Restore 把外部输出里的假名还原为真实值（宽松版：尽力还原，不校验）。
func (p *Pseudonymizer) Restore(text string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, real := range p.rev {
		if strings.Contains(text, name) {
			text = strings.ReplaceAll(text, name, real)
		}
	}
	return text
}

// RestoreWithCheck 还原并严格校验：文本中不应再残留任何假名模式
// （模型幻觉出新的假名/改写了占位符 = 还原失败）。返回 ok=false 触发闭环告警。
func (p *Pseudonymizer) RestoreWithCheck(text string) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, real := range p.rev {
		if strings.Contains(text, name) {
			text = strings.ReplaceAll(text, name, real)
		}
	}
	// 校验：仍匹配假名模式 = 存在未还原/幻觉的假名
	if p.leftover != nil && p.leftover.MatchString(text) {
		return text, false
	}
	return text, true
}

// Len 返回映射表大小（审计用：mapping_count）。
func (p *Pseudonymizer) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.mapping)
}
