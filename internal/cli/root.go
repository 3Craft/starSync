package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

// exitError 承载期望的进程退出码。
type exitError struct {
	code int
}

func (e *exitError) Error() string { return "" }

// Execute 构建并运行 CLI，返回进程退出码。
func Execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	root := &cobra.Command{
		Use:           "starsync",
		Short:         "在多个 GitHub 账号之间同步 star",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newSyncCmd(), newDoctorCmd(), newVersionCmd())

	if err := root.ExecuteContext(ctx); err != nil {
		if ec, ok := err.(*exitError); ok {
			return ec.code
		}
		fmt.Fprintln(os.Stderr, "错误:", err)
		return 1
	}
	return 0
}
