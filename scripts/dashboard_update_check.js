// Exercise the actual notice renderer without requiring a browser download.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const html = fs.readFileSync('web/index.html', 'utf8');
const script = html.match(/<script[^>]*>([\s\S]*?)<\/script>/)[1];
new Function(script);
const nodes = {};
const context = {URL, Date, $: id => nodes[id] ||= {textContent: '', hidden: true, href: ''}};
vm.createContext(context);
const renderer = script.slice(script.indexOf('let updateState;'), script.indexOf('async function updateAction'));
vm.runInContext(renderer, context);
function render(d) { context.d = d; vm.runInContext('renderUpdate(d)', context); }
const latest = {version:'0.6.0-pilot', summary:'<img src=x onerror=alert(1)>', url:'https://github.com/prithivrajmu/elephant/releases/tag/v0.6.0-pilot'};
render({enabled:true,state:'available',latest});
assert.equal(nodes['update-notice'].hidden, false);
assert.ok(nodes['update-copy'].textContent.includes(latest.summary));
assert.equal(nodes['update-link'].href, latest.url);
render({enabled:true,state:'available',latest,dismissed_version:latest.version});
assert.equal(nodes['update-notice'].hidden, true);
render({enabled:false,state:'disabled',latest});
assert.equal(nodes['update-notice'].hidden, true);
assert.equal(nodes['update-toggle'].textContent, 'Enable update checks');
for (const url of ['javascript:alert(1)', 'https://github.com.evil.test/prithivrajmu/elephant/releases/tag/v0.6.0-pilot', 'https://evil.test/releases']) {
    render({enabled:true,state:'available',latest:{...latest,url}});
    assert.equal(nodes['update-notice'].hidden, true);
}
render({enabled:true,state:'unavailable'});
assert.equal(nodes['update-notice'].hidden, true);
console.log('Dashboard notice rendering, controls, dismissal and unsafe URL checks passed.');
assert.match(nodes['update-state'].textContent, /Could not verify/);
render({enabled:true,state:'unavailable',reason:'access_required',message:'Set ELEPHANT_GITHUB_TOKEN for the process running Elephant.',checked_at:'2026-10-04T12:00:00Z'});
assert.match(nodes['update-state'].textContent, /ELEPHANT_GITHUB_TOKEN/);
assert.match(nodes['update-state'].textContent, /Last attempt:/);
render({enabled:true,state:'current'});
assert.match(nodes['update-state'].textContent, /No newer compatible release/);
const action = script.slice(script.indexOf('async function updateAction'), script.indexOf("$('update-check').onclick"));
vm.runInContext(action, context);
(async () => {
    let resolve, calls=0;
    context.api = (path,body) => {calls++;assert.equal(path,'update');assert.equal(body.check,true);return new Promise(r=>resolve=r)};
    context.toast = () => {};
    const pending = vm.runInContext('updateAction({check:true})',context);
    assert.equal(nodes['update-check'].textContent,'Checking…');
    assert.equal(nodes['update-check'].disabled,true);
    assert.match(nodes['update-state'].textContent,/Checking GitHub/);
    render({enabled:true,state:'unchecked'}); // polling must not erase pending feedback
    assert.match(nodes['update-state'].textContent,/Checking GitHub/);
    await vm.runInContext('updateAction({check:true})',context);
    assert.equal(calls,1);
    resolve({enabled:true,state:'unavailable',message:'Check your repository access.'});
    await pending;
    assert.equal(nodes['update-check'].disabled,false);
    assert.equal(nodes['update-check'].textContent,'Check for updates');
    assert.equal(nodes['update-state'].textContent,'Check your repository access.');
    context.api = async()=>{throw new Error('local server disconnected')};
    await vm.runInContext('updateAction({check:true})',context);
    assert.equal(nodes['update-check'].disabled,false);
    assert.match(nodes['update-state'].textContent,/Could not complete/);
    console.log('Update check progress, duplicate clicks, cached diagnostics and failure recovery passed.');
})().catch(e=>{console.error(e);process.exitCode=1});
