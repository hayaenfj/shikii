// The glossary of the page (0.9.2 redesign P4; npm run check). Words for the AI's insides were replaced with plain words a person who is not an engineer reads.
// This check reads every text literal in the dictionaries (src/locales/*.js, comments removed) and fails if an old word comes back.
// The table is the glossary itself (old word → the word to use); adding a row both documents the change and keeps it.
import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const dir = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'locales');

export const glossary = {
  ja: [
    ['判定', 'AI の見立て / 記録 / 問い'],
    ['答え合わせ', 'あなたの答え / 正解'],
    ['線を引', 'AI がひとりで決めてよい境目'],
    ['線が引', 'AI がひとりで決めてよい境目'],
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
