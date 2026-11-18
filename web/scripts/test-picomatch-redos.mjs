import { createRequire } from 'node:module';
import { spawnSync } from 'node:child_process';
import assert from 'node:assert/strict';
import test from 'node:test';

// GHSA-fc5q-39xc-vjpf: picomatch (up to 4.0.7, no fixed release) compiled a repeated extglob whose only branch has a
// star, such as +(*) or +(*a), into a quantifier nested in a quantifier, which backtracks exponentially.
// web/vendor/picomatch is 4.0.7 with that one fix; the override in package.json makes every consumer use it.
// Resolve picomatch the way the build does (vite -> tinyglobby -> picomatch), not through a separate test dependency.
const requireVite = createRequire(import.meta.resolve('vite'));
const requireTinyglobby = createRequire(requireVite.resolve('tinyglobby'));
const picomatchDir = requireTinyglobby.resolve('picomatch/package.json').replace(/package\.json$/, '');
const pm = requireTinyglobby('picomatch');

// Shapes whose only branch has a star, which the vulnerable code turned into (?:...*...)+ or (?:...*...)*.
const vulnerable = ['+(*)', '*(*)', '+(*a)', '*(*a)', '+(a*)', '*(a*)', '+(*a*)', '+(a*b)', '+(?*)', '+(.*)', '+(**)', '*(a*b*)'];

// Expected results of the unpatched 4.0.7 on inputs short enough that it finishes (generated once from the upstream
// package): the patch must not change what any of these patterns match.
const strings = ["", "a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", ".", "a/b", "/a", "a/", "ba/ab"];
const table = [
  ["+(*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", "."]],
  ["*(*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", "."]],
  ["+(*a)", ["a", "aa", "ba", ".a"]],
  ["*(*a)", ["a", "aa", "ba", ".a"]],
  ["+(a*)", ["a", "aa", "ab", "abab", "aab", "a.b"]],
  ["*(a*)", ["a", "aa", "ab", "abab", "aab", "a.b"]],
  ["+(*a*)", ["a", "aa", "ab", "ba", "abab", "aab", "a.b", ".a"]],
  ["+(a*b)", ["ab", "abab", "aab", "a.b"]],
  ["+(?*)", []],
  ["+(.*)", [".a"]],
  ["+(**)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "a/b", "/a", "a/", "ba/ab"]],
  ["*(a*b*)", ["ab", "abab", "aab", "a.b"]],
  ["+(a)", ["a", "aa"]],
  ["*(a)", ["a", "aa"]],
  ["+(ab)", ["ab", "abab"]],
  ["+(a|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab"]],
  ["*(a|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab"]],
  ["+(a/*)", ["a/b"]],
  ["+(*/a)", ["/a"]],
  ["@(a*)", ["a", "aa", "ab", "abab", "aab", "a.b"]],
  ["?(a*)", ["a", "aa", "ab", "abab", "aab", "a.b"]],
  ["!(a*)", ["b", "ba", ".a", "."]],
  ["+(a*(b))", ["a", "aa", "ab", "abab", "aab"]],
  ["+(*(a)|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab"]],
  ["+([ab]*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b"]],
  ["x+(*)", []],
  ["+(*)x", []],
  ["a/+(*)", ["a/b", "a/"]],
  ["x*(*)", []],
  ["x*(a*)", []],
  ["*(a*)x", []],
  ["x*(*a)", []],
  ["*(*a)x", []],
  ["a/*(a*)", ["a/"]],
  ["x+(a*)y", []],
  ["x@(a*)", []],
  ["x?(a*)", []],
];

test('picomatch used by the build is the patched copy', () => {
  const meta = requireTinyglobby('picomatch/package.json');
  assert.equal(meta.name, '@shikii/picomatch');
  // fdir, the other consumer, resolves to the same copy
  const requireFdir = createRequire(requireVite.resolve('fdir'));
  assert.equal(requireFdir.resolve('picomatch/package.json'), requireTinyglobby.resolve('picomatch/package.json'));
});

test('a repeated extglob whose only branch has a star finishes at once on a long input', () => {
  const input = 'a'.repeat(40) + '/';
  for (const pattern of vulnerable) {
    // The vulnerable code needs 2^40 steps here (28 characters took 6 to 31 seconds), so a hang is the failure.
    // It runs in a child process because a regular expression that never finishes cannot be interrupted in this one.
    const script = `const pm = require(${JSON.stringify(picomatchDir)}); process.stdout.write(String(pm.makeRe(${JSON.stringify(pattern)}).test(${JSON.stringify(input)})));`;
    const run = spawnSync(process.execPath, ['-e', script], { encoding: 'utf8', timeout: 3000, killSignal: 'SIGKILL' });
    assert.equal(run.error, undefined, `${pattern} did not finish within 3 seconds on 40 characters`);
    // only that it finishes matters here (+(**) matches this input because ** crosses the slash); what each
    // pattern matches is checked against the table below
    assert.match(run.stdout, /^(true|false)$/, `${pattern}: ${run.stderr}`);
  }
});

test('the patched patterns still match exactly what the unpatched ones did', () => {
  for (const [pattern, matching] of table) {
    for (const input of strings) {
      assert.equal(pm.isMatch(input, pattern), matching.includes(input), `${pattern} against ${JSON.stringify(input)}`);
    }
  }
});

test('the vulnerable shapes compile to a single pass, with no quantifier on a group that holds a star', () => {
  for (const pattern of vulnerable) {
    assert.doesNotMatch(pm.makeRe(pattern).source, /\*\??\)[+*]/, pattern);
  }
});
