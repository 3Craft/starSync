package sync

import (
	"context"
	"sort"
)

// Sync 执行单向同步：把 src 的资源补齐到 dst。
// union 只增不删；mirror 额外删除 dst 中 src 没有的条目。
// dryRun 为 true 时不执行任何写操作，只计算将要发生的变更。
// 条目级失败记入 Report.Failed 且不中断；仅 List 等运行级错误返回非 nil error。
func Sync(ctx context.Context, src, dst Account, s Syncer, mode Mode, dryRun bool) (Report, error) {
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
	for _, it := range toAdd {
		if dryRun {
			rep.Added = append(rep.Added, it)
			continue
		}
		if err := s.Add(ctx, dst, it); err != nil {
			rep.Failed = append(rep.Failed, Failure{Item: it, Error: err.Error()})
			continue
		}
		rep.Added = append(rep.Added, it)
	}

	if mode == ModeMirror {
		var toRemove []Item
		for it := range dstSet {
			if !srcSet.Has(it) {
				toRemove = append(toRemove, it)
			}
		}
		sortItems(toRemove)
		for _, it := range toRemove {
			if dryRun {
				rep.Removed = append(rep.Removed, it)
				continue
			}
			if err := s.Remove(ctx, dst, it); err != nil {
				rep.Failed = append(rep.Failed, Failure{Item: it, Error: err.Error()})
				continue
			}
			rep.Removed = append(rep.Removed, it)
		}
	}

	return rep, nil
}

func sortItems(xs []Item) {
	sort.Slice(xs, func(i, j int) bool { return xs[i] < xs[j] })
}
