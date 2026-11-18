# picomatch 4.0.7 with one fix

This directory is picomatch 4.0.7 (https://github.com/micromatch/picomatch, MIT, see LICENSE) with changes in
`lib/parse.js` only. It is installed in place of the upstream package for every consumer (vite, tinyglobby, fdir) through
`devDependencies.picomatch` and `overrides.picomatch` in `web/package.json`.

## Why

GHSA-fc5q-39xc-vjpf: up to 4.0.7 (the latest release, no fixed version) a repeated extglob whose only branch has a
star, such as `+(*)`, `+(*a)` or `*(a*)`, compiles to a quantifier nested in a quantifier (`(?:[^/]*?a)+`) and
backtracks exponentially on a non-matching input. 28 characters took 6 to 31 seconds, 40 characters never finish.
`npm audit` reports it as high, and CI runs `npm audit --audit-level=low`.

The same defect exists with several branches when one of them has a star (`+(a|*b)`, `+(foo*|bar*)`, `+(*.js|*.ts)`):
the advisory does not name them, upstream still hangs on them, and they are fixed here too.

## What changed

Both fixes are in `analyzeRepeatedExtglob` (decides) and `extglobClose` (emits). They apply to `+(...)` and `*(...)` whose
branches are plain text and wildcards (`*`, `?`) with no slash, group, class, brace, alternation, escape or quote.

- One branch with a `*`: the repeat is dropped. A branch with a star already matches any run of non-slash text, so
  `+(X)` matches what `X` matches and `*(X)` what `X` or the empty string matches.
- Several branches, at least one with a `*`: a branch that starts with `*` takes in any text before it, and one that ends
  with `*` takes in any text after it. So a string is a sequence of the branches exactly when it is
  `[one leading branch] [any number of plain branches] [one trailing branch]` (at least one part present). The group is
  emitted that way, from the parser's own output for each branch, so nothing is repeated except the plain branches. Those
  must be fixed text with no `*` that cannot be cut two ways (Sardinas-Patterson test; `?` only when they all have the same
  length), so repeating them is linear. `+(a|*b)` becomes `(?:(?:[^/]*?b)(?:a)*|(?:a)+)`.
- When no equal pattern is proven (a branch with a star inside, such as `a*b`; `**` in a branch; plain branches that can
  be cut two ways), the group is read as literal text, which is what upstream already does for its other risky shapes.
  Those patterns stop matching as extglobs.

Everything else compiles exactly as before.

## How it is checked

`web/scripts/test-picomatch-redos.mjs` (part of `npm run check`) resolves picomatch the way the build does and checks
that the copy in use is this one, that the vulnerable shapes finish on 40 characters, that a table of results taken
from the unpatched 4.0.7 is unchanged, that the shapes read as literal text still do, and that no quantifier is left on
a group that holds a star.

Also checked by hand when the patch was made (against the unpatched 4.0.7):

- upstream's own suite (1983 tests) passes on this copy;
- one branch: 21,840 patterns against all 29.8 million (pattern, text) pairs gave the same match results;
- several branches: 15,935 branch sets (all pairs of the words of up to 3 characters over `a b * ? .`, and 4,000 random
  triples) with `+`, `*` and three surroundings. The 37,026 patterns an independent classifier calls exact gave the same
  match results on all 1,366 texts of up to 5 characters, and none of the 95,610 patterns took more than 50 ms on four
  30-character non-matching inputs.

## Not covered

Still slow in 4.0.7 and here: repeated extglobs whose branches hold a bracket class, a brace or a nested group together with
a star (`+(a|[ab]*)`), a regex-style quantifier after a group (`@(a|*b)+`), and star-free branches that can be cut two ways
(`+(a|ab|b)`, which grows by about 1.6 times per character: 69 ms on 40 characters).
They are outside the advisory. The build only passes fixed globs from its own configuration to picomatch.

## When to remove it

When upstream publishes a version after 4.0.7 that fixes GHSA-fc5q-39xc-vjpf: delete this directory, the
`picomatch` entries in `devDependencies` and `overrides` of `web/package.json`, and `web/scripts/test-picomatch-redos.mjs`
(and its name in the `check` script), run `npm --prefix web install`, then `npm --prefix web audit`. Check that the new
upstream also handles the several-branch shapes (`+(a|*b)`) before relying on it for them.
