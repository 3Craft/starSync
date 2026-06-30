package sync

import (
	"context"
	"errors"
	"testing"
)

// fakeSyncer 是内存实现，用于不触网地测试引擎逻辑。
type fakeSyncer struct {
	data   map[string]Set    // user -> 已有条目
	addErr map[Item]error    // 指定条目 Add 时返回错误
	adds   []string          // 记录 Add 调用 "user:item"
	rms    []string          // 记录 Remove 调用 "user:item"
}

func newFake() *fakeSyncer {
	return &fakeSyncer{data: map[string]Set{}, addErr: map[Item]error{}}
}
func (f *fakeSyncer) Name() string { return "stars" }
func (f *fakeSyncer) set(u string) Set {
	if f.data[u] == nil {
		f.data[u] = Set{}
	}
	return f.data[u]
}
func (f *fakeSyncer) List(_ context.Context, a Account) (Set, error) { return f.set(a.User), nil }
func (f *fakeSyncer) Add(_ context.Context, a Account, it Item) error {
	if e := f.addErr[it]; e != nil {
		return e
	}
	f.set(a.User).Add(it)
	f.adds = append(f.adds, a.User+":"+string(it))
	return nil
}
func (f *fakeSyncer) Remove(_ context.Context, a Account, it Item) error {
	delete(f.set(a.User), it)
	f.rms = append(f.rms, a.User+":"+string(it))
	return nil
}

func TestSync_UnionAddsMissingAndSkipsExisting(t *testing.T) {
	f := newFake()
	f.data["src"] = Set{"a/1": {}, "a/2": {}, "a/3": {}}
	f.data["dst"] = Set{"a/2": {}} // 已有 a/2

	rep, err := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeUnion, false)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if rep.SourceCount != 3 || rep.TargetBefore != 1 {
		t.Fatalf("计数错误: %+v", rep)
	}
	if got := len(rep.Added); got != 2 { // a/1, a/3
		t.Fatalf("Added 应为 2, 实际 %d (%v)", got, rep.Added)
	}
	if rep.Skipped != 1 {
		t.Fatalf("Skipped 应为 1, 实际 %d", rep.Skipped)
	}
	if len(rep.Removed) != 0 {
		t.Fatalf("union 不应删除, 实际 %v", rep.Removed)
	}
	if !f.set("dst").Has("a/1") || !f.set("dst").Has("a/3") {
		t.Fatalf("dst 未补齐: %v", f.data["dst"])
	}
}

func TestSync_DryRunDoesNotWrite(t *testing.T) {
	f := newFake()
	f.data["src"] = Set{"a/1": {}}
	f.data["dst"] = Set{}

	rep, _ := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeUnion, true)
	if len(rep.Added) != 1 || !rep.DryRun {
		t.Fatalf("dry-run 应报告 1 项新增: %+v", rep)
	}
	if len(f.adds) != 0 {
		t.Fatalf("dry-run 不应真正写入, 实际调用 %v", f.adds)
	}
}

func TestSync_MirrorRemovesExtraneous(t *testing.T) {
	f := newFake()
	f.data["src"] = Set{"a/1": {}}
	f.data["dst"] = Set{"a/1": {}, "a/9": {}} // a/9 是多余的

	rep, _ := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeMirror, false)
	if len(rep.Removed) != 1 || rep.Removed[0] != "a/9" {
		t.Fatalf("mirror 应删除 a/9, 实际 %v", rep.Removed)
	}
	if f.set("dst").Has("a/9") {
		t.Fatalf("a/9 未被删除")
	}
}

func TestSync_ItemFailureRecordedNotFatal(t *testing.T) {
	f := newFake()
	f.data["src"] = Set{"a/1": {}, "a/2": {}}
	f.data["dst"] = Set{}
	f.addErr["a/1"] = errors.New("boom")

	rep, err := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeUnion, false)
	if err != nil {
		t.Fatalf("条目失败不应返回顶层 error: %v", err)
	}
	if !rep.HasFailures() || len(rep.Failed) != 1 || rep.Failed[0].Item != "a/1" {
		t.Fatalf("应记录 a/1 失败: %+v", rep.Failed)
	}
	if len(rep.Added) != 1 || rep.Added[0] != "a/2" { // a/2 仍成功
		t.Fatalf("a/2 应成功新增: %v", rep.Added)
	}
}
