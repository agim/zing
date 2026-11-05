// Exercises the actual resolved dependencies, including the backported paths.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import fs from 'node:fs';
import { createHash } from 'node:crypto';
const require = createRequire(import.meta.url);
const pdsRequire = createRequire(require.resolve('@atproto/pds'));
const { z } = pdsRequire('zod');
for (const schema of [z.array(z.string()), z.array(z.object({ children: z.array(z.number()) }))]) {
  const input = Array(10000).fill({ children: ['invalid', 'invalid'] });
  const result = schema.safeParse(input);
  assert.equal(result.success, false);
  assert.ok(result.error.issues.length > 0 && result.error.issues.length <= 64, 'unbounded validation issues');
}
assert.equal(z.array(z.string()).safeParse(['valid']).success, true);
const mailRequire = createRequire(pdsRequire.resolve('nodemailer-html-to-text'));
const textRequire = createRequire(mailRequire.resolve('html-to-text'));
const merge = textRequire('deepmerge');
const bad = JSON.parse('{"__proto__":{"polluted":true},"constructor":{"prototype":{"polluted":true}},"safe":{"value":1}}');
const nullObject = Object.assign(Object.create(null), bad);
for (const input of [bad, nullObject, { nested: bad }]) {
  const result = merge({}, input);
  assert.equal({}.polluted, undefined);
  assert.equal(result.polluted, undefined);
  assert.equal(Object.getPrototypeOf(result), Object.prototype);
  assert.equal(Object.hasOwn(result, '__proto__'), false);
  assert.equal(Object.hasOwn(result, 'constructor'), false);
  if(result.nested) {assert.equal(Object.hasOwn(result.nested,'__proto__'),false);assert.equal(Object.hasOwn(result.nested,'constructor'),false);}
}
assert.deepEqual(merge.all([{a:1}, {b:2}]), {a:1,b:2});
assert.deepEqual(merge({a:[1]}, {a:[2]}, {arrayMerge:(_old,next)=>next}), {a:[2]});
const patches = Object.fromEntries(['zod@3.25.76.patch','deepmerge@4.3.1.patch'].map(name => {
  const data = fs.readFileSync(new URL(`patches/${name}`, import.meta.url));
  return [name, createHash('sha256').update(data).digest('hex')];
}));
await import('@atproto/pds');
console.log(JSON.stringify({status:'ok', checks:['bounded Zod validation errors','prototype isolation','merge compatibility','PDS import'], patches}));
