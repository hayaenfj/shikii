// The glossary of the page (0.9.2 redesign P4; npm run check). Words for the AI's insides were replaced with plain words a person who is not an engineer reads.
// This check reads every text literal in the dictionaries (src/locales/*.js, comments removed) and the shipped questions' display texts (integrations/claude-code/specs/*.yaml
// display.ja / display.en), and fails if an old word comes back. The table is the glossary itself (old word → the word to use); adding a row both documents the change and keeps it.
// The components (src/**/*.svelte) are held by an allowlist instead (audit 0.9.2 ui, test hole 5): the visible text in their markup and the static text attributes may hold no words
// (only marks such as ✓ → · : % and the name shikii), and a Japanese text literal in their scripts must be one of the few listed below, so every word a person reads comes from a dictionary.
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, dirname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = join(dirname(fileURLToPath(import.meta.url)), '..');
const dir = join(web, 'src', 'locales');
const specsDir = join(web, '..', 'integrations', 'claude-code', 'specs');

export const glossary = {
  ja: [
    ['判定', 'AI の見立て / 記録 / 問い'],
    ['答え合わせ', 'あなたの答え / 正解'],
    ['線', 'AI がひとりで決めてよい境目'],
    ['帯', 'AI がひとりで進める / あなたに聞く / 止める'],
    ['閾値', 'AI がひとりで決めてよい境目'],
    ['しきい値', 'AI がひとりで決めてよい境目'],
    ['ターン', '今回'],
    ['人の依頼', 'あなたの依頼'],
    ['較正', 'AI がひとりで決めてよい境目を決め直すこと'],
    ['抜き打ち', '確かめのため時々出す分'],
    ['抜き取り', '確かめのため時々出す分'],
    ['分けられない', '今の聞き方では見分けられない'],
    ['仮採点', 'AI の仮の答え'],
    ['作法', '作業の手順'],
    ['札', 'カード'],
    ['spec', '問い'],
    ['top-1', '一致した割合'],
    ['brier', 'ずれ'],
    ['act の', 'AI が自分で進めた時の'],
    ['confirmed', '確かめ済み'],
    ['shikii guard', '(コマンドは出さず)AI に頼む'],
    ['--init', '(コマンドは出さず)AI に頼む'],
    ['(none)', '変更なし'],
  ],
  en: [
    ['calibrat', 'the point where the AI may decide alone'],
    ['threshold', 'the point where the AI may decide alone'],
    ['band', 'the AI goes ahead / asks you / stops'],
    ['line', 'the point where the AI may decide alone'],
    ['this turn', 'this time'],
    ['spot check', 'shown now and then to check'],
    ['audit sample', 'shown now and then to check'],
    ['cannot separate', 'this wording cannot tell the answers apart'],
    ['top-1', 'share that matched'],
    ['brier', 'error'],
    ['@act', 'when the AI went ahead'],
    ['decision model', 'the AI that answers'],
    ['spec', 'question'],
    ['shikii guard', '(no command) ask the AI'],
    ['--init', '(no command) ask the AI'],
    ['--write', '(no command)'],
    ['(none)', 'no changes'],
  ],
};

// the text literals of a dictionary file: '…', "…" and `…` outside comments
export function literals(src) {
  const out = [];
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (c === '/' && src[i + 1] === '/') { i = src.indexOf('\n', i); if (i < 0) break; continue; }
    if (c === "'" || c === '"' || c === '`') {
      let j = i + 1, s = '';
      while (j < src.length && src[j] !== c) { if (src[j] === '\\') { s += src[j + 1]; j += 2; continue; } s += src[j]; j++; }
      out.push(s); i = j + 1; continue;
    }
    i++;
  }
  return out;
}

export function offending(lang, src) {
  const found = [];
  for (const text of literals(src)) for (const [old] of glossary[lang]) if (text.toLowerCase().includes(old.toLowerCase())) found.push(old + ' in "' + text.slice(0, 50) + '"');
  return found;
}

test('the check catches an old word (a broken dictionary fails)', () => {
  assert.notDeepEqual(offending('ja', "x: '人の確認が必要な判定',"), []);
  assert.notDeepEqual(offending('en', "x: 'a spot check of a decision',"), []);
  assert.deepEqual(offending('ja', "// 判定 in a comment is fine\nx: 'AI の見立て',"), []);
});

for (const lang of ['ja', 'en']) {
  test(lang + ': the dictionary uses the glossary words', () => {
    assert.deepEqual(offending(lang, readFileSync(join(dir, lang + '.js'), 'utf8')), []);
  });
}

// The display texts of a shipped question file: every value under display.<lang> (title, question, options). A small reader for this one shape of YAML
export function displayTexts(yaml) {
  const out = { ja: [], en: [] };
  let inDisplay = false, lang = '';
  for (const line of yaml.split('\n')) {
    if (/^display:\s*$/.test(line)) { inDisplay = true; continue; }
    if (!inDisplay) continue;
    if (/^\S/.test(line)) { inDisplay = false; continue; }
    const l = line.match(/^ {2}(\w+):\s*$/);
    if (l) { lang = l[1]; continue; }
    const v = line.match(/^ {4,}(?:"[^"]*"|[\w-]+):\s*(.+)$/);
    if (v && out[lang]) out[lang].push(v[1].trim().replace(/^"(.*)"$/, '$1'));
  }
  return out;
}

// What a component shows by itself: the text between tags and static text attributes, with <script>, <style>, comments and {…} expressions removed
export function markupText(svelte) {
  const s = svelte.replace(/<script[\s\S]*?<\/script>/g, ' ').replace(/<style[\s\S]*?<\/style>/g, ' ').replace(/<!--[\s\S]*?-->/g, ' ');
  // drop {…} (quotes inside are skipped, so a } in a string does not end it)
  let out = '';
  for (let i = 0; i < s.length;) {
    if (s[i] !== '{') { out += s[i++]; continue; }
    let depth = 0, q = '';
    for (; i < s.length; i++) {
      const c = s[i];
      if (q) { if (c === '\\') i++; else if (c === q) q = ''; continue; }
      if (c === "'" || c === '"' || c === '`') q = c;
      else if (c === '{') depth++;
      else if (c === '}' && --depth === 0) { i++; break; }
    }
    out += ' ';
  }
  // tags: keep the static values of the attributes a person reads, drop the rest (a > inside a quoted class does not end the tag)
  const texts = [];
  let text = '';
  for (let i = 0; i < out.length;) {
    if (out[i] !== '<') { text += out[i++]; continue; }
    let j = i + 1, q = '';
    for (; j < out.length; j++) { const c = out[j]; if (q) { if (c === q) q = ''; continue; } if (c === '"' || c === "'") q = c; else if (c === '>') break; }
    for (const m of out.slice(i, j).matchAll(/\s(placeholder|title|aria-label|alt|label)=(["'])([^"']*)\2/g)) texts.push(m[3]);
    text += ' '; i = j + 1;
  }
  return [...text.split(/\s+/), ...texts].filter(w => w.trim());
}
// The only words a component may show by itself (everything else goes through a dictionary)
export const markupAllowed = new Set(['shikii']);
const wordless = w => !/[\p{L}]/u.test(w);
export const markupWords = svelte => markupText(svelte).filter(w => !wordless(w) && !markupAllowed.has(w));

// Japanese text literals allowed in component scripts: data the page matches against, never shown (the preface of old records in ItemCard, the Japanese scene name for "always" in Lessons)
export const scriptJapaneseAllowed = new Set(['AIによる整理（実行許可や人の正解ではない）: ', 'いつも']);
export function scriptJapanese(svelte) {
  const src = [...svelte.matchAll(/<script[^>]*>([\s\S]*?)<\/script>/g)].map(m => m[1]).join('\n').replace(/\/\*[\s\S]*?\*\//g, '');
  return literals(src).filter(l => /[\u3040-\u30ff\u3400-\u9fff]/.test(l) && !scriptJapaneseAllowed.has(l));
}

const components = [];
(function walk(d) { for (const n of readdirSync(d)) { const p = join(d, n); if (statSync(p).isDirectory()) walk(p); else if (n.endsWith('.svelte')) components.push(p); } })(join(web, 'src'));

test('the component check catches words written into markup, attributes and scripts (a broken component fails)', () => {
  assert.deepEqual(markupWords('<script>const a = "x > y";</script><p class="[&>b]:x">{t(\'k\')} ✓ <b>·</b> {#if a}→{/if}</p>'), []);
  assert.deepEqual(markupWords('<p>{t(\'k\')} 判定</p>'), ['判定']);
  assert.deepEqual(markupWords('<button title="Skip" class="[&>i]:x">{t(\'k\')}</button>'), ['Skip']);
  assert.deepEqual(markupWords('<input placeholder={t(\'k\')}><span>{"}"} band</span>'), ['band']);
  assert.deepEqual(scriptJapanese('<script>\n// 判定 in a comment\nconst x = t(\'k\'), y = \'いつも\';\n</script>'), []);
  assert.deepEqual(scriptJapanese('<script>const x = \'聞き方\';</script>'), ['聞き方']);
});

test('components show no words of their own (allowlist)', () => {
  const found = components.flatMap(f => { const s = readFileSync(f, 'utf8'); return [...markupWords(s), ...scriptJapanese(s)].map(w => relative(web, f) + ': ' + w); });
  assert.ok(components.length >= 15, 'components found: ' + components.length);
  assert.deepEqual(found, []);
});

const specFiles = readdirSync(specsDir).filter(n => n.endsWith('.yaml'));
test('the display reader reads every shipped question (a reader that reads nothing would pass everything)', () => {
  assert.ok(specFiles.length >= 5);
  for (const n of specFiles) {
    const d = displayTexts(readFileSync(join(specsDir, n), 'utf8'));
    assert.ok(d.ja.length >= 3 && d.en.length >= 3, n + ': ' + JSON.stringify(d));
  }
  assert.deepEqual(displayTexts('id: x\ndisplay:\n  ja:\n    title: 判定\n    options:\n      "yes": はい\n  en:\n    title: band\nquestion: q\n'), { ja: ['判定', 'はい'], en: ['band'] });
});

for (const lang of ['ja', 'en']) {
  test(lang + ': the shipped questions\' display texts use the glossary words', () => {
    const found = specFiles.flatMap(n => displayTexts(readFileSync(join(specsDir, n), 'utf8'))[lang].flatMap(text => glossary[lang].filter(([old]) => text.toLowerCase().includes(old.toLowerCase())).map(([old]) => n + ': ' + old + ' in "' + text.slice(0, 50) + '"')));
    assert.deepEqual(found, []);
  });
}
