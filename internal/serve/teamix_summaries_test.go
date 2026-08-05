package serve

import "testing"

func TestParseSummaryOutput(t *testing.T) {
	cases := []struct {
		in              string
		wantTitle       string
		wantDescription string
		wantBody        string
	}{
		{
			in:              "<title>猫娘助理</title>\n<description>她介绍了自己。</description>\n<content>她是一位猫娘助理，擅长卖萌。</content>",
			wantTitle:       "猫娘助理",
			wantDescription: "她介绍了自己。",
			wantBody:        "她是一位猫娘助理，擅长卖萌。",
		},
		{
			// 缺 content：正文回退到 description
			in:              "<title>数字记录</title>\n<description>用户依次给出四个数字，助手只确认未操作。</description>",
			wantTitle:       "数字记录",
			wantDescription: "用户依次给出四个数字，助手只确认未操作。",
			wantBody:        "用户依次给出四个数字，助手只确认未操作。",
		},
		{
			// 只有 description：标题空，正文回退
			in:              "<description>只有描述没有标题</description>",
			wantDescription: "只有描述没有标题",
			wantBody:        "只有描述没有标题",
		},
		{
			// 完全没有标签：整段当正文
			in:       "没有标签的原始输出",
			wantBody: "没有标签的原始输出",
		},
		{
			// 空标题不算标题
			in:              "<title>   </title>\n<description>正文</description>",
			wantDescription: "正文",
			wantBody:        "正文",
		},
	}
	for _, c := range cases {
		title, desc, body := parseSummaryOutput(c.in)
		if title != c.wantTitle || desc != c.wantDescription || body != c.wantBody {
			t.Errorf("parseSummaryOutput(%q) = (%q, %q, %q), want (%q, %q, %q)",
				c.in, title, desc, body, c.wantTitle, c.wantDescription, c.wantBody)
		}
	}
}
