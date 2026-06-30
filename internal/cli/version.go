package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version 可在构建时通过 -ldflags "-X .../cli.Version=x.y.z" 注入。
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "输出版本号",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Println(Version)
			return nil
		},
	}
}
