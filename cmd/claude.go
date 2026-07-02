package cmd

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"github.com/mturley/agent-handler/terminal"
	"github.com/spf13/cobra"
)

var claudeCmd = &cobra.Command{
	Use:                "claude [claude-args...]",
	Short:              "Start a peekable Claude session",
	Long:               "Wrapper that ensures the Claude session is peekable via handler peek, then passes all arguments through to claude.",
	DisableFlagParsing: true,
	RunE:               runClaude,
}

func init() {
	claudeCmd.GroupID = "human"
	rootCmd.AddCommand(claudeCmd)
}

func runClaude(cmd *cobra.Command, args []string) error {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found on PATH: %w", err)
	}

	backendType, _ := terminal.Detect()

	switch backendType {
	case "cmux":
		os.Setenv("HANDLER_MANAGED", "1")
		return syscall.Exec(claudeBin, append([]string{"claude"}, args...), os.Environ())

	case "tmux":
		// Set pane title to handler:pending
		exec.Command("tmux", "select-pane", "-T", "handler:pending").Run()
		os.Setenv("HANDLER_MANAGED", "1")
		return syscall.Exec(claudeBin, append([]string{"claude"}, args...), os.Environ())

	default:
		// Outside both — prompt user
		fmt.Println("No tmux or cmux detected. Start a tmux session for peek support? [y/N]")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))

		if answer == "y" || answer == "yes" {
			suffix, _ := rand.Int(rand.Reader, big.NewInt(99999))
			sessionName := fmt.Sprintf("handler-%05d", suffix.Int64())
			// Pass claude binary and args separately to tmux
			tmuxArgs := []string{"new-session", "-s", sessionName, "-e", "HANDLER_MANAGED=1", claudeBin}
			tmuxArgs = append(tmuxArgs, args...)
			tmuxCmd := exec.Command("tmux", tmuxArgs...)
			tmuxCmd.Stdin = os.Stdin
			tmuxCmd.Stdout = os.Stdout
			tmuxCmd.Stderr = os.Stderr
			return tmuxCmd.Run()
		}

		// User declined — run without peek support
		return syscall.Exec(claudeBin, append([]string{"claude"}, args...), os.Environ())
	}
}
