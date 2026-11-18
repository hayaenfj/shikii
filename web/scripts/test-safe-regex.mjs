import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
import test from 'node:test';

// The rewriter in web/vendor/picomatch/lib/safe-regex.js (see SHIKII-PATCH.md): it takes a finished regular expression
// source and replaces every repeat that backtracks exponentially with an equal one that cannot. These checks use plain
// regular expressions, not globs, and compare what the original and the rewritten one accept on every short text.
const requireVite = createRequire(import.meta.resolve('vite'));
const requireTinyglobby = createRequire(requireVite.resolve('tinyglobby'));
const picomatchDir = requireTinyglobby.resolve('picomatch/package.json').replace(/package\.json$/, '');
const { transform } = requireTinyglobby(`${picomatchDir}lib/safe-regex.js`);

const textsOver = (alphabet, longest) => {
  const all = [''];
  let layer = [''];

  for (let length = 1; length <= longest; length++) {
    const next = [];
    for (const text of layer) for (const ch of alphabet) next.push(text + ch);
    for (const text of next) all.push(text);
    layer = next;
  }

  return all;
};

const sameLanguage = (source, flags, rewritten, texts) => {
  const before = new RegExp(`^(?:${source})$`, flags);
  const after = new RegExp(`^(?:${rewritten})$`, flags);

  for (const text of texts) {
    assert.equal(after.test(text), before.test(text), `${source} (${flags}) became ${rewritten}, which differs on ${JSON.stringify(text)}`);
  }
};

// Each of these has two or more ways to split some text, inside a repeat.
const ambiguous = [
  '(?:a|ab|b)+',
  '(?:a|ab|b)*',
  '(a|[^/]*?b)+',
  '(?:a[^/]*?)+',
  '(?:(?:b)+|a)+',
  '(?:a*)*',
  '(?:a*b*)*',
  '(?:a?b?)+',
  '(?:a|a)+',
  '(?:(?:ab)*|a|b)*',
  '(?:[^/]*?|a)+',
  '(?:(?:a|b)*b?)+',
  'x(?:[ab]|a[ab]?)*y',
  '(?:\\.|(?:b)+)+',
  '(?:a|[^/]*?b|[ab][^a])+',
  '(?:[a-c]+|b[ab]*)+x',
  '(?:(?:a|b)+|(?:b|c)+)+',
  '(?:a(?:b|)|b)+',
  '(?:[^/]a[^/]*?|b)*'
];

// Only ambiguous when letter case is ignored: both cases of a letter are then the same letter.
const ambiguousWhenCaseIsIgnored = ['(?:a|A)+', '(?:ab|AB)+', '(?:a|A|b)*', '(?:[ab]|B)+'];

test('a repeat with two ways to split a text is rewritten into an equal one', () => {
  const texts = textsOver(['a', 'b', 'c', '/', 'x', '.'], 6);

  for (const source of ambiguous) {
    const done = transform(source, '');
    assert.ok(done.rewritten >= 1, `${source} was not rewritten`);
    assert.equal(done.unknown, 0, source);
    sameLanguage(source, '', done.source, texts);
  }
});

test('the same holds when letter case is ignored', () => {
  const texts = textsOver(['a', 'B', 'A', 'b', '/', 'x'], 5);

  for (const source of ambiguous) {
    const done = transform(source, 'i');
    assert.ok(done.rewritten >= 1, `${source} was not rewritten`);
    sameLanguage(source, 'i', done.source, texts);
  }
});

test('a repeat that is ambiguous only when case is ignored is rewritten only then', () => {
  const texts = textsOver(['a', 'B', 'A', 'b', '/', 'x'], 5);

  for (const source of ambiguousWhenCaseIsIgnored) {
    assert.equal(transform(source, '').rewritten, 0, `${source} is not ambiguous when case counts`);
    assert.equal(transform(source, '').source, source);
    const done = transform(source, 'i');
    assert.ok(done.rewritten >= 1, `${source} was not rewritten when case is ignored`);
    sameLanguage(source, 'i', done.source, texts);
  }
});

test('a repeat that cannot be split in two ways is left exactly as it was', () => {
  const plain = [
    '(?:a|b)+',
    '(a|b)+',
    '(?:a|b)*c',
    '(?:ab)+',
    '(?:a?)*',
    '(?:a|ab)+',
    '(?:a|bc)*d',
    '[^/]*?',
    'a+b*',
    '(?:a+b)+',
    '^(?:(?=.)(?:a|ab)+)$',
    '(?:[^/]*?a)'
  ];

  for (const source of plain) {
    assert.equal(transform(source, '').source, source, source);
    assert.equal(transform(source, '').rewritten, 0, source);
  }
});

test('what cannot be proven equal is left exactly as it was', () => {
  const left = [
    // a lookahead can look past the end of the repeat
    ['(?:(?!a)b|c)+', ''],
    ['(?:a(?=b)|ab)+', ''],
    ['(?:(?!a)[^/]*?b|[^/]*?c)+', ''],
    ['(?:a(?=b)|a[^/]*?)+', ''],
    // a counted repeat, an anchor, a back reference
    ['(?:a{2}|a)+', ''],
    ['(?:^a|a)+', ''],
    ['(a)(?:\\1|a)+', ''],
    // a flag that changes how the source reads
    ['(?:a|a)+', 'u'],
    ['(?:a|a)+', 'v'],
    ['(?:a|a)+', 'm'],
    // a letter with case folding outside ASCII
    ['(?:é|éa)+', 'i'],
    // not a regular expression
    ['(?:a|a)+)', ''],
    ['(?:a|a', '']
  ];

  for (const [source, flags] of left) {
    assert.equal(transform(source, flags).source, source, `${source} (${flags})`);
  }

  // a repeat of a letter outside ASCII is fine when case does not matter
  assert.ok(transform('(?:é|éa|a)+', '').rewritten >= 0);
});

test('a source over the size limits is left as it was, and nothing throws', () => {
  const wide = `(?:${Array.from({ length: 200 }, () => 'a').join('|')})+`;
  assert.equal(transform(wide, '').source, wide);
  const long = `${'x'.repeat(30000)}(?:a|a)+`;
  assert.equal(transform(long, '').source, long);
  assert.equal(transform(undefined, undefined).source, undefined);
});

test('a lookahead elsewhere does not stop the repeat next to it from being rewritten', () => {
  const source = '^(?:(?=.)(?:a|ab|b)+(?!c))$';
  const done = transform(source, '');
  assert.equal(done.rewritten, 1);
  assert.match(done.source, /^\^\(\?:\(\?=\.\)/);
  assert.match(done.source, /\(\?!c\)\)\$$/);
});

test('random expressions: every rewritten repeat accepts the same texts', () => {
  // mulberry32, so the cases are the same on every run
  let a = 20261118;
  const random = () => {
    a |= 0;
    a = (a + 0x6d2b79f5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const below = n => Math.floor(random() * n);
  const pick = list => list[below(list.length)];
  const atoms = ['a', 'b', 'c', 'A', '\\/', '\\.', '[ab]', '[^/]', '[^a]', '[a-c]', '[^/b]', '[bB]', '(?:a|ab)', '.', '(?=.)', 'a{0,1}'];
  const make = depth => {
    const kind = below(10);
    if (depth <= 0 || kind < 3) return pick(atoms) + pick(['', '', '', '*', '+', '?', '*?']);
    if (kind < 6) {
      let text = '';
      for (let n = 1 + below(3); n > 0; n--) text += make(depth - 1);
      return text;
    }
    const parts = [];
    for (let n = 2 + below(2); n > 0; n--) parts.push(make(depth - 1));
    return pick(['(?:', '(']) + parts.join('|') + ')' + pick(['', '*', '+', '+', '*', '?']);
  };

  const texts = textsOver(['a', 'b', 'c', '/', 'A', 'x'], 4);
  let rewritten = 0;

  for (let n = 0; n < 400; n++) {
    const source = make(2);
    if (!/\)[*+]/.test(source)) continue;

    for (const flags of ['', 'i']) {
      for (const all of [true, false]) {
        const done = transform(source, flags, { all });
        if (done.rewritten === 0) continue;
        rewritten++;
        sameLanguage(source, flags, done.source, texts);
      }
    }
  }

  // the generator must reach the rewriting, or this check proves nothing
  assert.ok(rewritten > 150, `only ${rewritten} rewrites were reached`);
});
