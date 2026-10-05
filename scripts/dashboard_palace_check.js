// Verify graph semantics and dashboard rendering without a browser dependency.
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const html = fs.readFileSync('web/index.html', 'utf8');
const script = html.match(/<script[^>]*>([\s\S]*?)<\/script>/)[1];
new Function(script);
const elements = new Map();
const node = id => {
  if (!elements.has(id)) elements.set(id, {value:'', textContent:'', innerHTML:'', style:{}, querySelectorAll:()=>[], querySelector:()=>({classList:{toggle(){}}})});
  return elements.get(id);
};
const context = {Map, Set, Date, Math, JSON, Number, String, $:node, num:x=>Number(x||0).toLocaleString(), state:{memories:[]}};
vm.createContext(context);
// Execute the helpers from the shipped page.
vm.runInContext(script.slice(script.indexOf('const $='), script.indexOf('let toastTimer')).replace('const $=id=>document.getElementById(id);',''), context);
vm.runInContext(script.slice(script.indexOf('function memoryClass'),script.indexOf('function memoryCard')),context);
vm.runInContext(script.slice(script.indexOf('let selectedMemoryID='),script.indexOf("$('language-button').onclick")),context);
function run(code) { return vm.runInContext(code, context); }
const base = {class:'lesson',lesson:'A useful lesson',incident:'An observed event',source:'test run',scope:'personal',created:'2026-10-06T00:00:00Z',features:{language:['go']}};
context.memories = [
  {...base,id:'a',project:'one'},
  {...base,id:'b',class:'win',project:'two'},
  {...base,id:'c',class:'scar',project:'one',features:{language:['typescript']}},
  {...base,id:'d',class:'warning',project:'',features:{}},
  {...base,id:'e',project:'',features:{}}
];
// Every edge must be backed by the selected relationship; no connections from missing projects.
for (const mode of ['signals','project']) {
  context.mode=mode;
  const graph=run('buildNeuralGraph(memories,mode)');
  assert.equal(graph.nodes.length,5);
  assert.equal(graph.edges.length,1);
  const [a,b]=graph.edges[0].map(i=>graph.nodes[i].memory.id).sort();
  assert.deepEqual([a,b],mode==='signals'?['a','b']:['a','c']);
  assert.equal(JSON.stringify(graph),JSON.stringify(run('buildNeuralGraph([...memories].reverse(),mode)')));
  for (const n of graph.nodes) assert.ok(Number.isFinite(n.x)&&n.x>130&&n.x<470&&n.y>70&&n.y<395);
}
context.memory={...base,id:'unsafe',lesson:'<img src=x onerror=alert(1)> '+'unbroken_path/'.repeat(100),source:'evidence-unique',project:undefined};
assert.equal(run("mapMemoryMatches(memory,'evidence-unique','')"),true);
assert.equal(run("mapMemoryMatches({...memory,retired:true},'','')"),false);
assert.equal(run("mapMemoryMatches(memory,'','win')"),false);
node('map-links').value='signals';
context.state.memories=[context.memory];
run('renderMap()');
assert.ok(node('map-list').innerHTML.includes('&lt;img'));
assert.ok(!node('map-list').innerHTML.includes('<img'));
run("selectMemory('unsafe',true)");
assert.ok(node('map-detail').innerHTML.includes('evidence-unique'));
assert.ok(node('map-detail').innerHTML.includes('unbroken_path/'.repeat(100)));
assert.ok(!node('map-detail').innerHTML.includes('<img'));
assert.equal(run('selectedMemoryID'),'unsafe');
// Unchanged polling must preserve the DOM and active selection.
node('map-detail').innerHTML='preserved';
run('renderMap()');
assert.equal(node('map-detail').innerHTML,'preserved');
assert.equal(run('selectedMemoryID'),'unsafe');
node('map-search').value='no match';run('renderMap()');
assert.equal(run('selectedMemoryID'),'');
assert.match(node('memory-map').innerHTML,/No memories match/);
node('map-search').value='';
context.state.memories=Array.from({length:150},(_,i)=>({...base,id:'m'+i}));
run('renderMap()');
assert.equal(run('mapNodes.length'),120);
assert.equal(run('mapMatches.length'),150);
assert.match(node('index-count').textContent,/first 120 on atlas/);
assert.equal((node('map-list').innerHTML.match(/data-memory-id=/g)||[]).length,150);
run('setMapZoom(10)');assert.equal(node('memory-map').style.width,'250%');
run('setMapZoom(-10)');assert.equal(node('memory-map').style.width,'100%');
context.state.memories=[];run('renderMap()');
assert.match(node('memory-map').innerHTML,/Your memory palace starts here/);
console.log('Memory Palace: verified relationship evidence, deterministic layout, filtering, escaping, full text, selection persistence, empty states, graph limits and zoom bounds.');
