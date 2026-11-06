import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {createRequire} from 'node:module';
import {createECDH,createHash} from 'node:crypto';
import tls from 'node:tls';
import https from 'node:https';

assert.equal(process.version,'v24.22.2');
assert.equal(process.platform,'linux');
assert.equal(process.report.getReport().header.glibcVersionRuntime,undefined);
assert.ok(fs.readdirSync('/lib').some(name=>name.startsWith('ld-musl-')));
for(const name of ['/lib/libc.so.6','/lib64/ld-linux-x86-64.so.2','/bin/sh','/sbin/apk','/usr/bin/g++'])assert.equal(fs.existsSync(name),false,name);
assert.ok(tls.rootCertificates.length>0);
assert.ok(fs.statSync('/etc/ssl/cert.pem').size>0);
assert.equal(Intl.DateTimeFormat.supportedLocalesOf(['sq','en-US','ja']).length,3);
const policy=JSON.parse(fs.readFileSync('/services/security/node-backports-policy.json','utf8'));
const nodeBackports=JSON.parse(fs.readFileSync('/usr/share/zing/node-backports.json','utf8'));
assert.equal(nodeBackports.nodeSourceSha256,policy.nodeSourceSha256);
assert.deepEqual(nodeBackports.headers,policy.headers);
for(const [mode,result] of [['baseline','reproduced'],['patched','passed']]){
  assert.deepEqual(nodeBackports.probes[mode],Object.fromEntries(Object.keys(policy.headers).map(name=>[name,result])));
}
assert.ok(nodeBackports.v8BooleanHeaderDependencies>0);
const sha256=file=>createHash('sha256').update(fs.readFileSync(file)).digest('hex');
assert.equal(nodeBackports.nodeSha256,sha256(process.execPath));
assert.equal(nodeBackports.libstdcSha256,sha256('/usr/lib/libstdc++.so.6'));
const addonPaths=[...new Set(fs.readdirSync('/services',{recursive:true}).filter(name=>name.endsWith('.node')).map(name=>fs.realpathSync(path.join('/services',name))))].sort();
assert.deepEqual(nodeBackports.nativeAddons,Object.fromEntries(addonPaths.map(file=>[path.relative('/services',file),sha256(file)])));
const alice=createECDH('secp256k1'),bob=createECDH('secp256k1');alice.generateKeys();bob.generateKeys();
assert.deepEqual(alice.computeSecret(bob.getPublicKey()),bob.computeSecret(alice.getPublicKey()));
assert.equal(createHash('sha256').update('fixture').digest('hex').length,64);
const require=createRequire(path.join(process.cwd(),'package.json'));
let sqlite=false;
if(process.argv.includes('--pds')){
  const pds=createRequire(require.resolve('@atproto/pds'));
  const Database=pds('better-sqlite3');const db=new Database(':memory:');
  db.exec('CREATE TABLE fixture(value TEXT NOT NULL)');db.prepare('INSERT INTO fixture(value) VALUES(?)').run('Zing — ✓');
  assert.equal(db.prepare('SELECT value FROM fixture').get().value,'Zing — ✓');db.close();sqlite=true;
  const addon=path.join(path.dirname(pds.resolve('better-sqlite3/package.json')),'build/Release/better_sqlite3.node');
  assert.equal(nodeBackports.sqliteAddonSha256,sha256(addon));
}
let localTLS=false;
if(process.argv.includes('--tls')){
  const cert=fs.readFileSync('/fixture/cert.pem'),key=fs.readFileSync('/fixture/key.pem');
  const server=https.createServer({cert,key},(_req,res)=>res.end('verified runtime'));
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const port=server.address().port;
  try{
    const response=await new Promise((resolve,reject)=>https.get({hostname:'127.0.0.1',port,ca:cert},res=>{let data='';res.on('data',part=>data+=part);res.on('end',()=>resolve({status:res.statusCode,data}));}).on('error',reject));
    assert.equal(response.status,200);assert.equal(response.data,'verified runtime');
    const untrusted=await new Promise(resolve=>https.get({hostname:'127.0.0.1',port},res=>{res.resume();resolve('accepted');}).on('error',error=>resolve(error.code)));
    assert.match(untrusted,/SELF_SIGNED/);localTLS=true;
  }finally{await new Promise(resolve=>server.close(resolve));}
}
console.log(JSON.stringify({status:'ok',node:process.version,libc:'musl',glibcAbsent:true,shellAndBuildToolsAbsent:true,ellipticCurveCrypto:true,sqlite,localTLS,nodeBackports:{...nodeBackports,runtimeHashesVerified:true}}));
