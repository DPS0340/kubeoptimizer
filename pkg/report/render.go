package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/DPS0340/kubeoptimizer/pkg/check"
)

// ANSI palette. Color is on only when writing to a real terminal, and
// NO_COLOR / CLICOLOR_FORCE are honored (https://no-color.org).
const (
	ansiReset  = "\033[0m"
	ansiBold   = "\033[1m"
	ansiDim    = "\033[2m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
)

type painter bool

func newPainter(w io.Writer) painter {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("CLICOLOR_FORCE") != "" && os.Getenv("CLICOLOR_FORCE") != "0" {
		return true
	}
	f, ok := w.(*os.File)
	return painter(ok && term.IsTerminal(int(f.Fd())))
}

// paint colors s for direct writes.
func (p painter) paint(code, s string) string {
	if !p {
		return s
	}
	return code + s + ansiReset
}

func (p painter) confColor(c check.Confidence) string {
	if c == check.Certain {
		return ansiGreen
	}
	return ansiYellow
}

// row is one plain-text table line plus per-column color codes.
// Layout is computed on the plain text; color is applied after
// padding so ANSI codes never skew the columns.
type row struct {
	cells  [5]string
	colors [5]string
}

func writeRows(w io.Writer, p painter, rows []row) {
	const gap = 2
	var widths [5]int
	for _, r := range rows {
		for i, c := range r.cells[:4] { // last column is never padded
			if n := utf8.RuneCountInString(c); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for _, r := range rows {
		var b strings.Builder
		for i, c := range r.cells {
			pad := 0
			if i < 4 {
				pad = widths[i] - utf8.RuneCountInString(c) + gap
			}
			if r.colors[i] != "" && c != "" {
				b.WriteString(p.paint(r.colors[i], c))
			} else {
				b.WriteString(c)
			}
			if i < 4 {
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}

func RenderTable(w io.Writer, r Report) {
	p := newPainter(w)
	fmt.Fprintf(w, "%s — %s\n\n", p.paint(ansiBold, "kubeoptimizer scan"), r.Cluster)
	head := ansiBold + ansiDim
	rows := []row{{
		cells:  [5]string{"EST./MO", "CONF", "CHECK", "TARGET", "REASON"},
		colors: [5]string{head, head, head, head, head},
	}}
	for _, f := range r.Findings {
		cost := "-"
		costColor := ""
		if f.MonthlyCost > 0 {
			cost = fmt.Sprintf("$%.2f", f.MonthlyCost)
			costColor = ansiBold + ansiRed
		}
		rows = append(rows, row{
			cells:  [5]string{cost, string(f.Confidence), f.CheckID, f.Target, f.Reason},
			colors: [5]string{costColor, p.confColor(f.Confidence), ansiCyan, "", ansiDim},
		})
	}
	writeRows(w, p, rows)
	fmt.Fprintf(w, "\n%s  %s\n",
		p.paint(ansiBold, "TOTAL"),
		p.paint(ansiBold+ansiRed, fmt.Sprintf("$%.2f/mo estimated waste (%d findings)", r.TotalMonthlyUSD, len(r.Findings))))

	if len(r.Findings) > 0 {
		fmt.Fprintln(w, "\n"+p.paint(ansiBold, "Top actions:"))
		max := len(r.Findings)
		if max > 5 {
			max = 5
		}
		for _, f := range r.Findings[:max] {
			if f.Action != "" {
				fmt.Fprintf(w, "  • %s %s\n", p.paint(ansiCyan, "["+f.Target+"]"), f.Action)
			}
		}
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "\n%s", p.paint(ansiYellow, "⚠ "+n))
	}
	fmt.Fprintln(w)
}

func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
