package sync

import (
	"context"
	"sort"
	"sync"
)

// Sync 执行单向同步：把 src 的资源补齐到 dst。
//
//   - union 只增不删；mirror 额外删除 dst 中 src 没有的条目。
//   - dryRun 为 true 时不执行任何写操作，只计算将要发生的变更。
//   - concurrency 控制写操作并发度；<= 1 时退化为串行（向后兼容）。
//   - 条目级失败记入 Report.Failed 且不中断；仅 List 等运行级错误返回非 nil error。
//
// 并发场景下，Report.Added/Removed/Failed 的顺序与 items 排序一致（确定输出）。
func Sync(ctx context.Context, src, dst Account, s Syncer, mode Mode, dryRun bool, concurrency int) (Report, error) {
	rep := Report{
		Resource: s.Name(),
		From:     src.User,
		To:       dst.User,
		Mode:     mode.String(),
		DryRun:   dryRun,
		Added:    []Item{},
		Removed:  []Item{},
		Failed:   []Failure{},
	}

	srcSet, err := s.List(ctx, src)
	if err != nil {
		return rep, err
	}
	dstSet, err := s.List(ctx, dst)
	if err != nil {
		return rep, err
	}
	rep.SourceCount = len(srcSet)
	rep.TargetBefore = len(dstSet)

	// 待添加 = src 有、dst 无；排序保证输出与测试确定性。
	var toAdd []Item
	for it := range srcSet {
		if dstSet.Has(it) {
			rep.Skipped++
		} else {
			toAdd = append(toAdd, it)
		}
	}
	sortItems(toAdd)
	applyWrites(ctx, s, src, dst, toAdd, dryRun, concurrency, &rep, true)

	if mode == ModeMirror {
		var toRemove []Item
		for it := range dstSet {
			if !srcSet.Has(it) {
				toRemove = append(toRemove, it)
			}
		}
		sortItems(toRemove)
		applyWrites(ctx, s, src, dst, toRemove, dryRun, concurrency, &rep, false)
	}

	rep.Counts = Counts{Added: len(rep.Added), Removed: len(rep.Removed), Failed: len(rep.Failed)}
	return rep, nil
}

// applyWrites 对 items 中的每个条目执行 Add (isAdd=true) 或 Remove (isAdd=false)。
// concurrency <= 1 走串行快路径；> 1 走 goroutine + semaphore 限流。
// 结果按 items 顺序填入 rep（保证输出稳定）。
func applyWrites(
	ctx context.Context,
	s Syncer,
	src, dst Account,
	items []Item,
	dryRun bool,
	concurrency int,
	rep *Report,
	isAdd bool,
) {
	if len(items) == 0 {
		return
	}

	// result 记录单条结果；err == nil 视为成功。
	// 用 mutex 保护 results map（Go 不允许并发写同一 map，即使 key 不同）。
	type result struct{ err error }
	var (
		mu      sync.Mutex
		results = make(map[Item]result, len(items))
	)

	run := func(it Item) {
		if dryRun {
			mu.Lock()
			results[it] = result{}
			mu.Unlock()
			return
		}
		var err error
		if isAdd {
			err = s.Add(ctx, src, dst, it)
		} else {
			err = s.Remove(ctx, src, dst, it)
		}
		mu.Lock()
		results[it] = result{err: err}
		mu.Unlock()
	}

	if concurrency <= 1 {
		for _, it := range items {
			run(it)
		}
	} else {
		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for _, it := range items {
			wg.Add(1)
			sem <- struct{}{}
			go func(it Item) {
				defer wg.Done()
				defer func() { <-sem }()
				run(it)
			}(it)
		}
		wg.Wait()
	}

	// 按 items 顺序填入 Report（与串行路径输出一致）。
	for _, it := range items {
		r := results[it]
		if r.err != nil {
			rep.Failed = append(rep.Failed, Failure{Item: it, Error: r.err.Error()})
			continue
		}
		if isAdd {
			rep.Added = append(rep.Added, it)
		} else {
			rep.Removed = append(rep.Removed, it)
		}
	}
}

func sortItems(xs []Item) {
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
}
