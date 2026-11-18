import { createRequire } from 'node:module';
import { spawnSync } from 'node:child_process';
import assert from 'node:assert/strict';
import test from 'node:test';

// GHSA-fc5q-39xc-vjpf: picomatch (up to 4.0.7, no fixed release) compiled a repeated extglob whose only branch has a
// star, such as +(*) or +(*a), into a quantifier nested in a quantifier, which backtracks exponentially. The same happens
// with several branches when one has a star (+(a|*b), +(foo*|bar*)).
// web/vendor/picomatch is 4.0.7 with those fixes (see SHIKII-PATCH.md there); the override in package.json makes every
// consumer use it.
// Resolve picomatch the way the build does (vite -> tinyglobby -> picomatch), not through a separate test dependency.
const requireVite = createRequire(import.meta.resolve('vite'));
const requireTinyglobby = createRequire(requireVite.resolve('tinyglobby'));
const picomatchDir = requireTinyglobby.resolve('picomatch/package.json').replace(/package\.json$/, '');
const pm = requireTinyglobby('picomatch');

// Shapes whose only branch has a star, which the vulnerable code turned into (?:...*...)+ or (?:...*...)*.
const vulnerable = ['+(*)', '*(*)', '+(*a)', '*(*a)', '+(a*)', '*(a*)', '+(*a*)', '+(a*b)', '+(?*)', '+(.*)', '+(**)', '*(a*b*)'];

// The same, with several branches and at least one star (the advisory covers the single-branch form; these are the same defect).
const vulnerableMulti = ['+(a|*b)', '*(a|*b)', '+(a*|b)', '*(a*|b)', '+(*a|*b)', '+(foo*|bar*)', '*(foo*|bar*)', '+(*.js|*.ts)', '+(a*|*b)', '+(a|b|*c)', '+(a?|*b)', '+(a|b*|*c)'];
// Shapes that cannot be rewritten without changing what they match (a branch with a star inside, or branches that are not
// uniquely decodable) are read as literal text, which is how upstream already treats its other risky extglobs.
const literalFallback = ['+(a*b|c)', '+(a*b|c*d)', '*(a*b|*c)', '+(a|ab|b|*c)', '+(a|**b)'];

// Expected results of the unpatched 4.0.7 on inputs short enough that it finishes (generated once from the upstream
// package): the patch must not change what any of these patterns match.
const strings = ["", "a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", ".", "a/b", "/a", "a/", "ba/ab", "foo", "bar", "foobar", "x.js", "y.ts", "abc", "c", "bc", "bab", "x", "xa", "ax", "xb", "xab", "abx", "y"];
const table = [
  ["+(*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", ".", "foo", "bar", "foobar", "x.js", "y.ts", "abc", "c", "bc", "bab", "x", "xa", "ax", "xb", "xab", "abx", "y"]],
  ["*(*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", ".", "foo", "bar", "foobar", "x.js", "y.ts", "abc", "c", "bc", "bab", "x", "xa", "ax", "xb", "xab", "abx", "y"]],
  ["+(*a)", ["a", "aa", "ba", ".a", "xa"]],
  ["*(*a)", ["a", "aa", "ba", ".a", "xa"]],
  ["+(a*)", ["a", "aa", "ab", "abab", "aab", "a.b", "abc", "ax", "abx"]],
  ["*(a*)", ["a", "aa", "ab", "abab", "aab", "a.b", "abc", "ax", "abx"]],
  ["+(*a*)", ["a", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", "bar", "foobar", "abc", "bab", "xa", "ax", "xab", "abx"]],
  ["+(a*b)", ["ab", "abab", "aab", "a.b"]],
  ["+(?*)", []],
  ["+(.*)", [".a"]],
  ["+(**)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "a/b", "/a", "a/", "ba/ab", "foo", "bar", "foobar", "x.js", "y.ts", "abc", "c", "bc", "bab", "x", "xa", "ax", "xb", "xab", "abx", "y"]],
  ["*(a*b*)", ["ab", "abab", "aab", "a.b", "abc", "abx"]],
  ["+(a|*b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bab", "xb", "xab"]],
  ["*(a|*b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bab", "xb", "xab"]],
  ["+(a*|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bab", "ax", "abx"]],
  ["*(a*|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bab", "ax", "abx"]],
  ["+(*a|*b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", "bab", "xa", "xb", "xab"]],
  ["*(*a|*b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", "bab", "xa", "xb", "xab"]],
  ["+(foo*|bar*)", ["foo", "bar", "foobar"]],
  ["*(foo*|bar*)", ["foo", "bar", "foobar"]],
  ["+(*.js|*.ts)", ["x.js", "y.ts"]],
  ["+(a*|*b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "foobar", "abc", "bab", "ax", "xb", "xab", "abx"]],
  ["+(a|b|*c)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "abc", "c", "bc", "bab"]],
  ["+(ab|*c)", ["ab", "abab", "abc", "c", "bc"]],
  ["x+(a|*b)y", []],
  ["x*(a|*b)", ["x", "xa", "xb", "xab"]],
  ["*(a|*b)x", ["x", "ax", "abx"]],
  ["+(a?|*b)", ["b", "aa", "ab", "abab", "aab", "a.b", "bar", "foobar", "bab", "ax", "xb", "xab"]],
  ["+(a*|b?)", ["a", "aa", "ab", "ba", "abab", "aab", "a.b", "abc", "bc", "ax", "abx"]],
  ["+(*a*|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", "bar", "foobar", "abc", "bab", "xa", "ax", "xab", "abx"]],
  ["+(a|b*|*c)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bar", "abc", "c", "bc", "bab", "abx"]],
  ["*(a*|*b)x", ["x", "ax", "abx"]],
  ["@(a|*b)", ["a", "b", "ab", "abab", "aab", "a.b", "bab", "xb", "xab"]],
  ["?(a|*b)", ["a", "b", "ab", "abab", "aab", "a.b", "bab", "xb", "xab"]],
  ["+(a)", ["a", "aa"]],
  ["*(a)", ["a", "aa"]],
  ["+(ab)", ["ab", "abab"]],
  ["+(a|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bab"]],
  ["*(a|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bab"]],
  ["+(a/*)", ["a/b"]],
  ["+(*/a)", ["/a"]],
  ["@(a*)", ["a", "aa", "ab", "abab", "aab", "a.b", "abc", "ax", "abx"]],
  ["?(a*)", ["a", "aa", "ab", "abab", "aab", "a.b", "abc", "ax", "abx"]],
  ["!(a*)", ["b", "ba", ".a", ".", "foo", "bar", "foobar", "x.js", "y.ts", "c", "bc", "bab", "x", "xa", "xb", "xab", "y"]],
  ["+(a*(b))", ["a", "aa", "ab", "abab", "aab"]],
  ["+(*(a)|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bab"]],
  ["+([ab]*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bc", "bab", "ax", "abx"]],
  ["x+(*)", ["x.js", "x", "xa", "xb", "xab"]],
  ["+(*)x", ["x", "ax", "abx"]],
  ["a/+(*)", ["a/b", "a/"]],
  ["x*(*)", ["x.js", "x", "xa", "xb", "xab"]],
  ["x*(a*)", ["x", "xa", "xab"]],
  ["*(a*)x", ["x", "ax", "abx"]],
  ["x*(*a)", ["x", "xa"]],
  ["*(*a)x", ["x", "ax"]],
  ["a/*(a*)", ["a/"]],
  ["x+(a*)y", []],
  ["x@(a*)", ["xa", "xab"]],
  ["x?(a*)", ["x", "xa", "xab"]],
];

test('picomatch used by the build is the patched copy', () => {
  const meta = requireTinyglobby('picomatch/package.json');
  assert.equal(meta.name, '@shikii/picomatch');
  // fdir, the other consumer, resolves to the same copy
  const requireFdir = createRequire(requireVite.resolve('fdir'));
  assert.equal(requireFdir.resolve('picomatch/package.json'), requireTinyglobby.resolve('picomatch/package.json'));
});

test('a repeated extglob with a starred branch finishes at once on a long input', () => {
  const inputs = ['a'.repeat(40) + '/', 'ab'.repeat(20) + '/', 'foobar'.repeat(7) + '/', 'abc'.repeat(13) + '/'];
  for (const [pattern, input] of [...vulnerable.map(p => [p, inputs[0]]), ...[...vulnerableMulti, ...literalFallback].flatMap(p => inputs.map(i => [p, i]))]) {
    // The vulnerable code needs 2^40 steps here (28 characters took 6 to 31 seconds), so a hang is the failure.
    // It runs in a child process because a regular expression that never finishes cannot be interrupted in this one.
    const script = `const pm = require(${JSON.stringify(picomatchDir)}); process.stdout.write(String(pm.makeRe(${JSON.stringify(pattern)}).test(${JSON.stringify(input)})));`;
    const run = spawnSync(process.execPath, ['-e', script], { encoding: 'utf8', timeout: 3000, killSignal: 'SIGKILL' });
    assert.equal(run.error, undefined, `${pattern} did not finish within 3 seconds on ${input.length} characters`);
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

test('the rewritten shapes read as literal text keep doing so', () => {
  for (const pattern of literalFallback) {
    assert.equal(pm.isMatch(pattern, pattern), true, pattern);
    assert.equal(pm.isMatch('ab', pattern), false, pattern);
  }
});

test('the vulnerable shapes compile to a single pass, with no quantifier on a group that holds a star', () => {
  for (const pattern of [...vulnerable, ...vulnerableMulti]) {
    assert.doesNotMatch(pm.makeRe(pattern).source, /\*\??\)[+*]/, pattern);
  }
});
