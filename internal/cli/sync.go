package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xsharp/starsync/internal/config"
	"github.com/xsharp/starsync/internal/gh"
	"github.com/xsharp/starsync/internal/stars"
	syncpkg "github.com/xsharp/starsync/internal/sync"
)

// pair 是展开后的单向同步任务。
type pair struct {
	from   string
	to     string
	mirror bool
}

func newSyncCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sync", Short: "同步资源"}
	cmd.AddCommand(newSyncStarsCmd())
	return cmd
}

func newSyncStarsCmd() *cobra.Command {
	var (
		from    string
		to      []string
		cfgPath string
		mirror  bool
		dryRun  bool
		asJSON  bool
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "stars",
		Short: "同步 starred 仓库",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pairs, err := resolvePairs(from, to, cfgPath, mirror)
			if err != nil {
				fmt.Fprintln(os.Stderr, "错误:", err)
				return &exitError{code: 1}
			}
			ghc := gh.New()
			syncer := stars.New(ghc.TokenFor)
			return runPairs(cmd.Context(), syncer, pairs, dryRun, asJSON, yes)
		},
	}
	f := cmd.Flags()
	f.StringVar(&from, "from", "", "源账号")
	f.StringArrayVar(&to, "to", nil, "目标账号（可重复）")
	f.StringVar(&cfgPath, "config", "", "多对多配置文件路径")
	f.BoolVar(&mirror, "mirror", false, "镜像模式（含 unstar，破坏性）")
	f.BoolVar(&dryRun, "dry-run", false, "只预演不写入")
	f.BoolVar(&asJSON, "json", false, "输出 JSON Report")
	f.BoolVar(&yes, "yes", false, "跳过交互确认")
	return cmd
}

// resolvePairs 把 flag / 配置展开为单向 pair 列表。
func resolvePairs(from string, to []string, cfgPath string, mirror bool) ([]pair, error) {
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
			for _, t := range p.To {
				ps = append(ps, pair{from: p.From, to: t, mirror: p.Mirror})
			}
		}
		return ps, nil
	}
	if from == "" || len(to) == 0 {
		return nil, fmt.Errorf("必须提供 --from 与至少一个 --to（或使用 --config）")
	}
	var ps []pair
	for _, t := range to {
		ps = append(ps, pair{from: from, to: t, mirror: mirror})
	}
	return ps, nil
}

// runPairs 串行执行所有 pair，按结果决定退出码。
func runPairs(ctx context.Context, syncer syncpkg.Syncer, pairs []pair, dryRun, asJSON, yes bool) error {
	var reports []syncpkg.Report
	var runErr, itemFail bool

	for _, p := range pairs {
		mode := syncpkg.ModeUnion
		if p.mirror {
			mode = syncpkg.ModeMirror
			if !dryRun && !yes && !confirmMirror(p.from, p.to) {
				fmt.Fprintf(os.Stderr, "跳过 %s → %s（用户取消镜像）\n", p.from, p.to)
				continue
			}
		}
		fmt.Fprintf(os.Stderr, "同步 stars: %s → %s（%s）\n", p.from, p.to, mode)
		rep, err := syncpkg.Sync(ctx, syncpkg.Account{User: p.from}, syncpkg.Account{User: p.to}, syncer, mode, dryRun)
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

// confirmMirror 在 stderr 提示并从 stdin 读取确认。
func confirmMirror(from, to string) bool {
	fmt.Fprintf(os.Stderr, "⚠️  镜像模式将取消 %s 上 %s 没有 star 的仓库。输入 yes 确认: ", to, from)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
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
