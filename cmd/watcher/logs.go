package watcher

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mturley/agent-handler/db"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "View watcher logs",
	Long: `View the most recent log entries for a watcher.
With no arguments, shows combined logs from all watchers with prefixes.
With a name, shows logs for that specific watcher.`,
	Args: cobra.MaximumNArgs(1),
	RunE: showLogs,
}

func init() {
	logsCmd.Flags().Int("lines", 50, "number of lines to show")
	logsCmd.Flags().Bool("tail", false, "follow logs in real-time")
	WatcherCmd.AddCommand(logsCmd)
}

type logLine struct {
	source string
	text   string
}

func showLogs(cmd *cobra.Command, args []string) error {
	lines, _ := cmd.Flags().GetInt("lines")
	tail, _ := cmd.Flags().GetBool("tail")

	if tail {
		return tailLogs(args)
	}

	if len(args) == 1 {
		return showSingleLog(args[0], lines, false)
	}
	return showCombinedLogs(lines)
}

func showSingleLog(name string, numLines int, prefix bool) error {
	logPath := filepath.Join(db.HandlerHome(), "data", "logs", fmt.Sprintf("watcher-%s.log", name))

	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("No logs found for %s watcher\n", name)
			return nil
		}
		return fmt.Errorf("reading log: %w", err)
	}

	fileLines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	start := len(fileLines) - numLines
	if start < 0 {
		start = 0
	}

	for _, line := range fileLines[start:] {
		if prefix {
			fmt.Printf("[%s] %s\n", name, line)
		} else {
			fmt.Println(line)
		}
	}
	return nil
}

func showCombinedLogs(numLines int) error {
	var all []logLine

	for _, name := range knownWatchers {
		logPath := filepath.Join(db.HandlerHome(), "data", "logs", fmt.Sprintf("watcher-%s.log", name))
		data, err := os.ReadFile(logPath)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if line != "" {
				all = append(all, logLine{source: name, text: line})
			}
		}
	}

	if len(all) == 0 {
		fmt.Println("No watcher logs found")
		return nil
	}

	// Sort by timestamp (log lines start with date from Go's log package)
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].text < all[j].text
	})

	start := len(all) - numLines
	if start < 0 {
		start = 0
	}

	for _, l := range all[start:] {
		fmt.Printf("[%s] %s\n", l.source, l.text)
	}
	return nil
}

func tailLogs(args []string) error {
	targets := knownWatchers
	if len(args) == 1 {
		targets = []string{args[0]}
	}
	prefix := len(targets) > 1

	type watchedFile struct {
		name   string
		path   string
		offset int64
	}

	var files []watchedFile
	for _, name := range targets {
		logPath := filepath.Join(db.HandlerHome(), "data", "logs", fmt.Sprintf("watcher-%s.log", name))
		info, err := os.Stat(logPath)
		offset := int64(0)
		if err == nil {
			offset = info.Size()
		}
		files = append(files, watchedFile{name: name, path: logPath, offset: offset})
	}

	fmt.Println("Tailing watcher logs... (Ctrl+C to stop)")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	for {
		select {
		case <-sig:
			return nil
		default:
		}

		for i := range files {
			f, err := os.Open(files[i].path)
			if err != nil {
				continue
			}

			info, err := f.Stat()
			if err != nil {
				f.Close()
				continue
			}

			if info.Size() > files[i].offset {
				f.Seek(files[i].offset, io.SeekStart)
				data, err := io.ReadAll(f)
				if err == nil && len(data) > 0 {
					for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
						if line != "" {
							if prefix {
								fmt.Printf("[%s] %s\n", files[i].name, line)
							} else {
								fmt.Println(line)
							}
						}
					}
				}
				files[i].offset = info.Size()
			}

			f.Close()
		}

		time.Sleep(1 * time.Second)
	}
}
