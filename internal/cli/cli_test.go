package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePairs_FlagFanout(t *testing.T) {
	ps, err := resolvePairs("src", []string{"a", "b"}, "", false, "stars")
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(ps) != 2 || ps[0].from != "src" || ps[0].to != "a" || ps[1].to != "b" {
		t.Fatalf("展开错误: %+v", ps)
	}
	if ps[0].resource != "stars" {
		t.Fatalf("resource 应为 stars, 实际 %q", ps[0].resource)
	}
}

func TestResolvePairs_ConfigAndFlagsMutuallyExclusive(t *testing.T) {
	if _, err := resolvePairs("src", []string{"a"}, "cfg.yaml", false, "stars"); err == nil {
		t.Fatal("--config 与 --from/--to 并用应报错")
	}
}

func TestResolvePairs_RequiresFromAndTo(t *testing.T) {
	if _, err := resolvePairs("", nil, "", false, "stars"); err == nil {
		t.Fatal("缺少 from/to 应报错")
	}
}

// TestResolvePairs_FiltersConfigByResource 验证配置文件里其它 resource 的条目被过滤掉。
func TestResolvePairs_FiltersConfigByResource(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "syncs.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
syncs:
  - from: xsharp
    to: [a]
  - resource: gists
    from: xsharp
    to: [b]
  - resource: following
    from: xsharp
    to: [c]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	ps, err := resolvePairs("", nil, cfgPath, false, "gists")
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(ps) != 1 || ps[0].resource != "gists" || ps[0].to != "b" {
		t.Fatalf("应只展开 gists 条目: %+v", ps)
	}
}

func TestResolvePairs_ConfigEmptyForResource_Errors(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "syncs.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
syncs:
  - from: xsharp
    to: [a]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolvePairs("", nil, cfgPath, false, "gists"); err == nil {
		t.Fatal("配置中没有目标 resource 应报错")
	}
}
