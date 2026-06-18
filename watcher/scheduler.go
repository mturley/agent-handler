package watcher

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	"github.com/mturley/agent-handler/db"
)

const launchdPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.agent-handler.watcher-{{.Name}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>{{.HandlerPath}}</string>
		<string>watcher</string>
		<string>run</string>
		<string>{{.Name}}</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>StartInterval</key>
	<integer>{{.IntervalSeconds}}</integer>
	<key>StandardOutPath</key>
	<string>{{.LogPath}}</string>
	<key>StandardErrorPath</key>
	<string>{{.LogPath}}</string>
</dict>
</plist>
`

type plistData struct {
	Name            string
	HandlerPath     string
	IntervalSeconds int
	LogPath         string
}

// Install installs a scheduled watcher on the current platform.
// On macOS, creates a launchd plist. On Linux, adds a cron entry.
func Install(name string, intervalSeconds int) error {
	if runtime.GOOS == "darwin" {
		return installLaunchd(name, intervalSeconds)
	}
	return installCron(name, intervalSeconds)
}

// Uninstall removes the scheduled watcher from the current platform.
func Uninstall(name string) error {
	if runtime.GOOS == "darwin" {
		return uninstallLaunchd(name)
	}
	return uninstallCron(name)
}

// Stop pauses a watcher without removing it. The plist/cron entry remains but is unloaded.
func Stop(name string) error {
	if !IsInstalled(name) {
		return fmt.Errorf("watcher %q is not installed", name)
	}
	if runtime.GOOS == "darwin" {
		plistPath, err := launchdPlistPath(name)
		if err != nil {
			return err
		}
		exec.Command("launchctl", "unload", plistPath).Run()
		return nil
	}
	return stopCron(name)
}

// Start resumes a stopped watcher.
func Start(name string) error {
	if !IsInstalled(name) {
		return fmt.Errorf("watcher %q is not installed", name)
	}
	if runtime.GOOS == "darwin" {
		plistPath, err := launchdPlistPath(name)
		if err != nil {
			return err
		}
		return exec.Command("launchctl", "load", plistPath).Run()
	}
	return startCron(name)
}

// IsRunning checks if the watcher is actively scheduled (installed and not stopped).
func IsRunning(name string) bool {
	if !IsInstalled(name) {
		return false
	}
	if runtime.GOOS == "darwin" {
		label := fmt.Sprintf("com.agent-handler.watcher-%s", name)
		output, err := exec.Command("launchctl", "list", label).CombinedOutput()
		return err == nil && len(output) > 0
	}
	return isRunningCron(name)
}

// IsInstalled checks if the watcher is installed on the current platform.
func IsInstalled(name string) bool {
	if runtime.GOOS == "darwin" {
		return isInstalledLaunchd(name)
	}
	return isInstalledCron(name)
}

// InstalledInterval returns the configured polling interval in seconds for an installed watcher.
// Returns 0 if the watcher is not installed or the interval can't be determined.
func InstalledInterval(name string) int {
	if runtime.GOOS == "darwin" {
		plistPath, err := launchdPlistPath(name)
		if err != nil {
			return 0
		}
		data, err := os.ReadFile(plistPath)
		if err != nil {
			return 0
		}
		content := string(data)
		// Parse <key>StartInterval</key>\n\t<integer>N</integer>
		idx := strings.Index(content, "<key>StartInterval</key>")
		if idx < 0 {
			return 0
		}
		rest := content[idx:]
		start := strings.Index(rest, "<integer>")
		end := strings.Index(rest, "</integer>")
		if start < 0 || end < 0 {
			return 0
		}
		var interval int
		fmt.Sscanf(rest[start+len("<integer>"):end], "%d", &interval)
		return interval
	}
	// For cron, parse the schedule — but cron intervals are in minutes
	// and harder to parse generically. Return 0 for now.
	return 0
}

// LastRunTime returns the last run time of the watcher by checking the log file modification time.
// Returns nil if the log file does not exist.
func LastRunTime(name string) *time.Time {
	logPath := filepath.Join(db.HandlerHome(), "data", "logs", fmt.Sprintf("watcher-%s.log", name))
	info, err := os.Stat(logPath)
	if err != nil {
		return nil
	}
	modTime := info.ModTime()
	return &modTime
}

// installLaunchd creates a launchd plist for the watcher.
func installLaunchd(name string, intervalSeconds int) error {
	// Find handler binary path
	handlerPath, err := exec.LookPath("handler")
	if err != nil {
		return fmt.Errorf("handler binary not found in PATH: %w", err)
	}

	// Ensure log directory exists
	logDir := filepath.Join(db.HandlerHome(), "data", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, fmt.Sprintf("watcher-%s.log", name))

	// Parse template
	tmpl, err := template.New("plist").Parse(launchdPlistTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse plist template: %w", err)
	}

	// Render template
	var buf bytes.Buffer
	data := plistData{
		Name:            name,
		HandlerPath:     handlerPath,
		IntervalSeconds: intervalSeconds,
		LogPath:         logPath,
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute plist template: %w", err)
	}

	// Get plist path
	plistPath, err := launchdPlistPath(name)
	if err != nil {
		return err
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	// Write plist
	if err := os.WriteFile(plistPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write plist: %w", err)
	}

	// Load with launchctl
	cmd := exec.Command("launchctl", "load", plistPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to load plist: %w (output: %s)", err, output)
	}

	return nil
}

// uninstallLaunchd removes the launchd plist for the watcher.
func uninstallLaunchd(name string) error {
	plistPath, err := launchdPlistPath(name)
	if err != nil {
		return err
	}

	// Check if plist exists
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return fmt.Errorf("watcher %q is not installed", name)
	}

	// Unload with launchctl
	cmd := exec.Command("launchctl", "unload", plistPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Don't fail if unload errors - the job might not be loaded
		_ = output
	}

	// Remove plist file
	if err := os.Remove(plistPath); err != nil {
		return fmt.Errorf("failed to remove plist: %w", err)
	}

	return nil
}

// isInstalledLaunchd checks if the launchd plist exists.
func isInstalledLaunchd(name string) bool {
	plistPath, err := launchdPlistPath(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(plistPath)
	return err == nil
}

// launchdPlistPath returns the path to the launchd plist for the given watcher name.
func launchdPlistPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("com.agent-handler.watcher-%s.plist", name)), nil
}

// installCron adds a cron entry for the watcher.
func installCron(name string, intervalSeconds int) error {
	// Find handler binary path
	handlerPath, err := exec.LookPath("handler")
	if err != nil {
		return fmt.Errorf("handler binary not found in PATH: %w", err)
	}

	// Ensure log directory exists
	logDir := filepath.Join(db.HandlerHome(), "data", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, fmt.Sprintf("watcher-%s.log", name))

	// Convert interval to cron schedule (minimum 1 minute)
	intervalMinutes := intervalSeconds / 60
	if intervalMinutes < 1 {
		intervalMinutes = 1
	}

	cronSchedule := fmt.Sprintf("*/%d * * * *", intervalMinutes)

	// Read existing crontab
	cmd := exec.Command("crontab", "-l")
	output, err := cmd.CombinedOutput()
	existingCrontab := ""
	if err == nil {
		existingCrontab = string(output)
	}

	// Filter out existing entry for this watcher
	lines := strings.Split(existingCrontab, "\n")
	commentMarker := fmt.Sprintf("# agent-handler-%s", name)
	var filtered []string
	skipNext := false
	for _, line := range lines {
		if strings.TrimSpace(line) == commentMarker {
			skipNext = true
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if strings.TrimSpace(line) != "" {
			filtered = append(filtered, line)
		}
	}

	// Add new entry
	newEntry := fmt.Sprintf("%s\n%s %s watcher run %s >> %s 2>&1", commentMarker, cronSchedule, handlerPath, name, logPath)
	filtered = append(filtered, newEntry)

	// Write new crontab
	newCrontab := strings.Join(filtered, "\n") + "\n"
	cmd = exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to install crontab: %w (output: %s)", err, output)
	}

	return nil
}

// uninstallCron removes the cron entry for the watcher.
func uninstallCron(name string) error {
	// Read existing crontab
	cmd := exec.Command("crontab", "-l")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to read crontab: %w", err)
	}

	// Filter out entry for this watcher
	existingCrontab := string(output)
	lines := strings.Split(existingCrontab, "\n")
	commentMarker := fmt.Sprintf("# agent-handler-%s", name)
	var filtered []string
	skipNext := false
	found := false
	for _, line := range lines {
		if strings.TrimSpace(line) == commentMarker {
			skipNext = true
			found = true
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if strings.TrimSpace(line) != "" {
			filtered = append(filtered, line)
		}
	}

	if !found {
		return fmt.Errorf("watcher %q is not installed", name)
	}

	// Write new crontab
	newCrontab := strings.Join(filtered, "\n")
	if strings.TrimSpace(newCrontab) != "" {
		newCrontab += "\n"
	}
	cmd = exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to update crontab: %w (output: %s)", err, output)
	}

	return nil
}

// isInstalledCron checks if a cron entry exists for the watcher.
func isInstalledCron(name string) bool {
	cmd := exec.Command("crontab", "-l")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	commentMarker := fmt.Sprintf("# agent-handler-%s", name)
	return strings.Contains(string(output), commentMarker)
}

func stopCron(name string) error {
	cmd := exec.Command("crontab", "-l")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to read crontab: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	commentMarker := fmt.Sprintf("# agent-handler-%s", name)
	stoppedMarker := fmt.Sprintf("# agent-handler-%s-stopped", name)
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) == commentMarker {
			result = append(result, stoppedMarker)
			continue
		}
		result = append(result, line)
	}

	newCrontab := strings.Join(result, "\n")
	cmd = exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	_, err = cmd.CombinedOutput()
	return err
}

func startCron(name string) error {
	cmd := exec.Command("crontab", "-l")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to read crontab: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	stoppedMarker := fmt.Sprintf("# agent-handler-%s-stopped", name)
	commentMarker := fmt.Sprintf("# agent-handler-%s", name)
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) == stoppedMarker {
			result = append(result, commentMarker)
			continue
		}
		result = append(result, line)
	}

	newCrontab := strings.Join(result, "\n")
	cmd = exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newCrontab)
	_, err = cmd.CombinedOutput()
	return err
}

func isRunningCron(name string) bool {
	cmd := exec.Command("crontab", "-l")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	commentMarker := fmt.Sprintf("# agent-handler-%s", name)
	stoppedMarker := fmt.Sprintf("# agent-handler-%s-stopped", name)
	content := string(output)
	return strings.Contains(content, commentMarker) && !strings.Contains(content, stoppedMarker)
}
