# picomatch 4.0.7 with one fix

This directory is picomatch 4.0.7 (https://github.com/micromatch/picomatch, MIT, see LICENSE) with one change in
`lib/parse.js`. It is installed in place of the upstream package for every consumer (vite, tinyglobby, fdir) through
`devDependencies.picomatch` and `overrides.picomatch` in `web/package.json`.

## Why

GHSA-fc5q-39xc-vjpf: up to 4.0.7 (the latest release, no fixed version) a repeated extglob whose only branch has a
star, such as `+(*)`, `+(*a)` or `*(a*)`, compiles to a quantifier nested in a quantifier (`(?:[^/]*?a)+`) and
backtracks exponentially on a non-matching input. 28 characters took 6 to 31 seconds, 40 characters never finish.
`npm audit` reports it as high, and CI runs `npm audit --audit-level=low`.

## What changed

`analyzeRepeatedExtglob` returns `{ risky: false, collapse: true }` for a lone branch that has a `*` and is made only
of plain text and wildcards (no group, class, brace, alternation, escape, quote or slash). `extglobClose` then drops
the repeat: `+(X)` becomes `(?:X)` and `*(X)` becomes `(?:X)?`. A branch with a star already matches any run of
non-slash text, so repeating it matches nothing new. Everything else compiles exactly as before.

## How it is checked

`web/scripts/test-picomatch-redos.mjs` (part of `npm run check`) resolves picomatch the way the build does and checks
that the copy in use is this one, that the vulnerable shapes finish on 40 characters, that a table of results taken
from the unpatched 4.0.7 is unchanged, and that no quantifier is left on a group that holds a star.
Also checked once by hand: upstream's own suite (1983 tests) passes on this copy, and 21,840 patterns against all
29.8 million (pattern, text) pairs gave the same match results as the unpatched 4.0.7.

## Not covered

Repeated extglobs with several branches where one has a star (`+(a|*b)`, `+(foo*|bar*)`) are exponential in 4.0.7
and still are here: they are outside the advisory, and no rewrite keeps their meaning.

## When to remove it

When upstream publishes a version after 4.0.7 that fixes GHSA-fc5q-39xc-vjpf: delete this directory, the
`picomatch` entries in `devDependencies` and `overrides` of `web/package.json`, and `web/scripts/test-picomatch-redos.mjs`
(and its name in the `check` script), run `npm --prefix web install`, then `npm --prefix web audit`.
