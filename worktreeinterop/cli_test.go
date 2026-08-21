package worktreeinterop

import (
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"testing"
)

// fakeExec replaces execCommand with one that runs the test binary's
// TestHelperProcess, feeding it canned stdout/exit via env.
func fakeExec(t *testing.T, stdout string, exitCode int) func() {
	t.Helper()
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_STDOUT="+stdout,
			"HELPER_EXIT="+strconv.Itoa(exitCode),
		)
		return cmd
	}
	return func() { execCommand = orig }
}

func TestListPrimaryResources_FiltersPrimaries(t *testing.T) {
	json := `[{"type":"pr","id":"o/r#1","url":"u1","primary":true},` +
		`{"type":"jira","id":"J-2","url":"u2","primary":false}]`
	defer fakeExec(t, json, 0)()

	got, err := ListPrimaryResources("/some/dir")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []Resource{{Type: "pr", ID: "o/r#1", URL: "u1"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestListPrimaryResources_Empty(t *testing.T) {
	defer fakeExec(t, `[]`, 0)()
	got, err := ListPrimaryResources("/d")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %+v", got)
	}
}

func TestListPrimaryResources_NonZeroExit(t *testing.T) {
	defer fakeExec(t, ``, 1)()
	if _, err := ListPrimaryResources("/d"); err == nil {
		t.Error("expected error on non-zero exit")
	}
}

func TestListPrimaryResources_BadJSON(t *testing.T) {
	defer fakeExec(t, `not json`, 0)()
	if _, err := ListPrimaryResources("/d"); err == nil {
		t.Error("expected error on malformed json")
	}
}

func TestAddResource_ArgvPrimary(t *testing.T) {
	var gotArgs []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotArgs = append([]string{name}, args...)
		// return a command that exits 0 without doing anything
		c := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--")
		c.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "HELPER_EXIT=0")
		return c
	}
	t.Cleanup(func() { execCommand = orig })

	err := AddResource("/d", Resource{Type: "pr", ID: "o/r#1", URL: "u1"}, false)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"worktree", "resources", "add", "pr", "o/r#1", "--url", "u1", "--worktree", "/d"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("argv got %v want %v", gotArgs, want)
	}
}

func TestAddResource_ArgvRelatedNoURL(t *testing.T) {
	var gotArgs []string
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotArgs = append([]string{name}, args...)
		c := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--")
		c.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "HELPER_EXIT=0")
		return c
	}
	t.Cleanup(func() { execCommand = orig })

	_ = AddResource("/d", Resource{Type: "jira", ID: "J-2"}, true)
	want := []string{"worktree", "resources", "add", "jira", "J-2", "--related", "--worktree", "/d"}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("argv got %v want %v", gotArgs, want)
	}
}

// TestHelperProcess is not a real test — it's the fake subprocess.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	os.Stdout.WriteString(os.Getenv("HELPER_STDOUT"))
	code := 0
	if os.Getenv("HELPER_EXIT") == "1" {
		code = 1
	}
	os.Exit(code)
}
