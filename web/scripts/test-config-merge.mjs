import { createRequire } from 'node:module';
import assert from 'node:assert/strict';
import test from 'node:test';

// Resolve the merger from its actual consumer, not a separate test dependency.
const requirePlugin = createRequire(import.meta.resolve('@sveltejs/vite-plugin-svelte'));
const merge = requirePlugin('deepmerge');

test('configuration merging never inherits attacker-controlled properties', () => {
  const poisoned = JSON.parse('{"__proto__":{"unexpectedConfigValue":true}}');
  for (const result of [
    merge(poisoned, {}),
    merge(Object.create(null), poisoned),
    merge({ nested: poisoned }, {}),
    merge({}, { nested: poisoned }),
    merge({ values: [poisoned] }, {}),
  ]) {
    const object = result.nested ?? result.values?.[0] ?? result;
    assert.equal(Object.getPrototypeOf(object), Object.prototype);
    assert.equal(object.unexpectedConfigValue, undefined);
    assert.equal(Object.hasOwn(object, '__proto__'), false);
  }
  assert.equal({}.unexpectedConfigValue, undefined);
});

test('configuration merging preserves nested options and plugin array replacement', () => {
  const preprocess = () => {};
  const date = new Date(0);
  const regexp = /svelte/;
  const key = Symbol('option');
  const target = { compilerOptions: { dev: true, nested: { first: 1 } }, preprocess: [preprocess], date, regexp, [key]: 'kept' };
  const source = { compilerOptions: { nested: { second: 2 } }, preprocess: ['replacement'] };
  const merged = merge(target, source, { arrayMerge: (_target, value) => value });
  assert.deepEqual(merged.compilerOptions, { dev: true, nested: { first: 1, second: 2 } });
  assert.deepEqual(merged.preprocess, ['replacement']);
  assert.equal(merged.date, date);
  assert.equal(merged.regexp, regexp);
  assert.equal(merged[key], 'kept');
  assert.deepEqual(target.preprocess, [preprocess]);
  assert.deepEqual(merge([1], [2]), [1, 2]);
  assert.deepEqual(merge.all([{ one: 1 }, { two: 2 }]), { one: 1, two: 2 });
});
