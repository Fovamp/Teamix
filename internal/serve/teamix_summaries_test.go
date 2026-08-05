package serve

import "testing"

func TestParseSummaryOutput(t *testing.T) {
	cases := []struct {
		in        string
		wantTitle string
		wantRest  string
	}{
		{
			in:        "<title>猫娘助理</title>\n<description>她介绍了自己。</description>",
			wantTitle: "猫娘助理",
			wantRest:  "她介绍了自己。",
		},
		{
			in:        "<title>数字记录</title>\n\n<description>用户依次给出四个数字，助手只确认未操作。</description>",
			wantTitle: "数字记录",
			wantRest:  "用户依次给出四个数字，助手只确认未操作。",
		},
		{
			// 标题缺失：整段当正文，不猜标题
			in:       "<description>只有正文没有标题</description>",
			wantRest: "只有正文没有标题",
		},
		{
			// 完全没有标签：整段当正文
			in:       "没有标签的原始输出",
			wantRest: "没有标签的原始输出",
		},
		{
			// 空标题不算标题
			in:       "<title>   </title>\n<description>正文</description>",
			wantRest: "正文",
		},
	}
	for _, c := range cases {
		title, rest := parseSummaryOutput(c.in)
		if title != c.wantTitle || rest != c.wantRest {
			t.Errorf("parseSummaryOutput(%q) = (%q, %q), want (%q, %q)", c.in, title, rest, c.wantTitle, c.wantRest)
		}
	}
}
