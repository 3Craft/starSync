package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/3Craft/starSync/internal/gh"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "检查 gh 登录状态与账号可用性",
		RunE: func(_ *cobra.Command, _ []string) error {
			out, err := gh.New().Status()
			fmt.Fprint(os.Stderr, out) // 诊断信息走 stderr
			if err != nil {
				return &exitError{code: 1}
			}
			return nil
		},
	}
}
