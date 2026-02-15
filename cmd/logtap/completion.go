package main

import (
	"os"

	"github.com/spf13/cobra"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for logtap.

To load completions:

Bash:
  $ source <(logtap completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ logtap completion bash > /etc/bash_completion.d/logtap
  # macOS:
  $ logtap completion bash > $(brew --prefix)/etc/bash_completion.d/logtap

Zsh:
  $ source <(logtap completion zsh)
  # To load completions for each session, execute once:
  $ logtap completion zsh > "${fpath[1]}/_logtap"

Fish:
  $ logtap completion fish | source
  # To load completions for each session, execute once:
  $ logtap completion fish > ~/.config/fish/completions/logtap.fish
`,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			}
			return nil
		},
	}

	return cmd
}
