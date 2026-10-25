# Spell out other as "not a request"

status: tentative  <!-- 2026-10-18: it helped only on one set, with Kev-4B. Make it confirmed once it helps on another model or another set of truths (a person changes this) -->

## When it applies

- Type: choice. When the options include a catch-all such as "other", "unknown" or "none of these"
- Models: confirmed on Kev-4B. The same reasoning should hold for other decision models (the vaguer a catch-all's description, the more it absorbs)

## How to write it

Instead of describing the catch-all as "none of the above", write **concretely what belongs in it**. For example:

> other: Not a request at all - greetings, thanks, acknowledgements ("ok", "了解"), small talk, or ending the session.

At the same time, put examples of short inputs in the other options' descriptions ("fix the typo", "直して"). This stops short inputs from drifting into the catch-all.

## Evidence

- prompt-intent v1 → v2 (Kev-4B, 62 items): 19 of 21 errors had fallen into other → with concrete descriptions, top-1 went 0.682 → 0.903 and errors into other went to 0. [specs/prompt-intent](https://github.com/hayaenfj/shikii/blob/main/wiki/specs/prompt-intent.md)
- A candidate tightening other further on prompt-intent v2 (other-tight): 0.968 → 0.984 on Ternary-Bonsai-27B, but rejected at holdout 0.952 (not significant on 62 items). The effect is close to its ceiling

## Counter-evidence

None (as of 2026-10-17)

## Notes from people

Tightening the catch-all too much pushes inputs that truly belong in it into other options. In the confusion table, look at the other column as well as the other row.
