package report

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
)

func RenderTable(w io.Writer, r Report) {
	fmt.Fprintf(w, "kubeoptimizer scan — %s\n\n", r.Cluster)
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "EST./MO\tCONF\tCHECK\tTARGET\tREASON")
	for _, f := range r.Findings {
		cost := "-"
		if f.MonthlyCost > 0 {
			cost = fmt.Sprintf("$%.2f", f.MonthlyCost)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", cost, f.Confidence, f.CheckID, f.Target, f.Reason)
	}
	fmt.Fprintf(tw, "\nTOTAL\t\t\t\t$%.2f/mo estimated waste (%d findings)\n",
		r.TotalMonthlyUSD, len(r.Findings))
	tw.Flush()

	if len(r.Findings) > 0 {
		fmt.Fprintln(w, "\nTop actions:")
		max := len(r.Findings)
		if max > 5 {
			max = 5
		}
		for _, f := range r.Findings[:max] {
			if f.Action != "" {
				fmt.Fprintf(w, "  • [%s] %s\n", f.Target, f.Action)
			}
		}
	}
	for _, n := range r.Notes {
		fmt.Fprintf(w, "\n⚠ %s", n)
	}
	fmt.Fprintln(w)
}

func RenderJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
