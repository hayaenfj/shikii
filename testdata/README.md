# testdata

Everything here is **made up**. It has nothing to do with real products, orders or people; the text was written for this repository.

## same-issue-labeled.json

32 pairs, in Japanese, for calibrating duplicate detection of bug reports (`decisions/same-issue.yaml`, yes/no).
12 same bug (`truth: true`), 12 different bugs (`false`), and 8 on the same screen or feature but different bugs (borderline; `false` with weight 0.5).
The borderline pairs are examples that "must not be treated as the same automatically", so they sit on the false side. There is no `raw`, so the model is asked.

## same-inquiry-labeled.json

32 pairs, in Japanese, for calibrating the same-inquiry decision (`decisions/same-inquiry.yaml`, yes/no).
12 same case, 12 different cases, and 8 of the same kind of problem but different cases (borderline; weight 0.5). The order numbers are fictional.

## prompt-intent-labeled.json / -ja.json / -en.json

The **seed set** for prompt-intent (5 choices): 62 requests in English and Japanese (23 Japanese, 39 English). The truth labels were set by the worker (Claude).

## store/labels/

The **backing** for the calibration blocks written in `decisions/*.yaml`. `check` re-measures the precision at act per (spec_hash, model) from the store's labels,
so in CI, which lacks the local `.shikii/` (ignored by Git), pass this folder as `--store testdata/store`.
The contents are copies of the labels that `calibrate --write` saved (trace_id, spec_hash, model, inputs_hash, raw, truth, weight).
**No text is included** (inputs are hashes only). When you rewrite a calibration, copy the rows for the same (spec_hash, model) again.
