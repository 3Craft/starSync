package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version / Commit / Date 均可通过构建期 ldflags 注入（GoReleaser 默认行为）。
// 默认值 "dev" / "none" / "unknown" 用于本地 `go build` 无注入的场景。
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "输出版本号",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Printf("%s (commit %s, built %s)\n", Version, Commit, Date)
			return nil
		},
	}
}
