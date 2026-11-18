# picomatch 4.0.7 with a ReDoS fix

This directory is picomatch 4.0.7 (https://github.com/micromatch/picomatch, MIT, see LICENSE) with changes in
`lib/parse.js` and `lib/picomatch.js`, and one added file, `lib/safe-regex.js`. It is installed in place of the upstream
package for every consumer (vite, tinyglobby, fdir) through `devDependencies.picomatch` and `overrides.picomatch` in
`web/package.json`.

## Why

GHSA-fc5q-39xc-vjpf: up to 4.0.7 (the latest release, no fixed version) a repeated extglob whose only branch has a
star, such as `+(*)`, `+(*a)` or `*(a*)`, compiles to a quantifier nested in a quantifier (`(?:[^/]*?a)+`) and
backtracks exponentially on a non-matching input. 28 characters took 6 to 31 seconds, 40 characters never finish.
`npm audit` reports it as high, and CI runs `npm audit --audit-level=low`.

The advisory names one shape, but the defect is any repeat whose body can be split in more than one way, and upstream
hangs on all of them: several branches with a star (`+(a|*b)`, `+(foo*|bar*)`, `+(*.js|*.ts)`), a star inside a branch
(`+(a*b|c)`), branches that can be cut two ways (`+(a|ab|b)`), a class, a brace or a nested group next to a star
(`+(a|[ab]*)`, `+(a|{b,c}*)`, `+(a|b+(c)*)`), and a `+` after a group, which is a quantifier of its own
(`@(a|*b)+`, `{a*,b*}+`, `(a*|b)+`). All of these are fixed here.

## What changed

Three layers, cheapest first. Each one gives a regular expression that accepts exactly the texts upstream's accepts.

1. `lib/parse.js`, one branch with a `*` (`+(*)`, `+(a*b)`): the repeat is dropped. A branch with a star already matches any run
   of non-slash text, so `+(X)` matches what `X` matches and `*(X)` what `X` or the empty string matches.
2. `lib/parse.js`, several plain branches, at least one with a `*`: a branch that starts with `*` takes in any text before
   it, and one that ends with `*` takes in any text after it, so a string is a sequence of the branches exactly when it is
   `[one leading branch] [any number of plain branches] [one trailing branch]`. The group is emitted that way, so only the
   plain branches are repeated, and those must be fixed text that cannot be cut two ways (Sardinas-Patterson test).
   `+(a|*b)` becomes `(?:(?:[^/]*?b)(?:a)*|(?:a)+)`.
3. `lib/safe-regex.js`, called from `picomatch.toRegex` on the finished regular expression source, so it covers whatever
   the first two layers do not, whichever way the source was produced. It reads the source into a tree (plain
   syntax only: literals, classes, groups, alternation, `* + ?`), counts for every move of its position automaton in how
   many ways the expression can make it, and tests for exponential ambiguity (two different paths on the same text that
   leave a state and come back to it). Only a repeat that is ambiguous is touched: it is made deterministic (subset
   construction), minimized and written back by state elimination, so every text has one way to match and the time is
   linear. A repeat that is not ambiguous, and every other part of the source, is left byte for byte.

What stays as upstream has it:

- Anything the rewriter does not rewrite: the output is upstream's own, never a guess. The rewrite is exact or absent.
- A `+` or `?` right after the closing paren of `+(X)` or `*(X)`: after `+` upstream's output is not a valid regular
  expression (`(?:X)++`) and the pattern never matches; after `?` it is a lazy `+?`, so the group is still needed once
  (`a+(a*)?` does not match `a`). Collapsing the group would change both, so the first two layers leave them as upstream
  has them; this change is about the hang only. The third layer may still rewrite the finished expression (`+(a*)?` becomes
  `(?:a[^/]*)`), which accepts the same texts. Only the invalid `+` form stays exactly as upstream has it.
- Upstream's own risky groups (`+(*(a)|b)`, a group with an empty or all-star branch, ...): they keep their output, and so
  their rebuild from the tokens, which joins text by its raw value and leaves a `.` in `x.js` after the group unescaped
  (`+(*(a)|b)x.js` matches `xajs`). A group this patch rewrites is output the way upstream outputs any other group, token by
  token as it is read, so the `.` stays escaped, unless something after the group makes upstream rebuild the output as it does
  for its own risky groups (`+(a|*b)x.js{1..3}`: a brace range, `[[:alpha:]]`, `***`); then the output is upstream's, with
  the same unescaped `.`.
- `maxExtglobRecursion: false`, or a number above the default: these are upstream's explicit requests to switch its safeguard
  off or to allow nested repeats, so the source is left as built (upstream's own test suite asserts this).
- Capturing groups inside a rewritten repeat are not kept, so the numbers of the groups after it move. This shows only
  in `exec` results of a `capture: true` pattern, and only for patterns that hung before.

## How it is checked

`web/scripts/test-picomatch-redos.mjs` and `web/scripts/test-safe-regex.mjs` (both part of `npm run check`):

- the copy in use is this one, and every shape above finishes on 40-character non-matching inputs (one child process
  that names the pattern it stopped on, killed after 10 seconds: the unpatched code needs about 2^40 steps there);
- a table of results taken from the unpatched 4.0.7 (87 patterns, 36 texts) is unchanged, including the patterns that never
  match because of the `++` above;
- what follows a rewritten group keeps its meaning: a `.` in the text after it stays a dot (`+(a|*b)x.js` does not match
  `axzjs`), and a `?` right after it keeps the group needed once (`a+(a*)?` does not match `a`);
- what picomatch returns holds no repeat that is still ambiguous;
- the rewriter, on plain regular expressions: 19 hand-picked ambiguous repeats and 4 that are ambiguous only when case is
  ignored are rewritten and accept the same texts, the repeats that are not ambiguous and everything it cannot prove
  (lookahead, anchor, back reference, counted repeat, `u`/`v`/`m`/`s` flags, non-ASCII letters with case folding, size
  limits) are left unchanged, and 400 seeded random expressions accept the same texts before and after.

Each check was seen failing against a deliberately broken copy (the rewriter not called, the ambiguity test always no,
routes not counted, an edge label cut short, the empty text not accepted, case not folded, a lookahead read as plain
syntax, the `+` rule removed, the loosening request ignored).

Also checked by hand when the patch was made (against the unpatched 4.0.7, with a seeded generator of 20 shapes of
repeated groups such as classes, braces, nested extglobs, `**`, `!(...)` and a `+` after the group):

- upstream's own suite (1983 tests) passes on this copy;
- one branch: 21,840 patterns against all 29.8 million (pattern, text) pairs gave the same match results;
- several branches: 15,935 branch sets with `+`, `*` and three surroundings; the 37,026 patterns an independent classifier
  calls exact gave the same results on all 1,366 texts of up to 5 characters;
- any shape: about 1.5 million generated patterns, with the default options and with `nocase`, `dot`, `capture`,
  `windows`, `bash` and `strictBrackets`, each against about 4,000 texts: the same match results on every pattern;
- the rewriter alone: random regular expressions up to nesting depth 4 and texts up to 7 characters, 800 million
  comparisons, with V8's linear-time engine (`--enable-experimental-regexp-engine`, flag `l`) as the reference for the
  original: no difference;
- time: of the generated patterns, the ones that still take over 1.5 seconds on non-matching input all contain a lookahead
  (see below); none without one does.

A first version of these random generators used a linear congruential generator with a modulus, whose low bits repeat, so
some combinations (a `+` after the group) were never generated and a difference went unseen for a while. They use
mulberry32 now. They also put almost no text after the group, so two differences (a `.` after the group, a `?` right after
it) were found only by the independent audit before 0.37.1. A comparison with every shape of group, four kinds of prefix
and 23 kinds of text after it (14,812 patterns against 2,801 texts, with the default, `dot` and `nocase` options) found no
difference after the fix.

## Not covered

A repeated group whose regular expression holds a lookahead is not rewritten: a negated extglob inside it
(`+(!(a)b|c)`), a `**` that is a whole path segment (`+(a|b/**)`), or a branch that starts with `.` (the dot rule adds
`(?!\.{0,1}(?:\/|$))`). A lookahead can see past the end of the repeat, into what follows it, so no equal expression can be built
from the repeat alone. These compile as upstream does and can still backtrack exponentially. They are outside the advisory, and
the build only passes fixed globs from its own configuration to picomatch.

Also left as upstream has it: a repeat whose automaton has more than 160 positions, 200 distinct character sets, 256 states,
or whose rewritten expression would pass 64 KB; a source over 20,000 characters; the `u`, `v`, `m` and `s` flags; and, under
case-insensitive matching, a letter outside ASCII.

## When to remove it

When upstream publishes a version after 4.0.7 that fixes GHSA-fc5q-39xc-vjpf: delete this directory, the
`picomatch` entries in `devDependencies` and `overrides` of `web/package.json`, and `web/scripts/test-picomatch-redos.mjs`
and `web/scripts/test-safe-regex.mjs` (and their names in the `check` script), run `npm --prefix web install`, then
`npm --prefix web audit`. Check that the new upstream also handles the shapes listed under Why (start with `+(a|*b)`,
`+(a|ab|b)` and `@(a|*b)+`) before relying on it for them.
