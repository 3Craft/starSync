package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "syncs.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_ParsesSyncs(t *testing.T) {
	p := writeTemp(t, `
syncs:
  - from: xsharp
    to: [justdn, trendcms]
  - from: trendcms
    to: [xsharp]
    mirror: true
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(cfg.Syncs) != 2 {
		t.Fatalf("应解析 2 组, 实际 %d", len(cfg.Syncs))
	}
	if cfg.Syncs[0].From != "xsharp" || len(cfg.Syncs[0].To) != 2 {
		t.Fatalf("第一组解析错误: %+v", cfg.Syncs[0])
	}
	// 未指定 resource 时默认 stars（向后兼容）
	if cfg.Syncs[0].Resource != "stars" {
		t.Fatalf("默认 resource 应为 stars, 实际 %q", cfg.Syncs[0].Resource)
	}
	if !cfg.Syncs[1].Mirror {
		t.Fatalf("第二组 mirror 应为 true")
	}
}

func TestLoad_DefaultsResourceAndAcceptsKnown(t *testing.T) {
	p := writeTemp(t, `
syncs:
  - resource: gists
    from: xsharp
    to: [a]
  - resource: following
    from: xsharp
    to: [b]
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if cfg.Syncs[0].Resource != "gists" || cfg.Syncs[1].Resource != "following" {
		t.Fatalf("resource 字段未正确解析: %+v %+v", cfg.Syncs[0], cfg.Syncs[1])
	}
}

func TestLoad_RejectsUnknownResource(t *testing.T) {
	p := writeTemp(t, `
syncs:
  - resource: repos
    from: xsharp
    to: [a]
`)
	if _, err := Load(p); err == nil {
		t.Fatal("未知 resource 应报错")
	}
}

func TestLoad_RejectsEmptyFrom(t *testing.T) {
	p := writeTemp(t, "syncs:\n  - to: [a]\n")
	if _, err := Load(p); err == nil {
		t.Fatal("from 为空应报错")
	}
}

func TestLoad_RejectsEmptySyncs(t *testing.T) {
	p := writeTemp(t, "syncs: []\n")
	if _, err := Load(p); err == nil {
		t.Fatal("空 syncs 应报错")
	}
}
