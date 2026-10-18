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

// cmdImport は手元の過去の表(CSV)を正解の貯金(calibrate / tune が --labeled で読む JSON)にする。
// 冷たい出発(線を引く前に 50 件答える)を、業務にもうある表で温めるため。
// 列: spec の入力名(required は必須) + truth(選択肢の id。noul は yes/no/true/false) + 任意で weight (0,1]。
func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	specPath := fs.String("spec", "", "判定の spec(列名と truth の検証に使う)")
	out := fs.String("out", "", "書く先(省略時は <csv>.json)")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *specPath == "" {
		return errors.New("usage: shikii import <past.csv> --spec <spec.yaml> [--out truths.json]\n  列: spec の入力名 + truth [+ weight]。できた JSON は calibrate / tune の --labeled に渡す")
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
		col[strings.TrimSpace(h)] = i
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
