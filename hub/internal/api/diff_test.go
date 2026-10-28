package api

import (
	"strings"
	"testing"
)

func TestUnified(t *testing.T) {
	a := "# web1\n- IP: 1\n- OS: Debian 12\n- Tags: prod\n- Owner: alice\n- Last: x\n- Note: y\n"
	b := "# web1\n- IP: 1\n- OS: Debian 12\n- Tags: prod, web\n- Owner: alice\n- Last: x\n- Note: y\n"
	got := Unified("a/memory.md", "b/memory.md", []byte(a), []byte(b))
	want := "--- a/memory.md\n+++ b/memory.md\n@@ -1,7 +1,7 @@\n # web1\n - IP: 1\n - OS: Debian 12\n-- Tags: prod\n+- Tags: prod, web\n - Owner: alice\n - Last: x\n - Note: y\n"
	if got != want {
		t.Errorf("diff:\n%s\nwant:\n%s", got, want)
	}
	add, rm := DiffStat(got)
	if add != 1 || rm != 1 {
		t.Errorf("stat %d %d", add, rm)
	}
	if Unified("a", "b", []byte(a), []byte(a)) != "" {
		t.Error("identical files give a diff")
	}
	// Append at the end: one hunk, insertion after the last line.
	got = Unified("a", "b", []byte("1\n2\n3\n4\n5\n"), []byte("1\n2\n3\n4\n5\n6\n"))
	if !strings.Contains(got, "@@ -3,3 +3,4 @@\n 3\n 4\n 5\n+6\n") {
		t.Errorf("append diff:\n%s", got)
	}
	// Empty old file: pure insertion.
	got = Unified("a", "b", nil, []byte("x\ny\n"))
	if !strings.Contains(got, "@@ -0,0 +1,2 @@\n+x\n+y\n") {
		t.Errorf("insertion diff:\n%s", got)
	}
	// Two far-apart changes make two hunks.
	long := strings.Repeat("l\n", 20)
	got = Unified("a", "b", []byte("A\n"+long+"B\n"), []byte("a\n"+long+"b\n"))
	if strings.Count(got, "@@ ") != 2 || !strings.Contains(got, "@@ -1,4 +1,4 @@\n-A\n+a\n l\n l\n l\n") || !strings.Contains(got, "@@ -19,4 +19,4 @@\n l\n l\n l\n-B\n+b\n") {
		t.Errorf("two hunks:\n%s", got)
	}
	// Missing final newline is marked.
	got = Unified("a", "b", []byte("x\ny"), []byte("x\ny\n"))
	if !strings.Contains(got, "-y\n\\ No newline at end of file\n+y\n") {
		t.Errorf("newline diff:\n%s", got)
	}
	// Non-UTF-8 bytes and CR survive as data.
	got = Unified("a", "b", []byte("a\r\n\xff\n"), []byte("a\r\n\xfe\n"))
	if !strings.Contains(got, "-\xff\n+\xfe\n") {
		t.Errorf("bytes diff:\n%q", got)
	}
	// A huge difference still is a valid patch.
	var sa, sb strings.Builder
	for i := 0; i < 3000; i++ {
		sa.WriteString("a" + strings.Repeat("x", i%7) + "\n")
		sb.WriteString("b" + strings.Repeat("y", i%5) + "\n")
	}
	got = Unified("a", "b", []byte(sa.String()), []byte(sb.String()))
	add, rm = DiffStat(got)
	if add != 3000 || rm != 3000 || !strings.HasPrefix(got, "--- a\n+++ b\n@@ -1,3000 +1,3000 @@\n") {
		t.Errorf("large diff: +%d -%d, head %q", add, rm, got[:40])
	}
}
