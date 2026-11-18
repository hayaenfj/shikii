import { createRequire } from 'node:module';
import { spawnSync } from 'node:child_process';
import assert from 'node:assert/strict';
import test from 'node:test';

// GHSA-fc5q-39xc-vjpf: picomatch (up to 4.0.7, no fixed release) compiled a repeated extglob whose only branch has a
// star, such as +(*) or +(*a), into a quantifier nested in a quantifier, which backtracks exponentially. The same
// happens with several branches (+(a|*b)), with a star inside a branch (+(a*b|c)), with branches that can be cut two
// ways (+(a|ab|b)), with classes, braces and nested groups (+(a|[ab]*)), and with a `+` after the group (@(a|*b)+).
// web/vendor/picomatch is 4.0.7 with those fixes (see SHIKII-PATCH.md there); the override in package.json makes every
// consumer use it.
// Resolve picomatch the way the build does (vite -> tinyglobby -> picomatch), not through a separate test dependency.
const requireVite = createRequire(import.meta.resolve('vite'));
const requireTinyglobby = createRequire(requireVite.resolve('tinyglobby'));
const picomatchDir = requireTinyglobby.resolve('picomatch/package.json').replace(/package\.json$/, '');
const pm = requireTinyglobby('picomatch');
const { transform } = requireTinyglobby(`${picomatchDir}lib/safe-regex.js`);

// Every shape that took exponential time in the unpatched 4.0.7 (the advisory names the first group; the rest is the same
// defect): one branch with a star, several branches with a star, a star inside a branch, branches that can be cut two
// ways, a class, a brace or a nested group next to a star, and a `+` after the group (a quantifier of its own).
const hung = [
  '+(*)', '*(*)', '+(*a)', '*(*a)', '+(a*)', '*(a*)', '+(*a*)', '+(a*b)', '+(?*)', '+(.*)', '+(**)', '*(a*b*)',
  '+(a|*b)', '*(a|*b)', '+(a*|b)', '*(a*|b)', '+(*a|*b)', '+(foo*|bar*)', '*(foo*|bar*)', '+(*.js|*.ts)', '+(a*|*b)', '+(a|b|*c)', '+(a?|*b)', '+(a|b*|*c)',
  '+(a*b|c)', '+(a*b|c*d)', '*(a*b|*c)', '+(a|ab|b|*c)', '+(a|**b)', '+(a|ab|b)', '*(a|ab|b)', '+(ab|a|b)', '*(ab|a|b)x',
  '+(a|[ab]*)', '*(a|[ab]*)', '+(a|{b,c}*)', '+(a|[!b]*)', '+(a|b+(c)*)', '+(*(a)|b*)',
  '@(a|*b)+', '@(a*|b)+', '(a*|b)+', '{a*,b*}+', '{+(b),a}+', '(c[!a]|ab|+(b))+', 'x@(a|*b)+y'
];

// Expected results of the unpatched 4.0.7 on inputs short enough that it finishes (generated once from the upstream
// package): the patch must not change what any of these patterns match.
const strings = ["", "a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", ".", "a/b", "/a", "a/", "ba/ab", "foo", "bar", "foobar", "x.js", "y.ts", "abc", "c", "bc", "bab", "x", "xa", "ax", "xb", "xab", "abx", "y", "bb", "ac", "cc", "bcb", "abcab"];
const table = [
  ["+(*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", ".", "foo", "bar", "foobar", "x.js", "y.ts", "abc", "c", "bc", "bab", "x", "xa", "ax", "xb", "xab", "abx", "y", "bb", "ac", "cc", "bcb", "abcab"]],
  ["*(*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", ".", "foo", "bar", "foobar", "x.js", "y.ts", "abc", "c", "bc", "bab", "x", "xa", "ax", "xb", "xab", "abx", "y", "bb", "ac", "cc", "bcb", "abcab"]],
  ["+(*a)", ["a", "aa", "ba", ".a", "xa"]],
  ["*(*a)", ["a", "aa", "ba", ".a", "xa"]],
  ["+(a*)", ["a", "aa", "ab", "abab", "aab", "a.b", "abc", "ax", "abx", "ac", "abcab"]],
  ["*(a*)", ["a", "aa", "ab", "abab", "aab", "a.b", "abc", "ax", "abx", "ac", "abcab"]],
  ["+(*a*)", ["a", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", "bar", "foobar", "abc", "bab", "xa", "ax", "xab", "abx", "ac", "abcab"]],
  ["+(a*b)", ["ab", "abab", "aab", "a.b", "abcab"]],
  ["+(?*)", []],
  ["+(.*)", [".a"]],
  ["+(**)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "a/b", "/a", "a/", "ba/ab", "foo", "bar", "foobar", "x.js", "y.ts", "abc", "c", "bc", "bab", "x", "xa", "ax", "xb", "xab", "abx", "y", "bb", "ac", "cc", "bcb", "abcab"]],
  ["*(a*b*)", ["ab", "abab", "aab", "a.b", "abc", "abx", "abcab"]],
  ["+(a|*b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bab", "xb", "xab", "bb", "bcb", "abcab"]],
  ["*(a|*b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bab", "xb", "xab", "bb", "bcb", "abcab"]],
  ["+(a*|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bab", "ax", "abx", "bb", "ac", "abcab"]],
  ["*(a*|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bab", "ax", "abx", "bb", "ac", "abcab"]],
  ["+(*a|*b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", "bab", "xa", "xb", "xab", "bb", "bcb", "abcab"]],
  ["*(*a|*b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", "bab", "xa", "xb", "xab", "bb", "bcb", "abcab"]],
  ["+(foo*|bar*)", ["foo", "bar", "foobar"]],
  ["*(foo*|bar*)", ["foo", "bar", "foobar"]],
  ["+(*.js|*.ts)", ["x.js", "y.ts"]],
  ["+(a*|*b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "foobar", "abc", "bab", "ax", "xb", "xab", "abx", "bb", "ac", "bcb", "abcab"]],
  ["+(a|b|*c)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "abc", "c", "bc", "bab", "bb", "ac", "cc", "bcb", "abcab"]],
  ["+(ab|*c)", ["ab", "abab", "abc", "c", "bc", "ac", "cc", "abcab"]],
  ["x+(a|*b)y", []],
  ["x*(a|*b)", ["x", "xa", "xb", "xab"]],
  ["*(a|*b)x", ["x", "ax", "abx"]],
  ["+(a?|*b)", ["b", "aa", "ab", "abab", "aab", "a.b", "bar", "foobar", "bab", "ax", "xb", "xab", "bb", "ac", "bcb", "abcab"]],
  ["+(a*|b?)", ["a", "aa", "ab", "ba", "abab", "aab", "a.b", "abc", "bc", "ax", "abx", "bb", "ac", "abcab"]],
  ["+(*a*|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", ".a", "bar", "foobar", "abc", "bab", "xa", "ax", "xab", "abx", "bb", "ac", "abcab"]],
  ["+(a|b*|*c)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bar", "abc", "c", "bc", "bab", "abx", "bb", "ac", "cc", "bcb", "abcab"]],
  ["*(a*|*b)x", ["x", "ax", "abx"]],
  ["@(a|*b)", ["a", "b", "ab", "abab", "aab", "a.b", "bab", "xb", "xab", "bb", "bcb", "abcab"]],
  ["?(a|*b)", ["a", "b", "ab", "abab", "aab", "a.b", "bab", "xb", "xab", "bb", "bcb", "abcab"]],
  ["+(a)", ["a", "aa"]],
  ["*(a)", ["a", "aa"]],
  ["+(ab)", ["ab", "abab"]],
  ["+(a|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bab", "bb"]],
  ["*(a|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bab", "bb"]],
  ["+(a/*)", ["a/b"]],
  ["+(*/a)", ["/a"]],
  ["@(a*)", ["a", "aa", "ab", "abab", "aab", "a.b", "abc", "ax", "abx", "ac", "abcab"]],
  ["?(a*)", ["a", "aa", "ab", "abab", "aab", "a.b", "abc", "ax", "abx", "ac", "abcab"]],
  ["!(a*)", ["b", "ba", ".a", ".", "foo", "bar", "foobar", "x.js", "y.ts", "c", "bc", "bab", "x", "xa", "xb", "xab", "y", "bb", "cc", "bcb"]],
  ["+(a*(b))", ["a", "aa", "ab", "abab", "aab"]],
  ["+(*(a)|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bab", "bb"]],
  ["+([ab]*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bc", "bab", "ax", "abx", "bb", "ac", "bcb", "abcab"]],
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
  ["+(a*b|c)", ["ab", "abab", "aab", "a.b", "abc", "c", "cc", "abcab"]],
  ["+(a*b|c*d)", ["ab", "abab", "aab", "a.b", "abcab"]],
  ["*(a*b|*c)", ["ab", "abab", "aab", "a.b", "abc", "c", "bc", "ac", "cc", "abcab"]],
  ["+(a|ab|b|*c)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "abc", "c", "bc", "bab", "bb", "ac", "cc", "bcb", "abcab"]],
  ["+(a|**b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bab", "xb", "xab", "bb", "bcb", "abcab"]],
  ["+(a|ab|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bab", "bb"]],
  ["*(a|ab|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bab", "bb"]],
  ["+(ab|a|b)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bab", "bb"]],
  ["*(ab|a|b)x", ["x", "ax", "abx"]],
  ["+(a|[ab]*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bc", "bab", "ax", "abx", "bb", "ac", "bcb", "abcab"]],
  ["*(a|[ab]*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bc", "bab", "ax", "abx", "bb", "ac", "bcb", "abcab"]],
  ["+(a|{b,c}*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bar", "abc", "c", "bc", "bab", "abx", "bb", "ac", "cc", "bcb", "abcab"]],
  ["+(a|[!b]*)", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bar", "abc", "bc", "bab", "abx", "bb", "bcb", "abcab"]],
  ["+(a|b+(c)*)", ["a", "aa", "abc", "bc", "bcb", "abcab"]],
  ["+(*(a)|b*)", []],
  ["@(a|*b)+", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bab", "xb", "xab", "bb", "bcb", "abcab"]],
  ["@(a*|b)+", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bab", "ax", "abx", "bb", "ac", "abcab"]],
  ["(a*|b)+", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bab", "ax", "abx", "bb", "ac", "abcab"]],
  ["{a*,b*}+", ["a", "b", "aa", "ab", "ba", "abab", "aab", "a.b", "bar", "abc", "bc", "bab", "ax", "abx", "bb", "ac", "bcb", "abcab"]],
  ["{+(b),a}+", ["a", "b", "aa", "ab", "ba", "abab", "aab", "bab", "bb"]],
  ["(c[!a]|ab|+(b))+", ["b", "ab", "abab", "bab", "bb", "abcab"]],
  ["x@(a|*b)+y", []],
  ["+(a|*b)+", []],
  ["+(a|b){2}", []],
  ["+(a*)+", []],
  ["+(*)+", []],
  ["*(a*)+", []],
  ["+(a*b|c)+", []],
];

test('picomatch used by the build is the patched copy', () => {
  const meta = requireTinyglobby('picomatch/package.json');
  assert.equal(meta.name, '@shikii/picomatch');
  // fdir, the other consumer, resolves to the same copy
  const requireFdir = createRequire(requireVite.resolve('fdir'));
  assert.equal(requireFdir.resolve('picomatch/package.json'), requireTinyglobby.resolve('picomatch/package.json'));
});

test('a repeated group that backtracked exponentially finishes at once on a long input', () => {
  const inputs = ['a'.repeat(40) + '/', 'ab'.repeat(20) + '/', 'foobar'.repeat(7) + '/', 'abc'.repeat(13) + '/', 'a.b'.repeat(13) + '\n'];
  // The unpatched code needs about 2^40 steps on these (28 characters took 6 to 31 seconds), so the limit is a hang, not a
  // close call. A regular expression that never finishes cannot be interrupted in this process, so one child process runs
  // them all and writes each pattern's name when it is through: a hang then names the pattern it stopped on.
  const script = `const pm = require(${JSON.stringify(picomatchDir)});
    for (const pattern of ${JSON.stringify(hung)}) {
      const re = pm.makeRe(pattern);
      for (const input of ${JSON.stringify(inputs)}) re.test(input);
      process.stdout.write(pattern + '\\n');
    }`;
  const run = spawnSync(process.execPath, ['-e', script], { encoding: 'utf8', timeout: 10000, killSignal: 'SIGKILL' });
  const finished = run.stdout.split('\n').filter(Boolean);
  assert.equal(run.error, undefined, `${hung[finished.length]} did not finish within 10 seconds (${finished.length} of ${hung.length} were through)`);
  assert.deepEqual(finished, hung, run.stderr);
});

test('the patched patterns still match exactly what the unpatched ones did', () => {
  for (const [pattern, matching] of table) {
    for (const input of strings) {
      assert.equal(pm.isMatch(input, pattern), matching.includes(input), `${pattern} against ${JSON.stringify(input)}`);
    }
  }
});

test('case-insensitive matching is kept', () => {
  const nocase = { nocase: true };
  assert.equal(pm.isMatch('ABAB', '+(a|ab|b)', nocase), true);
  assert.equal(pm.isMatch('xAb', '+(a|*B)', nocase), true);
  assert.equal(pm.isMatch('XB', '+(a|*b)', nocase), true);
  assert.equal(pm.isMatch('AB', '+(a|[ab]*)', nocase), true);
  assert.equal(pm.isMatch('Ab', '@(a|*B)+', nocase), true);
  assert.equal(pm.isMatch('ABAB/', '+(a|*b)', nocase), false);
});

test('a request to loosen upstream\'s safeguard keeps upstream\'s output', () => {
  assert.equal(pm.makeRe('+(+(a))', { maxExtglobRecursion: 1 }).source, '^(?:(?=.)(?:(?:a)+)+)$');
  assert.equal(pm.makeRe('+(a|aa)', { maxExtglobRecursion: false }).source, '^(?:(?=.)(?:a|aa)+)$');
  // the default still protects the same patterns
  assert.doesNotMatch(pm.makeRe('+(a|ab|b)').source, /\)\+/);
});

test('no repeat that backtracks exponentially is left in what picomatch returns', () => {
  for (const pattern of hung) {
    const re = pm.makeRe(pattern);
    // transform finds nothing more to rewrite, and understands the whole expression
    const again = transform(re.source, re.flags);
    assert.equal(again.rewritten, 0, `${pattern}: ${re.source}`);
    assert.equal(again.unknown, 0, `${pattern}: ${re.source}`);
  }
});
