package serve

import (
	"bufio"
	"strings"
	"testing"
)

func TestCloneProgressParse(t *testing.T) {
	// 模拟 git clone --progress 的 stderr（进度行以 \r 结尾、不换行）
	r := strings.NewReader("Cloning into 'x'...\nReceiving objects:   0% (1/50519), 0.1 MiB\rReceiving objects:  45% (1234/2742), 5.2 MiB | 1.2 MiB/s\rReceiving objects: 100% (50519/50519), 78.1 MiB | 3.4 MiB/s, done.\n")
	br := bufio.NewReader(r)
	var frames []string
	for {
		line, err := br.ReadString('\r')
		if m := receivingRe.FindStringSubmatch(line); m != nil {
			frames = append(frames, m[1]+"% ("+m[2]+"/"+m[3]+")")
		}
		if err != nil {
			break
		}
	}
	t.Logf("解析到帧: %v", frames)
	if len(frames) == 0 {
		t.Fatal("未解析到任何进度帧")
	}
}
