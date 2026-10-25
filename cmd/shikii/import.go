package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/hayaenfj/shikii/internal/spec"
)

// cmdImport turns a local table of past cases (CSV) into a bank of truths (the JSON that calibrate / tune read with --labeled).
// It warms up a cold start (answering 50 cases before a line can be drawn) with a table the business already has.
// Columns: the spec's input names (required ones must be present) + truth (a choice id; for noul, yes/no/true/false) + an optional weight in (0,1].
func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	specPath := fs.String("spec", "", "the decision spec (used to check column names and truth)")
	out := fs.String("out", "", "where to write (default: <csv>.json)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *specPath == "" {
		return errors.New("usage: shikii import <past.csv> --spec <spec.yaml> [--out truths.json]\n  columns: the spec's input names + truth [+ weight]. Pass the resulting JSON to calibrate / tune with --labeled")
	}
	sp, err := spec.Load(*specPath)
	if err != nil {
		return err
	}
	f, err := os.Open(pos[0])
	if err != nil {
		return err
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("%s: header: %w", pos[0], err)
	}
	col := map[string]int{}
	for i, h := range header {
		name := strings.TrimSpace(strings.TrimPrefix(h, "\ufeff")) // Excel's UTF-8 BOM sticks to the first column name
		if _, dup := col[name]; dup {
			return fmt.Errorf("%s: column %q appears twice", pos[0], name)
		}
		col[name] = i
	}
	if _, ok := col["truth"]; !ok {
		return fmt.Errorf("%s: no \"truth\" column (columns: %s)", pos[0], strings.Join(header, ", "))
	}
	for _, req := range sp.State.Required {
		if _, ok := col[req]; !ok {
			return fmt.Errorf("%s: required input %q has no column (columns: %s)", pos[0], req, strings.Join(header, ", "))
		}
	}
	var rows []labeled
	for n := 1; ; n++ {
		rec, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("%s: row %d: %w", pos[0], n, err)
		}
		inputs := map[string]string{}
		for name, i := range col {
			if name == "truth" || name == "weight" || i >= len(rec) {
				continue
			}
			inputs[name] = rec[i]
		}
		truth := strings.TrimSpace(rec[col["truth"]])
		if sp.Type == spec.Noul {
			switch strings.ToLower(truth) {
			case "yes", "true", "1":
				truth = "yes"
			case "no", "false", "0":
				truth = "no"
			default:
				return fmt.Errorf("%s: row %d: noul truth must be yes/no/true/false, got %q", pos[0], n, truth)
			}
		} else if !sp.HasTruthKey(truth) {
			return fmt.Errorf("%s: row %d: truth %q is not an option of %s", pos[0], n, truth, sp.ID)
		}
		row := labeled{Inputs: inputs}
		if sp.Type == spec.Noul {
			row.Truth = json.RawMessage(strconv.FormatBool(truth == "yes"))
		} else {
			row.Truth, _ = json.Marshal(truth)
		}
		if wi, ok := col["weight"]; ok && wi < len(rec) && strings.TrimSpace(rec[wi]) != "" {
			w, err := strconv.ParseFloat(strings.TrimSpace(rec[wi]), 64)
			if err != nil || w <= 0 || w > 1 {
				return fmt.Errorf("%s: row %d: weight must be in (0, 1], got %q", pos[0], n, rec[wi])
			}
			row.Weight = w
		}
		rows = append(rows, row)
	}
	if *out == "" {
		*out = strings.TrimSuffix(pos[0], ".csv") + ".json"
	}
	b, err := json.MarshalIndent(rows, "", " ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s: %d truths for %s (pass it to calibrate / tune with --labeled)\n", *out, len(rows), sp.ID)
	return nil
}
