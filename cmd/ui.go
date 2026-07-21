package cmd

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"runtime"

	"github.com/mturley/agent-handler/cmd/api"
	"github.com/mturley/agent-handler/terminal"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Start the web UI server",
	RunE:  runUI,
}

var (
	uiPort int
	uiDev  bool
)

func init() {
	uiCmd.GroupID = "human"
	rootCmd.AddCommand(uiCmd)
	uiCmd.Flags().IntVar(&uiPort, "port", 8420, "HTTP server port")
	uiCmd.Flags().BoolVar(&uiDev, "dev", false, "development mode (skip static file serving)")
}

func runUI(cmd *cobra.Command, args []string) error {
	// Check if web assets are built
	var webFS fs.FS
	if !uiDev {
		var err error
		webFS, err = fs.Sub(globalWebFS, "web/dist")
		if err != nil {
			fmt.Println("Web UI not built. Run 'make build-web' first.")
			return nil
		}
		// Check if dist has any real files
		entries, _ := fs.ReadDir(globalWebFS, "web/dist")
		hasContent := false
		for _, e := range entries {
			if e.Name() != ".gitkeep" {
				hasContent = true
				break
			}
		}
		if !hasContent {
			fmt.Println("Web UI not built. Run 'make build-web' first.")
			return nil
		}
	}

	// Detect cmux
	backendType, _, _ := terminal.Detect()
	cmuxAvailable := backendType == "cmux"

	if !cmuxAvailable && !uiDev {
		fmt.Println("cmux not detected. Session switching and other cmux features will not be available.")
		fmt.Print("Continue without cmux features? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Open DB
	d, err := openReadOnlyDB()
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer d.Close()

	logger := log.New(os.Stderr, "[handler-ui] ", log.LstdFlags)

	server := &api.Server{
		DB:            d,
		CmuxAvailable: cmuxAvailable,
		DevMode:       uiDev,
		WebFS:         webFS,
		Port:          uiPort,
		Logger:        logger,
	}

	// Open browser (in dev mode, open the Vite dev server port)
	if uiDev {
		url := "http://localhost:5173"
		logger.Printf("Dev mode: Vite dev server at %s", url)
		openBrowser(url)
	} else {
		url := fmt.Sprintf("http://localhost:%d", uiPort)
		logger.Printf("Opening %s in browser...", url)
		openBrowser(url)
	}

	return server.Start()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	cmd.Start()
}
