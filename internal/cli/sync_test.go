package cli

import (
	"context"
	"testing"

	syncpkg "github.com/xsharp/starsync/internal/sync"
)

// cliSyncer 是 cli 包内用于测试 runPairs 的内存 Syncer 实现。
type cliSyncer struct {
	data map[string]syncpkg.Set // user -> 已有条目
	rms  []string               // 记录 Remove 调用 "user:item"
	adds []string               // 记录 Add 调用 "user:item"
}

func newCliSyncer() *cliSyncer {
	return &cliSyncer{data: map[string]syncpkg.Set{}}
}

func (c *cliSyncer) Name() string { return "stars" }

func (c *cliSyncer) set(u string) syncpkg.Set {
	if c.data[u] == nil {
		c.data[u] = syncpkg.Set{}
	}
	return c.data[u]
}

func (c *cliSyncer) List(_ context.Context, a syncpkg.Account) (syncpkg.Set, error) {
	return c.set(a.User), nil
}

func (c *cliSyncer) Add(_ context.Context, a syncpkg.Account, it syncpkg.Item) error {
	c.set(a.User).Add(it)
	c.adds = append(c.adds, a.User+":"+string(it))
	return nil
}

func (c *cliSyncer) Remove(_ context.Context, a syncpkg.Account, it syncpkg.Item) error {
	delete(c.set(a.User), it)
	c.rms = append(c.rms, a.User+":"+string(it))
	return nil
}

// confirmFalse 始终拒绝，用于验证"跳过"路径。
func confirmFalse(_, _ string, _ []syncpkg.Item) bool { return false }

// confirmTrue 始终同意，用于验证"执行"路径。
func confirmTrue(_, _ string, _ []syncpkg.Item) bool { return true }

// TestRunPairs_MirrorConfirmFalse_SkipsRemove 验证 confirm 返回 false 时不执行 Remove。
func TestRunPairs_MirrorConfirmFalse_SkipsRemove(t *testing.T) {
	s := newCliSyncer()
	s.data["src"] = syncpkg.Set{"a/1": {}}
	s.data["dst"] = syncpkg.Set{"a/1": {}, "a/9": {}} // a/9 需删除

	pairs := []pair{{from: "src", to: "dst", mirror: true}}
	err := runPairs(context.Background(), s, pairs, false, false, false, confirmFalse)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(s.rms) != 0 {
		t.Fatalf("confirm=false 时不应调用 Remove，实际 rms=%v", s.rms)
	}
	// a/9 应仍然存在于 dst
	if !s.set("dst").Has("a/9") {
		t.Fatal("confirm=false 时 a/9 不应被删除")
	}
}

// TestRunPairs_MirrorConfirmTrue_ExecutesRemove 验证 confirm 返回 true 时执行 Remove。
func TestRunPairs_MirrorConfirmTrue_ExecutesRemove(t *testing.T) {
	s := newCliSyncer()
	s.data["src"] = syncpkg.Set{"a/1": {}}
	s.data["dst"] = syncpkg.Set{"a/1": {}, "a/9": {}} // a/9 需删除

	pairs := []pair{{from: "src", to: "dst", mirror: true}}
	err := runPairs(context.Background(), s, pairs, false, false, false, confirmTrue)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if len(s.rms) != 1 || s.rms[0] != "dst:a/9" {
		t.Fatalf("confirm=true 时应删除 dst:a/9，实际 rms=%v", s.rms)
	}
	if s.set("dst").Has("a/9") {
		t.Fatal("confirm=true 后 a/9 应已被删除")
	}
}

// TestRunPairs_MirrorNoRemoved_SkipsConfirm 验证 removed 为空时不调用 confirm。
func TestRunPairs_MirrorNoRemoved_SkipsConfirm(t *testing.T) {
	s := newCliSyncer()
	s.data["src"] = syncpkg.Set{"a/1": {}, "a/2": {}}
	s.data["dst"] = syncpkg.Set{"a/1": {}} // 只缺 a/2，无多余条目

	confirmCalled := false
	confirm := func(_, _ string, _ []syncpkg.Item) bool {
		confirmCalled = true
		return false
	}

	pairs := []pair{{from: "src", to: "dst", mirror: true}}
	if err := runPairs(context.Background(), s, pairs, false, false, false, confirm); err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if confirmCalled {
		t.Fatal("removed 为空时不应调用 confirm")
	}
	// a/2 应被新增
	if !s.set("dst").Has("a/2") {
		t.Fatal("a/2 应被同步到 dst")
	}
}
