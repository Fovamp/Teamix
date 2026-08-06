package teamixconfig

import "testing"

func TestMergeScalars(t *testing.T) {
	global := &Config{Teamix: TeamixConfig{Name: "Teamix", DefaultModel: "deepseek-v3"}}
	user := &UserConfig{Preferences: Preferences{Language: "en"}}
	m := Merge(global, user)
	// 模型仅公共可配：私有配置不再能覆盖（公司统一 token）
	if m.DefaultModel != "deepseek-v3" {
		t.Errorf("model should come from global only, got %q", m.DefaultModel)
	}
	if m.Language != "en" {
		t.Errorf("language should be user's, got %q", m.Language)
	}

	// 公共未配置 → 回落为空（由启动参数兜底）
	m2 := Merge(nil, &UserConfig{Preferences: Preferences{Language: "en"}})
	if m2.DefaultModel != "" {
		t.Errorf("empty global model should stay empty, got %q", m2.DefaultModel)
	}
	if m2.Language != "en" {
		t.Errorf("language mismatch, got %q", m2.Language)
	}

	// 全部为空 → 默认
	m3 := Merge(nil, nil)
	if m3.DefaultModel != "" || m3.Language != "zh" {
		t.Errorf("defaults wrong: model=%q lang=%q", m3.DefaultModel, m3.Language)
	}
}
