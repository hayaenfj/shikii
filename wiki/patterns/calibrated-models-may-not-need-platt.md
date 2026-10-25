# Decide whether calibration is needed from the numbers per (question, model), not from claims

status: confirmed

## When it applies

- Type: noul (the same goes for choice / score with vector)
- Models: all of them. Decision models advertised as "calibrated" as well as ordinary chat models

## How to write it

This is about operation, not wording: follow the CV result of `calibrate`. If identity wins, the line only carries the "verified" mark; if platt wins, write the constants.
Readout from an ordinary chat model pushes probabilities to the extremes (0.999 / 0.003), so a Platt fit easily goes wild. `calib.Fit` uses a line search for this reason.

## Evidence

- OpenJev: same-issue 0.007, same-inquiry 0.037, both identity (Platt got worse in CV)
- Kev-4B: identity on the old material; on the 2 replacement sets platt wins slightly (0.038 → 0.035, 0.030 → 0.020). **Even the same model changes with the question**
- Bonsai 2 27B (BonJev): 0.049 → 0.019, 0.084 → 0.028. A big effect on an ordinary LLM
- Ternary-Bonsai-27B (readout): 0.048 → 0.024, 0.033 → 0.020

[docs/model-comparison.md](https://github.com/hayaenfj/shikii/blob/main/docs/model-comparison.md)
