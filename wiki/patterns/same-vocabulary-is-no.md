# Write "similar vocabulary alone is no" in the question (noul for sameness)

status: tentative

## When it applies

- Type: noul. When asking whether two records are "the same thing" (duplicate bug reports, inquiries about the same case, the same matter)
- Models: confirmed on Kev-4B, OpenJev and the Bonsai family

## How to write it

After writing the conditions for "yes" (the same cause, one following up the other), add one sentence on **the confusing cases that should be "no"**:

> Answer "no" if they merely concern the same screen, feature or technology.

Putting borderline examples (a different bug on the same screen, a different case of the same kind) in the saved truths as false with weight 0.5 draws the line on that side.

## Evidence

- same-issue / same-inquiry (32 pairs each, 8 borderline): positives and negatives separated perfectly in all 10 combinations of 5 endpoints × 2 sets (2026-10-17). There is no comparison yet with a version lacking this sentence, so it is tentative

## Counter-evidence

None. But there is no comparison run
