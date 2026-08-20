package watcher

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mturley/watcher/credsetup"
)

// authPrompter implements credsetup.Prompter over handler's existing
// terminal primitives: bufio stdin reads and plain fmt/✓/⚠ printing, the
// same style already used by the (now-removed) configureGitHub/configureJira
// helpers in auth.go. It mirrors the confirm() helper in cmd/uninstall.go
// (package cmd) — duplicated here rather than shared across packages, since
// cmd/watcher cannot import cmd (cmd already imports cmd/watcher), matching
// the precedent already set by writeWatcherBehaviorConfig in auth.go.
//
// Handler has no automated Slack credential extraction (no extract.mjs, per
// worktree's approach) — PromptSlack always falls back to manual devtools
// instructions.
type authPrompter struct {
	reader *bufio.Reader
}

func newAuthPrompter() *authPrompter {
	return &authPrompter{reader: bufio.NewReader(os.Stdin)}
}

// Info prints a status line from credsetup.TestAndRepair, marking success
// and failure outcomes with ✓/⚠ based on the exact wording credsetup emits
// today ("<Service>: ok" on success; "...failed..."/"...invalid..." on
// failure). Re-check this if the pinned credsetup version changes its
// messages.
func (p *authPrompter) Info(msg string) {
	switch {
	case strings.Contains(msg, ": ok"):
		fmt.Printf("✓ %s\n", msg)
	case strings.Contains(msg, "failed"), strings.Contains(msg, "invalid"):
		fmt.Printf("⚠ %s\n", msg)
	default:
		fmt.Println(msg)
	}
}

// Confirm asks a yes/no question, defaulting to "no" on empty input or a
// read error, matching the existing confirm() helper in cmd/uninstall.go.
func (p *authPrompter) Confirm(msg string) bool {
	fmt.Printf("%s [y/N] ", msg)
	response, err := p.reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// PromptToken prints the given instructions and reads a single secret line
// for the named service. An empty response (just Enter) means "skip".
func (p *authPrompter) PromptToken(service credsetup.Service, instructions string) string {
	fmt.Println(instructions)
	fmt.Printf("Enter %s token (or press Enter to skip): ", service)
	token, err := p.reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(token)
}

// slackTokenInstructions describes, step by step, how to extract a Slack
// browser session token (xoxc-...) and its matching cookie (the "d" cookie,
// xoxd-...) from a logged-in Slack web session using browser dev tools.
// Mirrors worktree's internal/setup/slack.go tokenInstructions text so the
// two consumers give operators identical guidance; handler has no automated
// extraction path, so this is the only way handler acquires Slack creds.
const slackTokenInstructions = `To use Slack features you need two values from a browser where you are
logged into Slack: an API token (starts with "xoxc-") and a session cookie
(the "d" cookie, which starts with "xoxd-").

  1. Open Slack in your browser at https://app.slack.com and log in.
  2. Open your browser's developer tools:
       - Chrome:  Cmd+Option+I (macOS) / Ctrl+Shift+I or F12 (Windows/Linux),
                  or menu ⋮ → More Tools → Developer Tools.
       - Firefox: Cmd+Option+I (macOS) / Ctrl+Shift+I or F12 (Windows/Linux),
                  or menu ≡ → More Tools → Web Developer Tools.
       - Safari:  first enable the Develop menu via Settings → Advanced →
                  "Show features for web developers", then press
                  Cmd+Option+I (or Develop menu → Show Web Inspector).

  To get the TOKEN (xoxc-...):
  3. Go to the Console tab and run:

       JSON.parse(localStorage.localConfig_v2).teams[
         Object.keys(JSON.parse(localStorage.localConfig_v2).teams)[0]
       ].token

     Copy the returned string (it begins with "xoxc-"). If you belong to
     multiple workspaces, make sure the tab is on the workspace you want.

  To get the COOKIE (xoxd-...):
  4. Open the cookie storage view:
       - Chrome:  Application tab → Storage → Cookies.
       - Firefox: Storage tab → Cookies.
       - Safari:  Storage tab → Cookies.
  5. Under Cookies, select https://app.slack.com.
  6. Find the cookie named "d" and copy its Value (it begins with "xoxd-").

These are session credentials tied to your browser login and typically expire
after a week or two. When requests start failing with an auth error, just run
"handler watcher auth slack" again to store fresh values.`

// PromptSlack prints manual devtools instructions (handler has no automated
// browser extraction) and reads the token + cookie pair. An empty token
// means "skip"; the caller (credsetup.TestAndRepair) treats an empty token
// or cookie as a no-op.
func (p *authPrompter) PromptSlack(_ string) (token, cookie string) {
	fmt.Println()
	fmt.Println(slackTokenInstructions)
	fmt.Println()

	fmt.Print("Enter Slack token (xoxc-...) (or press Enter to skip): ")
	tok, err := p.reader.ReadString('\n')
	if err != nil {
		return "", ""
	}
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return "", ""
	}

	fmt.Print("Enter Slack cookie (d value, xoxd-...): ")
	ck, err := p.reader.ReadString('\n')
	if err != nil {
		return "", ""
	}
	return tok, strings.TrimSpace(ck)
}

// PromptJira prints the given instructions and reads the Jira site URL and
// account email needed for greenfield setup (no host/email configured yet).
// Neither value is secret, so both are read as plain (non-masked) lines. An
// empty host means "skip", matching the empty-means-skip contract used by
// PromptToken and PromptSlack.
func (p *authPrompter) PromptJira(instructions string) (host, email string) {
	fmt.Println(instructions)

	fmt.Print("Enter Jira site URL (or press Enter to skip): ")
	h, err := p.reader.ReadString('\n')
	if err != nil {
		return "", ""
	}
	h = strings.TrimSpace(h)
	if h == "" {
		return "", ""
	}

	fmt.Print("Enter Jira account email: ")
	e, err := p.reader.ReadString('\n')
	if err != nil {
		return "", ""
	}
	return h, strings.TrimSpace(e)
}
