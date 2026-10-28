package api

import (
	"fmt"
	"strings"
)

// Unified renders a unified diff of a against b, as diff -u
// would, with three lines of context; aName and bName are the
// header labels. It is line based: the common prefix and suffix
// are stripped, the middle goes through an LCS table. A middle
// larger than maxCells cells (two files that differ almost
// everywhere) is shown as one removal and one insertion, which
// is still a correct patch. Memory-sized files only.
func Unified(aName, bName string, a, b []byte) string {
	al, aEOL := splitLines(a)
	bl, bEOL := splitLines(b)
	ops := edits(al, bl)
	if !hasChange(ops) && aEOL == bEOL {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", aName, bName)
	writeHunks(&sb, al, bl, aEOL, bEOL, ops)
	return sb.String()
}

// splitLines cuts text into lines without their newline and
// says whether the last line had one.
func splitLines(b []byte) ([]string, bool) {
	if len(b) == 0 {
		return nil, true
	}
	s := string(b)
	eol := strings.HasSuffix(s, "\n")
	if eol {
		s = s[:len(s)-1]
	}
	return strings.Split(s, "\n"), eol
}

// op is one line of the edit script: ' ' kept, '-' removed from
// a, '+' added from b. ai and bi are the positions in a and b
// at that point: the line itself on the side it belongs to, the
// next line on the other side.
type op struct {
	kind byte
	ai   int
	bi   int
}

const maxCells = 4_000_000

// edits computes the edit script of a to b.
func edits(a, b []string) []op {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	var ops []op
	for i := 0; i < pre; i++ {
		ops = append(ops, op{' ', i, i})
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	if len(ma)*len(mb) > maxCells {
		for i := range ma {
			ops = append(ops, op{'-', pre + i, pre})
		}
		for j := range mb {
			ops = append(ops, op{'+', pre + len(ma), pre + j})
		}
	} else {
		ops = append(ops, lcs(ma, mb, pre)...)
	}
	for i := 0; i < suf; i++ {
		ops = append(ops, op{' ', len(a) - suf + i, len(b) - suf + i})
	}
	return ops
}

// lcs is the classic table walk; off shifts the indexes by the
// stripped prefix.
func lcs(a, b []string, off int) []op {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	w := m + 1
	tbl := make([]int32, (n+1)*w)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				tbl[i*w+j] = tbl[(i+1)*w+j+1] + 1
			} else if tbl[(i+1)*w+j] >= tbl[i*w+j+1] {
				tbl[i*w+j] = tbl[(i+1)*w+j]
			} else {
				tbl[i*w+j] = tbl[i*w+j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', off + i, off + j})
			i++
			j++
		case tbl[(i+1)*w+j] >= tbl[i*w+j+1]:
			ops = append(ops, op{'-', off + i, off + j})
			i++
		default:
			ops = append(ops, op{'+', off + i, off + j})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', off + i, off + j})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', off + i, off + j})
	}
	return ops
}

const ctxLines = 3

// writeHunks groups the script into hunks with context lines
// and prints them. A missing final newline is marked on the
// last line of the side that lacks it, as diff does.
func writeHunks(sb *strings.Builder, a, b []string, aEOL, bEOL bool, ops []op) {
	// Without a line change the only difference is the final
	// newline: show the last line as changed.
	if aEOL != bEOL && !hasChange(ops) && len(ops) > 0 {
		last := ops[len(ops)-1]
		ops = append(ops[:len(ops)-1], op{'-', last.ai, last.bi}, op{'+', last.ai + 1, last.bi})
	}
	i := 0
	for i < len(ops) {
		if ops[i].kind == ' ' {
			i++
			continue
		}
		start := i - ctxLines
		if start < 0 {
			start = 0
		}
		end := i
		for end < len(ops) {
			// Extend over the next change if it is within 2*context.
			next := end
			for next < len(ops) && ops[next].kind == ' ' {
				next++
			}
			if next >= len(ops) || next-end > 2*ctxLines {
				end += ctxLines
				if end > len(ops) {
					end = len(ops)
				}
				break
			}
			end = next + 1
			for end < len(ops) && ops[end].kind != ' ' {
				end++
			}
		}
		h := ops[start:end]
		as, al, bs, bl := ranges(h)
		fmt.Fprintf(sb, "@@ -%s +%s @@\n", span(as, al), span(bs, bl))
		for k, o := range h {
			switch o.kind {
			case ' ':
				sb.WriteString(" " + a[o.ai] + "\n")
				if o.ai == len(a)-1 && !aEOL && k == len(h)-1 {
					sb.WriteString("\\ No newline at end of file\n")
				}
			case '-':
				sb.WriteString("-" + a[o.ai] + "\n")
				if o.ai == len(a)-1 && !aEOL {
					sb.WriteString("\\ No newline at end of file\n")
				}
			case '+':
				sb.WriteString("+" + b[o.bi] + "\n")
				if o.bi == len(b)-1 && !bEOL {
					sb.WriteString("\\ No newline at end of file\n")
				}
			}
		}
		i = end
	}
}

func hasChange(ops []op) bool {
	for _, o := range ops {
		if o.kind != ' ' {
			return true
		}
	}
	return false
}

// ranges gives the 1-based start and length of a hunk on each
// side. A side without a line in the hunk gets length 0 and, as
// diff prints it, the number of its lines before the hunk.
func ranges(h []op) (as, al, bs, bl int) {
	for _, o := range h {
		switch o.kind {
		case ' ':
			al++
			bl++
		case '-':
			al++
		case '+':
			bl++
		}
	}
	as, bs = h[0].ai, h[0].bi
	if al > 0 {
		as++
	}
	if bl > 0 {
		bs++
	}
	return
}

func span(start, n int) string {
	if n == 1 {
		return fmt.Sprintf("%d", start)
	}
	if n == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	return fmt.Sprintf("%d,%d", start, n)
}

// DiffStat counts the added and removed lines of a unified
// diff, headers excluded.
func DiffStat(diff string) (added, removed int) {
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++"), strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			added++
		case strings.HasPrefix(l, "-"):
			removed++
		}
	}
	return
}
