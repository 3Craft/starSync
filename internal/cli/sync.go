package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/3Craft/starSync/internal/config"
	"github.com/3Craft/starSync/internal/following"
	"github.com/3Craft/starSync/internal/gh"
	"github.com/3Craft/starSync/internal/gists"
	"github.com/3Craft/starSync/internal/stars"
	syncpkg "github.com/3Craft/starSync/internal/sync"
)

// pair 是展开后的单向同步任务。
type pair struct {
	resource string
	from     string
	to       string
	mirror   bool
}

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sync", Short: "同步资源"}
	cmd.AddCommand(newSyncStarsCmd())
	cmd.AddCommand(newSyncGistsCmd())
	cmd.AddCommand(newSyncFollowingCmd())
	return cmd
}

// syncFlags 是各 sync subcommand 共用的 flag 定义。
type syncFlags struct {
	from    string
	to      []string
	cfgPath string
	mirror  bool
	dryRun  bool
	asJSON      bool
	yes         bool
	concurrency int
}

// bindSyncFlags 把共用 flag 绑定到 cmd 上，返回指针便于闭包内读。
func bindSyncFlags(cmd *cobra.Command, f *syncFlags) {
	fl := cmd.Flags()
	fl.StringVar(&f.from, "from", "", "源账号")
	fl.StringArrayVar(&f.to, "to", nil, "目标账号（可重复）")
	fl.StringVar(&f.cfgPath, "config", "", "多对多配置文件路径")
	fl.BoolVar(&f.mirror, "mirror", false, "镜像模式（含 unstar/unfollow，破坏性）")
	fl.BoolVar(&f.dryRun, "dry-run", false, "只预演不写入")
	fl.BoolVar(&f.asJSON, "json", false, "输出 JSON Report")
	fl.BoolVar(&f.yes, "yes", false, "跳过交互确认")
	fl.IntVar(&f.concurrency, "concurrency", 1, "写操作并发数（默认 1=串行；建议 ≤ 8 避免 secondary rate limit）")
}

// buildConfirm 返回 mirror 二次确认函数（共享 stdin reader 避免预读丢弃）。
func buildConfirm() func(from, to string, removed []syncpkg.Item) bool {
	stdinR := bufio.NewReader(os.Stdin)
	return func(from, to string, removed []syncpkg.Item) bool {
		return defaultConfirm(stdinR, from, to, removed)
	}
}

func newSyncStarsCmd() *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "stars",
		Short: "同步 starred 仓库",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pairs, err := resolvePairs(f.from, f.to, f.cfgPath, f.mirror, "stars")
			if err != nil {
				fmt.Fprintln(os.Stderr, "错误:", err)
				return &exitError{code: 1}
			}
			ghc := gh.New()
			syncers := map[string]syncpkg.Syncer{
				"stars": stars.New(ghc.TokenFor),
			}
			return runPairs(cmd.Context(), syncers, pairs, f.dryRun, f.asJSON, f.yes, f.concurrency, buildConfirm())
		},
	}
	bindSyncFlags(cmd, &f)
	return cmd
}

func newSyncGistsCmd() *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "gists",
		Short: "同步 gist（按 description 匹配）",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pairs, err := resolvePairs(f.from, f.to, f.cfgPath, f.mirror, "gists")
			if err != nil {
				fmt.Fprintln(os.Stderr, "错误:", err)
				return &exitError{code: 1}
			}
			ghc := gh.New()
			syncers := map[string]syncpkg.Syncer{
				"gists": gists.New(ghc.TokenFor),
			}
			return runPairs(cmd.Context(), syncers, pairs, f.dryRun, f.asJSON, f.yes, f.concurrency, buildConfirm())
		},
	}
	bindSyncFlags(cmd, &f)
	return cmd
}

func newSyncFollowingCmd() *cobra.Command {
	var f syncFlags
	cmd := &cobra.Command{
		Use:   "following",
		Short: "同步关注列表",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pairs, err := resolvePairs(f.from, f.to, f.cfgPath, f.mirror, "following")
			if err != nil {
				fmt.Fprintln(os.Stderr, "错误:", err)
				return &exitError{code: 1}
			}
			ghc := gh.New()
			syncers := map[string]syncpkg.Syncer{
				"following": following.New(ghc.TokenFor),
			}
			return runPairs(cmd.Context(), syncers, pairs, f.dryRun, f.asJSON, f.yes, f.concurrency, buildConfirm())
		},
	}
	bindSyncFlags(cmd, &f)
	return cmd
}

// resolvePairs 把 flag / 配置展开为目标 resource 的单向 pair 列表。
// 配置文件里其它 resource 的条目会被过滤掉。
func resolvePairs(from string, to []string, cfgPath string, mirror bool, targetResource string) ([]pair, error) {
	if cfgPath != "" {
		if from != "" || len(to) > 0 {
			return nil, fmt.Errorf("--config 不能与 --from/--to 同时使用")
		}
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return nil, err
		}
		var ps []pair
		for _, p := range cfg.Syncs {
			if p.Resource != targetResource {
				continue
			}
			for _, t := range p.To {
				ps = append(ps, pair{resource: targetResource, from: p.From, to: t, mirror: p.Mirror})
			}
		}
		if len(ps) == 0 {
			return nil, fmt.Errorf("配置 %q 中没有 resource=%q 的条目", cfgPath, targetResource)
		}
		return ps, nil
	}
	if from == "" || len(to) == 0 {
		return nil, fmt.Errorf("必须提供 --from 与至少一个 --to（或使用 --config）")
	}
	var ps []pair
	for _, t := range to {
		ps = append(ps, pair{resource: targetResource, from: from, to: t, mirror: mirror})
	}
	return ps, nil
}

// runPairs 串行执行所有 pair，按结果决定退出码。
// syncers 按 resource 名索引；pair 根据自身的 resource 字段选 syncer。
// confirm 仅在 mirror 且 !dryRun 且 !yes 且存在待删条目时调用；返回 false 表示跳过该 pair。
// concurrency 透传给每个 syncpkg.Sync 调用（控制单 pair 内的写并发）。
func runPairs(ctx context.Context, syncers map[string]syncpkg.Syncer, pairs []pair, dryRun, asJSON, yes bool, concurrency int, confirm func(from, to string, removed []syncpkg.Item) bool) error {
	// #5：确保空结果序列化为 [] 而非 null。
	reports := []syncpkg.Report{}
	var runErr, itemFail bool

	for _, p := range pairs {
		syncer, ok := syncers[p.resource]
		if !ok {
			fmt.Fprintf(os.Stderr, "未知 resource %q\n", p.resource)
			runErr = true
			continue
		}
		mode := syncpkg.ModeUnion
		if p.mirror {
			mode = syncpkg.ModeMirror
			// 确认逻辑下沉到差集计算之后（破坏性操作知情同意）。
			// 预演用串行（confirm 是用户交互，不并发），避免重复 concurrency 复杂度。
			if !dryRun && !yes {
				dryRep, err := syncpkg.Sync(ctx, syncpkg.Account{User: p.from}, syncpkg.Account{User: p.to}, syncer, mode, true, 1)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  预演失败: %v\n", err)
					runErr = true
					continue
				}
				if len(dryRep.Removed) > 0 && !confirm(p.from, p.to, dryRep.Removed) {
					fmt.Fprintf(os.Stderr, "跳过 %s → %s（用户取消镜像）\n", p.from, p.to)
					continue
				}
			}
		}
		fmt.Fprintf(os.Stderr, "同步 %s: %s → %s（%s, 并发 %d）\n", syncer.Name(), p.from, p.to, mode, concurrency)
		rep, err := syncpkg.Sync(ctx, syncpkg.Account{User: p.from}, syncpkg.Account{User: p.to}, syncer, mode, dryRun, concurrency)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  失败: %v\n", err)
			runErr = true
			continue
		}
		if rep.HasFailures() {
			itemFail = true
		}
		reports = append(reports, rep)
		if !asJSON {
			printHuman(rep)
		}
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(reports); err != nil {
			fmt.Fprintln(os.Stderr, "JSON 编码失败:", err)
			return &exitError{code: 1}
		}
	}

	switch {
	case runErr:
		return &exitError{code: 1}
	case itemFail:
		return &exitError{code: 2}
	}
	return nil
}

// defaultConfirm 向 stderr 打印待删清单，从 r 读取确认。
func defaultConfirm(r *bufio.Reader, from, to string, removed []syncpkg.Item) bool {
	fmt.Fprintf(os.Stderr, "⚠️ 镜像将取消 %s 上的 %d 个 star：\n", to, len(removed))
	for _, item := range removed {
		fmt.Fprintf(os.Stderr, "  - %s\n", item)
	}
	fmt.Fprintf(os.Stderr, "输入 yes 确认: ")
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line) == "yes"
}

// printHuman 把 Report 摘要打到 stdout（结果数据）。
func printHuman(r syncpkg.Report) {
	mark := ""
	if r.DryRun {
		mark = "[dry-run] "
	}
	fmt.Printf("%s%s → %s: 源 %d, 目标原有 %d, 新增 %d, 跳过 %d, 删除 %d, 失败 %d\n",
		mark, r.From, r.To, r.SourceCount, r.TargetBefore, len(r.Added), r.Skipped, len(r.Removed), len(r.Failed))
	for _, f := range r.Failed {
		fmt.Printf("  失败: %s (%s)\n", f.Item, f.Error)
	}
}
