package sync

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSyncer 是内存实现，用于不触网地测试引擎逻辑。
type fakeSyncer struct {
	data   map[string]Set // user -> 已有条目
	addErr map[Item]error // 指定条目 Add 时返回错误
	adds   []string       // 记录 Add 调用 "user:item"
	rms    []string       // 记录 Remove 调用 "user:item"
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
func (f *fakeSyncer) Add(_ context.Context, _, a Account, it Item) error {
	if e := f.addErr[it]; e != nil {
		return e
	}
	f.set(a.User).Add(it)
	f.adds = append(f.adds, a.User+":"+string(it))
	return nil
}
func (f *fakeSyncer) Remove(_ context.Context, _, a Account, it Item) error {
	delete(f.set(a.User), it)
	f.rms = append(f.rms, a.User+":"+string(it))
	return nil
}

func TestSync_UnionAddsMissingAndSkipsExisting(t *testing.T) {
	f := newFake()
	f.data["src"] = Set{"a/1": {}, "a/2": {}, "a/3": {}}
	f.data["dst"] = Set{"a/2": {}} // 已有 a/2

	rep, err := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeUnion, false, 1)
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

	rep, _ := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeUnion, true, 1)
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

	rep, _ := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeMirror, false, 1)
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

	rep, err := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeUnion, false, 1)
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

func TestSync_CountsAggregation(t *testing.T) {
	f := newFake()
	f.data["src"] = Set{"a/1": {}, "a/2": {}}
	f.data["dst"] = Set{"a/1": {}, "a/9": {}} // a/1 已有（skipped），a/9 多余，a/2 需新增
	f.addErr = map[Item]error{}

	rep, err := Sync(context.Background(), Account{"src"}, Account{"dst"}, f, ModeMirror, false, 1)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	// a/2 新增，a/9 删除，无失败
	if rep.Counts.Added != 1 {
		t.Fatalf("Counts.Added 应为 1，实际 %d", rep.Counts.Added)
	}
	if rep.Counts.Removed != 1 {
		t.Fatalf("Counts.Removed 应为 1，实际 %d", rep.Counts.Removed)
	}
	if rep.Counts.Failed != 0 {
		t.Fatalf("Counts.Failed 应为 0，实际 %d", rep.Counts.Failed)
	}
}

// slowSyncer 是并发路径专用的 fakeSyncer：Add/Remove 会 sleep 一段固定时间，
// 用于验证并发执行确实让 wall-clock 时间缩短。
// 内部用 mutex + atomic 保护共享状态，可安全用于 -race。
type slowSyncer struct {
	mu     sync.Mutex
	data   map[string]Set
	active atomic.Int32 // 同一时刻正在执行 Add/Remove 的 goroutine 数（峰值）
	peak   atomic.Int32
	delay  time.Duration
	addErr map[Item]error
}

func newSlowSyncer(delay time.Duration) *slowSyncer {
	return &slowSyncer{data: map[string]Set{}, delay: delay}
}

// seed 预填某账号下的初始集合（线程安全）。
func (s *slowSyncer) seed(user string, items ...Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data[user] == nil {
		s.data[user] = Set{}
	}
	for _, it := range items {
		s.data[user][it] = struct{}{}
	}
}

func (s *slowSyncer) Name() string { return "slow" }
func (s *slowSyncer) snapshot(u string) Set {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Set{}
	for k := range s.data[u] {
		out[k] = struct{}{}
	}
	return out
}
func (s *slowSyncer) List(_ context.Context, a Account) (Set, error) { return s.snapshot(a.User), nil }
func (s *slowSyncer) Add(_ context.Context, _, a Account, it Item) error {
	cur := s.active.Add(1)
	defer s.active.Add(-1)
	for {
		p := s.peak.Load()
		if cur <= p || s.peak.CompareAndSwap(p, cur) {
			break
		}
	}
	time.Sleep(s.delay)
	if e := s.addErr[it]; e != nil {
		return e
	}
	s.mu.Lock()
	if s.data[a.User] == nil {
		s.data[a.User] = Set{}
	}
	s.data[a.User][it] = struct{}{}
	s.mu.Unlock()
	return nil
}
func (s *slowSyncer) Remove(_ context.Context, _, a Account, it Item) error {
	cur := s.active.Add(1)
	defer s.active.Add(-1)
	for {
		p := s.peak.Load()
		if cur <= p || s.peak.CompareAndSwap(p, cur) {
			break
		}
	}
	time.Sleep(s.delay)
	s.mu.Lock()
	delete(s.data[a.User], it)
	s.mu.Unlock()
	return nil
}

// TestSync_ConcurrentOverlap 验证 concurrency>1 时多个 Add 真的并发执行。
// 8 个 item × 50ms / concurrency=4 ≈ 100ms（串行会 ≈ 400ms）。
func TestSync_ConcurrentOverlap(t *testing.T) {
	const (
		nItems = 8
		delay  = 50 * time.Millisecond
		conc   = 4
	)
	syncer := newSlowSyncer(delay)
	for i := 0; i < nItems; i++ {
		syncer.seed("src", Item(string(rune('a'+i))))
	}
	start := time.Now()
	rep, err := Sync(context.Background(), Account{"src"}, Account{"dst"}, syncer, ModeUnion, false, conc)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	if rep.Counts.Added != nItems {
		t.Fatalf("应新增 %d 项, 实际 %d", nItems, rep.Counts.Added)
	}
	// 串行预期 ~ nItems*delay = 400ms；并发 4 预期 ~ 2*delay = 100ms。
	// 设个宽限阈值（150ms），CI 偶发慢一点不至于 flake，但仍能捕捉到完全没并发的 bug。
	if elapsed > 3*delay {
		t.Fatalf("并发执行未生效: %d items × %v / conc=%d 应 ≤ %v，实际 %v",
			nItems, delay, conc, 3*delay, elapsed)
	}
	if syncer.peak.Load() < 2 {
		t.Fatalf("并发执行未生效: 峰值活跃 goroutine 仅 %d", syncer.peak.Load())
	}
}

// TestSync_ConcurrentPreservesOrder 验证并发执行下 Report.Added 顺序与
// sorted items 一致（与串行路径输出一致）。
func TestSync_ConcurrentPreservesOrder(t *testing.T) {
	syncer := newSlowSyncer(10 * time.Millisecond)
	syncer.seed("src", "c/1", "a/1", "b/1", "d/1", "e/1")
	rep, err := Sync(context.Background(), Account{"src"}, Account{"dst"}, syncer, ModeUnion, false, 5)
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	want := []Item{"a/1", "b/1", "c/1", "d/1", "e/1"}
	if len(rep.Added) != len(want) {
		t.Fatalf("Added 数量错误: %v", rep.Added)
	}
	for i, it := range want {
		if rep.Added[i] != it {
			t.Fatalf("Added[%d] 应为 %q, 实际 %q (full: %v)", i, it, rep.Added[i], rep.Added)
		}
	}
}

// TestSync_ConcurrentItemFailureRecorded 验证并发执行下条目失败仍记入 Failed。
func TestSync_ConcurrentItemFailureRecorded(t *testing.T) {
	syncer := newSlowSyncer(5 * time.Millisecond)
	syncer.seed("src", "a/1", "a/2", "a/3")
	syncer.addErr = map[Item]error{"a/2": errors.New("boom")}
	rep, err := Sync(context.Background(), Account{"src"}, Account{"dst"}, syncer, ModeUnion, false, 3)
	if err != nil {
		t.Fatalf("条目失败不应返回顶层 error: %v", err)
	}
	if rep.Counts.Added != 2 {
		t.Fatalf("应新增 2 项 (a/1, a/3), 实际 %d", rep.Counts.Added)
	}
	if rep.Counts.Failed != 1 || rep.Failed[0].Item != "a/2" {
		t.Fatalf("应记录 a/2 失败: %+v", rep.Failed)
	}
}
