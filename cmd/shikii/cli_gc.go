package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hayaenfj/shikii/internal/spec"
	"github.com/hayaenfj/shikii/internal/store"
)

// cmdGC clears out "old records with no label". Labeled records and labels are kept regardless of age (calibration material).
// By default it only shows. It rewrites only with --apply.
func cmdGC(args []string) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	storeDir := bindStore(fs)
	olderThan := fs.String("older-than", "30d", "delete unlabeled records older than this (e.g. 30d, 12h, 1w; 0 means any age)")
	apply := fs.Bool("apply", false, "actually delete; without it, only count")
	paths, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	age, err := parseAge(*olderThan)
	if err != nil {
		return err
	}
	st := store.Store{Dir: *storeDir}
	var ids []string
	for _, path := range paths {
		sp, err := spec.Load(path)
		if err != nil {
			return err
		}
		ids = append(ids, sp.ID)
	}
	if len(ids) == 0 {
		if ids, err = st.SpecIDs(); err != nil {
			return err
		}
	}
	sort.Strings(ids)
	cutoff := time.Now().Add(-age)
	total := 0
	for _, id := range ids {
		res, err := st.GC(id, cutoff, *apply)
		if err != nil {
			return fmt.Errorf("gc %s: %w", id, err)
		}
		verb := "would remove"
		if *apply {
			verb = "removed"
		}
		fmt.Fprintf(stdout, "%s: %s %d unlabeled record(s) older than %s; keep %d labeled, %d recent, %d unparsable\n",
			id, verb, res.Removed, *olderThan, res.KeptLabeled, res.KeptRecent, res.KeptUnparsed)
		total += res.Removed
	}
	if !*apply && total > 0 {
		fmt.Fprintln(stdout, "dry run: nothing was changed. Re-run with --apply to remove them.")
	}
	return nil
}

// parseAge parses ages like "30d" "1w" "12h". time.ParseDuration does not know d and w, so convert them first. Negative values are rejected.
func parseAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("--older-than needs a value such as 30d, 12h or 0")
	}
	if strings.HasPrefix(s, "-") {
		return 0, fmt.Errorf("--older-than %q: must not be negative", s)
	}
	if s == "0" {
		return 0, nil
	}
	var d time.Duration
	var err error
	switch {
	case strings.HasSuffix(s, "d"):
		d, err = scaledDuration(strings.TrimSuffix(s, "d"), 24*time.Hour)
	case strings.HasSuffix(s, "w"):
		d, err = scaledDuration(strings.TrimSuffix(s, "w"), 7*24*time.Hour)
	default:
		d, err = time.ParseDuration(s)
	}
	if err != nil {
		return 0, fmt.Errorf("--older-than %q: use a duration such as 30d, 1w, 12h or 0", s)
	}
	if d < 0 {
		return 0, fmt.Errorf("--older-than %q: must not be negative", s)
	}
	return d, nil
}

func scaledDuration(num string, unit time.Duration) (time.Duration, error) {
	n, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, err
	}
	scaled := n * float64(unit)
	if math.Signbit(n) || math.IsNaN(scaled) || math.IsInf(scaled, 0) || scaled >= math.Exp2(63) {
		return 0, errors.New("duration must be finite, nonnegative and within range")
	}
	return time.Duration(scaled), nil
}
