package cli

import "testing"

func TestResolvePairs_FlagFanout(t *testing.T) {
	ps, err := resolvePairs("src", []string{"a", "b"}, "", false)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(ps) != 2 || ps[0].from != "src" || ps[0].to != "a" || ps[1].to != "b" {
		t.Fatalf("展开错误: %+v", ps)
	}
}

func TestResolvePairs_ConfigAndFlagsMutuallyExclusive(t *testing.T) {
	if _, err := resolvePairs("src", []string{"a"}, "cfg.yaml", false); err == nil {
		t.Fatal("--config 与 --from/--to 并用应报错")
	}
}

func TestResolvePairs_RequiresFromAndTo(t *testing.T) {
	if _, err := resolvePairs("", nil, "", false); err == nil {
		t.Fatal("缺少 from/to 应报错")
	}
}
