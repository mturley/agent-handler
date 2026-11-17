package git

import "testing"

func TestParsePorcelainV2(t *testing.T) {
	out := `# branch.oid abc
# branch.head feat
# branch.upstream origin/feat
# branch.ab +1 -2
1 .M N... 100644 100644 100644 aaaa aaaa file1.txt
1 .M N... 100644 100644 100644 bbbb bbbb file2.txt
2 R100 N... 100644 100644 100644 cccc cccc file3.txt	file3-old.txt
? new.txt
`
	got := parsePorcelainV2(out)
	want := porcelainV2{Head: "feat", Oid: "abc", Modified: 3, Untracked: 1}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParsePorcelainV2Detached(t *testing.T) {
	out := `# branch.oid abc
# branch.head (detached)
1 .M N... 100644 100644 100644 aaaa aaaa file1.txt
`
	got := parsePorcelainV2(out)
	if got.Head != "(detached)" {
		t.Errorf("Head = %q, want %q", got.Head, "(detached)")
	}
	if got.Modified != 1 {
		t.Errorf("Modified = %d, want 1", got.Modified)
	}
}

func TestParsePorcelainV2IgnoresIgnoredEntries(t *testing.T) {
	out := `# branch.oid abc
# branch.head main
! ignored.txt
`
	got := parsePorcelainV2(out)
	if got.Modified != 0 || got.Untracked != 0 {
		t.Errorf("got %+v, want zero counts", got)
	}
}
