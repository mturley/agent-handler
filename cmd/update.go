package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update agent-handler to the latest version and re-run setup",
	RunE:  runUpdate,
}

func init() {
	updateCmd.GroupID = "admin"
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine binary location: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("could not resolve binary path: %w", err)
	}

	// Check if the binary is in a Go bin directory
	gobin := os.Getenv("GOBIN")
	if gobin == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			out, err := exec.Command("go", "env", "GOPATH").Output()
			if err == nil {
				gopath = strings.TrimSpace(string(out))
			}
		}
		if gopath != "" {
			gobin = filepath.Join(gopath, "bin")
		}
	}

	inGoBin := gobin != "" && strings.HasPrefix(realPath, gobin)

	if !inGoBin {
		fmt.Println("The handler binary was not installed via 'go install'.")
		fmt.Printf("  Binary location: %s\n", realPath)
		fmt.Println("")
		fmt.Println("To update a local install:")
		fmt.Println("  cd <agent-handler repo>")
		fmt.Println("  git pull")
		fmt.Println("  make build && make install")
		return nil
	}

	fmt.Println("Updating agent-handler...")
	fmt.Println("")

	// Run go install
	fmt.Println("  Running: go install github.com/mturley/agent-handler@latest")
	goInstall := exec.Command("go", "install", "github.com/mturley/agent-handler@latest")
	goInstall.Stdout = os.Stdout
	goInstall.Stderr = os.Stderr
	if err := goInstall.Run(); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}
	fmt.Println("  ✓ Binary updated")
	fmt.Println("")

	// Run handler setup with the new binary
	setup := exec.Command(filepath.Join(gobin, "handler"), "setup")
	setup.Stdout = os.Stdout
	setup.Stderr = os.Stderr
	setup.Stdin = os.Stdin
	return setup.Run()
}
