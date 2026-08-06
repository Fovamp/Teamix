package serve

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSummaryOutput(t *testing.T) {
	cases := []struct {
		in              string
		wantTitle       string
		wantDescription string
		wantBody        string
	}{
		{
			in:              "<title>\u732b\u5a18\u52a9\u7406</title>\n<description>\u5979\u4ecb\u7ecd\u4e86\u81ea\u5df1\u3002</description>\n<content>\u5979\u662f\u4e00\u4f4d\u732b\u5a18\u52a9\u7406\uff0c\u64c5\u957f\u5356\u840c\u3002</content>",
			wantTitle:       "\u732b\u5a18\u52a9\u7406",
			wantDescription: "\u5979\u4ecb\u7ecd\u4e86\u81ea\u5df1\u3002",
			wantBody:        "\u5979\u662f\u4e00\u4f4d\u732b\u5a18\u52a9\u7406\uff0c\u64c5\u957f\u5356\u840c\u3002",
		},
		{
			// missing content: body falls back to description
			in:              "<title>\u6570\u5b57\u8bb0\u5f55</title>\n<description>\u7528\u6237\u4f9d\u6b21\u7ed9\u51fa\u56db\u4e2a\u6570\u5b57\uff0c\u52a9\u624b\u53ea\u786e\u8ba4\u672a\u64cd\u4f5c\u3002</description>",
			wantTitle:       "\u6570\u5b57\u8bb0\u5f55",
			wantDescription: "\u7528\u6237\u4f9d\u6b21\u7ed9\u51fa\u56db\u4e2a\u6570\u5b57\uff0c\u52a9\u624b\u53ea\u786e\u8ba4\u672a\u64cd\u4f5c\u3002",
			wantBody:        "\u7528\u6237\u4f9d\u6b21\u7ed9\u51fa\u56db\u4e2a\u6570\u5b57\uff0c\u52a9\u624b\u53ea\u786e\u8ba4\u672a\u64cd\u4f5c\u3002",
		},
		{
			// only description: empty title, body falls back
			in:              "<description>\u53ea\u6709\u63cf\u8ff0\u6ca1\u6709\u6807\u9898</description>",
			wantDescription: "\u53ea\u6709\u63cf\u8ff0\u6ca1\u6709\u6807\u9898",
			wantBody:        "\u53ea\u6709\u63cf\u8ff0\u6ca1\u6709\u6807\u9898",
		},
		{
			// no tags at all: whole content as body
			in:       "\u6ca1\u6709\u6807\u7b7e\u7684\u539f\u59cb\u8f93\u51fa",
			wantBody: "\u6ca1\u6709\u6807\u7b7e\u7684\u539f\u59cb\u8f93\u51fa",
		},
		{
			// empty title does not count
			in:              "<title>   </title>\n<description>\u6b63\u6587</description>",
			wantDescription: "\u6b63\u6587",
			wantBody:        "\u6b63\u6587",
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

func TestSummaryFileRelativeWorkspace(t *testing.T) {
	// workspaceRoot being a relative path must still yield a real session file, not _invalid
	ts := &TeamixServer{workspaceRoot: "relative/path"}
	f := ts.summaryFile("C:/users/zhangsan", "proj-a", "0805-1234")
	if filepath.Base(f) != "0805-1234.json" {
		t.Fatalf("relative workspaceRoot: summaryFile = %q, want real session file", f)
	}
	// 先类型后项目：路径包含 <userRoot>/.teamix/summaries/<project>/<session>.json
	if !strings.Contains(filepath.ToSlash(f), "users/zhangsan/.teamix/summaries/proj-a/0805-1234.json") {
		t.Fatalf("summaryFile should be user+project-scoped: %q", f)
	}
}

func TestAllSummariesRequiresProject(t *testing.T) {
	// 未选择项目：总结返回空（前端提示先选项目）
	ts := &TeamixServer{workspaceRoot: t.TempDir()}
	u := &userSession{name: "zhangsan", selectedProject: ""}
	out := ts.allSummaries(u)
	if len(out) != 0 {
		t.Fatalf("no project selected: summaries = %d, want 0", len(out))
	}
}
