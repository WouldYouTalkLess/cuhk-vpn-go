// Maintainer tool: refresh pinned download URLs/hashes from upstream repositories.
// No installers, package managers, or downloaded scripts are executed.
import fs from 'node:fs/promises';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {createHash} from 'node:crypto';
import {gunzipSync} from 'node:zlib';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const cache=path.join(root,'build','source-cache');await fs.mkdir(cache,{recursive:true});
const sha=b=>createHash('sha256').update(b).digest('hex');
async function fetchBytes(url,headers={}) {
  const key=path.join(cache,sha(url));try{return await fs.readFile(key)}catch{}
  const r=await fetch(url,{headers,signal:AbortSignal.timeout(120000)});if(!r.ok)throw Error(`${r.status} ${url}`);
  const b=Buffer.from(await r.arrayBuffer());await fs.writeFile(key,b);return b;
}
async function json(url,headers={}){
  // Public registry bearer tokens are ephemeral; never persist them in the cache.
  if(url.startsWith('https://ghcr.io/token?'))await fs.rm(path.join(cache,sha(url)),{force:true});
  const r=await fetch(url,{headers,signal:AbortSignal.timeout(120000)});if(!r.ok)throw Error(`${r.status} ${url}`);return r.json();
}
function tarEntries(b){const out=[];for(let o=0;o+512<=b.length;){const h=b.subarray(o,o+512);if(h.every(x=>!x)){o+=512;continue;}const name=h.subarray(0,100).toString().split('\0')[0];const size=parseInt(h.subarray(124,136).toString().replace(/\0/g,'').trim(),8)||0;out.push({name,body:b.subarray(o+512,o+512+size)});o+=512+Math.ceil(size/512)*512;}return out;}
const sources=new Map();
async function asset(name,url,format,headers={}) {const b=await fetchBytes(url,headers);const a={name,url,sha256:sha(b),format};sources.set(name,a);return a;}
const plans={};
for(const [arch,flavour,hash] of [['amd64','64','6ee9e8eb9bc59ef70bb0717df7f99703f8a2ccd11d8e45d58a61f9a2e6ef7d00'],['386','32','1fbf9cfe1ae732427348d2321a107c912fcc6049d25c2ebbb2fa91763f225b15']]){
  const url=`https://gitlab.com/openconnect/openconnect/-/jobs/artifacts/v9.21/raw/openconnect-installer-MinGW${flavour}-GnuTLS.exe?job=MinGW${flavour}%2FGnuTLS`;
  const core=await asset(`windows-${arch}-openconnect`,url,'nsis');if(core.sha256!==hash)throw Error('OpenConnect upstream digest changed');
  const unpack=await asset(`windows-${arch}-7zip`,`https://github.com/ip7z/7zip/releases/download/26.04/7z2604${arch==='386'?'': '-x64'}.exe`,'7zip-sfx');
  plans[`windows/${arch}`]={version:'9.21',sources:[unpack,core]};
}
// OpenConnect upstream provides no native ARM64 Windows build. Explicitly declare
// the x64 emulation dependency instead of claiming an ARM64 core exists.
plans['windows/arm64']={...plans['windows/amd64'],emulatedArch:'amd64'};

// Obtain vpnc-script from an immutable upstream commit, not mutable master.
const commit=await json('https://gitlab.com/api/v4/projects/openconnect%2Fvpnc-scripts/repository/commits?per_page=1');
const script=await asset('vpnc-script',`https://gitlab.com/openconnect/vpnc-scripts/-/raw/${commit[0].id}/vpnc-script`,'file');

for(const [arch,tag] of [['amd64','sonoma'],['arm64','arm64_sonoma']]){
  const seen=new Set(),list=[];
  async function visit(name,version){if(seen.has(name))return;seen.add(name);
    if(!version){const f=await json(`https://formulae.brew.sh/api/formula/${name}.json`);version=f.versions.stable+(f.revision?'_'+f.revision:'');}
    const repo=name.replaceAll('@','/');const token=await json(`https://ghcr.io/token?service=ghcr.io&scope=repository:homebrew/core/${repo}:pull`);
    const headers={'Authorization':`Bearer ${token.token}`,Accept:'application/vnd.oci.image.index.v1+json'};
    let index;try{index=await json(`https://ghcr.io/v2/homebrew/core/${repo}/manifests/${version}`,headers);}
    catch(e){const f=await json(`https://formulae.brew.sh/api/formula/${name}.json`);version=f.versions.stable+(f.revision?'_'+f.revision:'');index=await json(`https://ghcr.io/v2/homebrew/core/${repo}/manifests/${version}`,headers);}
    const m=index.manifests?.find(x=>x.annotations?.['org.opencontainers.image.ref.name']===`${version}.${tag}`)||index.manifests?.find(x=>x.annotations?.['org.opencontainers.image.ref.name']===`${version}.all`);
    if(!m)throw Error(`${name}@${version} has no ${tag} bottle`);
    const tab=JSON.parse(m.annotations['sh.brew.tab']);
    for(const dep of tab.runtime_dependencies||[])await visit(dep.full_name,dep.pkg_version||dep.version+(dep.revision?'_'+dep.revision:''));
    const hash=m.annotations['sh.brew.bottle.digest'];const url=`https://ghcr.io/v2/homebrew/core/${repo}/blobs/sha256:${hash}`;
    const a=await asset(`macos-${arch}-${name}`,url,'bottle',headers);if(a.sha256!==hash)throw Error(`Homebrew SHA mismatch: ${name}`);list.push(a);console.log(`Locked ${arch} ${name}@${version}`);
  }
  await visit('openconnect');await visit('ca-certificates');
  plans[`darwin/${arch}`]={version:'9.21',minOS:'14.0',sources:[...list,script]};console.log(`macOS ${arch}: ${list.length} bottles locked`);
}

for(const [arch,alpineArch] of [['amd64','x86_64'],['arm64','aarch64']]){
  const all=new Map(),providers=new Map(),base='https://dl-cdn.alpinelinux.org/alpine/v3.24';
  for(const repo of ['main','community']){
    const bytes=gunzipSync(await fetchBytes(`${base}/${repo}/${alpineArch}/APKINDEX.tar.gz`));const index=tarEntries(bytes).find(x=>x.name==='APKINDEX');if(!index)throw Error('APKINDEX missing');
    for(const block of index.body.toString().trim().split('\n\n')){const p={repo};for(const line of block.split('\n'))p[line.slice(0,1)]=line.slice(2);all.set(p.P,p);}
  }
  const nameOf=d=>d.replace(/[<>=~].*$/,'');
  for(const p of all.values()){providers.set(p.P,p);for(const alias of (p.p||'').split(' ').filter(Boolean))providers.set(nameOf(alias),p);}
  const seen=new Set(),list=[];
  async function visit(dep){if(dep.startsWith('!'))return;const name=nameOf(dep);const p=providers.get(name);if(!p)throw Error(`Missing Alpine dependency ${dep}`);if(seen.has(p.P))return;seen.add(p.P);
    for(const d of (p.D||'').split(' ').filter(Boolean))await visit(d);
    list.push(await asset(`linux-${arch}-${p.P}`,`${base}/${p.repo}/${alpineArch}/${p.P}-${p.V}.apk`,'apk'));
  }
  await visit('openconnect');await visit('ca-certificates-bundle');await visit('iproute2-minimal');
  plans[`linux/${arch}`]={version:all.get('openconnect').V,sources:[...list,script]};console.log(`Linux ${arch}: ${list.length} APK payloads locked`);
}
await fs.writeFile(path.join(root,'internal','core','sources.json'),JSON.stringify({schema:1,plans},null,2)+'\n');
console.log('Wrote internal/core/sources.json');
