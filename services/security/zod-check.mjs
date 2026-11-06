import assert from 'node:assert/strict';
// The upstream fix rejects upper-bound failures before element parsers run.
// Check synchronous/async paths while preserving the v3 refinement interface.
export async function checkZodBounds(z) {
  let calls=0;
  const element=z.string().transform(value=>{calls++;return value;});
  const huge=Array(100000).fill('valid element');
  for(const schema of [z.array(element).max(2),z.array(element).length(2)]){
    calls=0;const parsed=schema.safeParse(huge);
    assert.equal(parsed.success,false);assert.equal(parsed.error.issues[0].code,'too_big');assert.equal(calls,0);
    const asyncParsed=await schema.safeParseAsync(huge);
    assert.equal(asyncParsed.success,false);assert.equal(calls,0);
    assert.deepEqual(schema.parse(['a','b']),['a','b']);assert.equal(calls,2);
  }
  calls=0;const nested=z.object({items:z.array(element).max(2)}).safeParse({items:huge});
  assert.equal(nested.success,false);assert.deepEqual(nested.error.issues[0].path,['items']);assert.equal(calls,0);
  for(const parse of ['safeParse','safeParseAsync']){
    calls=0;const result=await z.set(element).max(2)[parse](new Set(['a','b','c']));
    assert.equal(result.success,false);assert.equal(result.error.issues[0].code,'too_big');assert.equal(calls,0);
  }
  assert.equal(z.object({ok:z.boolean()}).refinement(value=>value.ok).safeParse({ok:true}).success,true);
}
