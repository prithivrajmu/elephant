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
