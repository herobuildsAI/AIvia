const {test} = require('node:test');
const assert = require('node:assert/strict');
const {readFileSync} = require('node:fs');
const {createContext, runInContext} = require('node:vm');

// Exercise real async UI functions with a controlled HTTP boundary. Browser
// acceptance covers document bootstrap, native controls, and rendering.
function harness() {
  const nodes = new Map();
  const requests = [], copied = [];
  const node = () => ({checked:false, disabled:false, open:false, textContent:'', value:'', children:[], handlers:{}, replaceChildren(...items){this.children=items;}, append(...items){this.children.push(...items);}, get firstChild(){return this.children[0];}, setAttribute(){}, addEventListener(event,fn){this.handlers[event]=fn;}, scrollIntoView(){}, focus(){}, showModal(){this.open=true;}, close(){this.open=false;}, querySelector(){return null;}});
  for(const match of readFileSync('internal/app/web/index.html','utf8').matchAll(/id="([^"]+)"/g)) nodes.set(match[1],node());
  for (const id of ['cloud-confirmed','cli-path','ip-api-key','ip-provider','assistant-mode','triage-stage','triage-error']) if (!nodes.has(id)) nodes.set(id,node());
  const navigator = {languages:["en-US"], clipboard:{writeText:async text => copied.push(text)}};
  const context = createContext({
    AbortController, structuredClone, setTimeout, clearTimeout, setInterval, clearInterval, navigator,
    window:{addEventListener(){}}, location:{hash:'#diagnostics'}, history:{replaceState(){}}, Option: function(text,value){this.text=text;this.value=value;},
    document: {createElement:node, createTextNode:text=>({textContent:text}), querySelectorAll:()=>[], getElementById(id) { if (!nodes.has(id)) nodes.set(id, node()); return nodes.get(id); }},
    fetch(url, options) { return new Promise((resolve,reject) => requests.push({url, options, reject, resolve: data => resolve({ok:true,json:async()=>data})})); },
  });
  const source = readFileSync('internal/app/web/app.js', 'utf8').replace(/^perform\(init\);\s*$/m, '');
  runInContext(source + '\nglobalThis.hooks = {state, sendChat, resetChat, renderReport, reportGuidance:typeof reportGuidance === "function" ? reportGuidance : null, openResearch:typeof openResearch === "function" ? openResearch : null, runResearch:typeof runResearch === "function" ? runResearch : null, cancelResearch:typeof cancelResearch === "function" ? cancelResearch : null, compareReports:typeof compareReports === "function" ? compareReports : null, updateExport, runChecks, cancelRun, bindEvents, saveSettings, previewContext, copyContext, saveIPSettings, renderSelectors, copyExport:typeof copyExport === "function" ? copyExport : null, setProfileExport(){exportKind="profile";}};', context);
  context.hooks.state.selected = 'profile-a'; context.hooks.setProfileExport();
  runInContext('Object.assign(hooks,{editProfile,saveProfile,editIssue,saveIssue,updatePolicyForm})',context);
  runInContext('Object.assign(hooks,{previewExport,openTemplateImport:typeof openTemplateImport === "function" ? openTemplateImport : null,previewTemplate:typeof previewTemplate === "function" ? previewTemplate : null,cancelTemplateImport:typeof cancelTemplateImport === "function" ? cancelTemplateImport : null})',context);
  return {hooks:context.hooks, nodes, requests, copied, navigator};
}

function diagnosticFixture() {
  const finding = (id,weight,lower,upper) => ({id,title:id,weight,lower,upper,state:lower===upper?'observed':'unknown',explanation:'Evidence explanation',recommendation:'Review this signal',source:'Fixture'});
  return {id:'report-a',profileId:'profile-a',profileRevision:1,createdAt:'2026-10-02T12:00:00Z',scoreVersion:'1.0',lower:0,upper:97,coverage:3,grade:'Insufficient evidence to classify',policy:{mode:'unknown'},target:{state:'unknown',summary:'Not checked'},findings:[finding('timezone-system',3,0,0),finding('vpn',2,0,2),finding('region',40,0,40),finding('abuse',15,0,15),finding('egress',10,0,10)]};
}
function flattened(node) { return [node,...(node.children||[]).flatMap(flattened)]; }
function mountReport(hooks,r) { hooks.state.profiles=[{id:'profile-a',revision:1,policy:{mode:'unknown'}}]; hooks.state.reports=[r]; hooks.state.reportId=r.id; }

const sampleTemplate = {format:'aivia-service-template',version:1,name:'Example',kind:'website',origin:'https://example.com',policy:{mode:'unknown'}};
const templateDraft = {name:'Example',kind:'website',origin:'https://example.com',status:'unchecked',notes:'',policy:{mode:'unknown'},issues:[]};

const savedProfile = {id:'profile-a',revision:1,name:'Example',kind:'website',origin:'',status:'unchecked',notes:'',policy:{mode:'unknown'},issues:[],createdAt:'2026-10-02T12:00:00Z',updatedAt:'2026-10-02T12:00:00Z'};
async function finishEditorRefresh(requests, profiles = [savedProfile]) {
  await new Promise(setImmediate);
  for (const request of requests) {
    if (request.url === '/api/state') request.resolve({profiles,reports:[],settings:{provider:'ollama',endpoint:'http://127.0.0.1:11434',model:'',localConfirmed:false}});
    if (request.url === '/api/ip-settings') request.resolve({revision:0,ready:false});
  }
}

test('a new service cannot be submitted twice while its first save is pending', async () => {
  const {hooks,nodes,requests}=harness();
  hooks.editProfile(); nodes.get('profile-name').value='Example';
  const pending=hooks.saveProfile({preventDefault(){}});
  const repeated=hooks.saveProfile({preventDefault(){}});
  assert.equal(requests.length,1);
  assert.equal(nodes.get('profile-name').disabled,true);
  requests[0].resolve(savedProfile); await finishEditorRefresh(requests); await pending; await repeated;
  assert.equal(nodes.get('profile-name').disabled,false);
});

test('template import waits for a pending save before opening its profile-editor workflow', async () => {
  const {hooks,nodes,requests}=harness(); hooks.bindEvents();
  hooks.editProfile(); nodes.get('profile-name').value='Example';
  const pending=hooks.saveProfile({preventDefault(){}});
  nodes.get('profile-dialog').oncancel({preventDefault(){}});
  hooks.openTemplateImport();
  assert.equal(nodes.get('template-dialog').open,false);
  assert.match(nodes.get('notice').textContent,/wait/i);
  requests[0].reject(new Error('Save failed')); await pending;
  hooks.openTemplateImport();
  assert.equal(nodes.get('template-dialog').open,true);
});

test('a completed service save cannot replace a selection made while its refresh is pending', async () => {
  const {hooks,nodes,requests}=harness();
  const other={...savedProfile,id:'profile-b',name:'Other'};
  hooks.state.profiles=[savedProfile,other];
  hooks.editProfile(); nodes.get('profile-name').value='New service';
  const pending=hooks.saveProfile({preventDefault(){}});
  const created={...savedProfile,id:'profile-c',name:'New service'};
  requests[0].resolve(created); await new Promise(setImmediate);
  hooks.state.selected='profile-b';
  requests.find(r=>r.url==='/api/state').resolve({profiles:[savedProfile,other,created],reports:[],settings:{}});
  requests.find(r=>r.url==='/api/ip-settings').resolve({revision:0,ready:false});
  await pending;
  assert.equal(hooks.state.selected,'profile-b');
});

for (const first of ['profile','issue']) {
  for (const next of ['profile','issue']) {
    test(`${next} editor waits for ${first} save and refresh, then saves the full fresh revision without retry`, async () => {
      const {hooks,nodes,requests}=harness(); hooks.bindEvents();
      const issue={id:'issue-first',stage:'access',error:'First issue',notes:'Keep this note',resolved:false,observedAt:'2026-10-02T12:00:00Z'};
      const original={...savedProfile,notes:'Service note',issues:[issue]};
      const fresh={...original,revision:2,notes:first==='profile'?'Saved service note':'Service note',issues:[first==='issue'?{...issue,error:'Saved first issue'}:issue]};
      hooks.state.profiles=[original];
      if (first==='profile') { hooks.editProfile(original); nodes.get('profile-notes').value=fresh.notes; }
      else { hooks.editIssue(issue); nodes.get('issue-error').value='Saved first issue'; }
      const pending=hooks[first==='profile'?'saveProfile':'saveIssue']({preventDefault(){}});
      nodes.get(`${first}-dialog`).oncancel({preventDefault(){}});
      const openNext=()=>next==='profile'?hooks.editProfile(hooks.state.profiles[0]):hooks.editIssue();
      openNext();
      assert.equal(nodes.get(`${next}-dialog`).open,false,'must not capture revision 1 while the mutation is pending');
      assert.match(nodes.get('notice').textContent,/wait/i);
      requests[0].resolve(fresh); await new Promise(setImmediate);
      openNext();
      assert.equal(nodes.get(`${next}-dialog`).open,false,'must also wait for the state refresh');
      await finishEditorRefresh(requests,[fresh]); await pending;
      openNext(); assert.equal(nodes.get(`${next}-dialog`).open,true);
      if (next==='issue') nodes.get('issue-error').value='Second issue';
      const second=hooks[next==='profile'?'saveProfile':'saveIssue']({preventDefault(){}});
      const writes=requests.filter(r=>r.url==='/api/profiles');
      assert.equal(writes.length,2,'one request per save, no conflict retry');
      const payload=JSON.parse(writes[1].options.body);
      assert.equal(payload.revision,2);
      assert.equal(payload.notes,fresh.notes);
      assert.deepEqual(payload.issues[0],fresh.issues[0]);
      if (next==='issue') assert.equal(payload.issues[1].error,'Second issue');
      const saved={...payload,revision:3};
      writes[1].resolve(saved); await finishEditorRefresh(requests,[saved]); await second;
      assert.equal(nodes.get(`${next}-dialog`).open,false);
      assert.equal(hooks.state.profiles[0].revision,3);
    });
  }
}

for (const kind of ['profile','issue']) {
  const edit=kind==='profile'?'editProfile':'editIssue', save=kind==='profile'?'saveProfile':'saveIssue';
  const field=kind==='profile'?'profile-name':'issue-error', error=kind==='profile'?'profile-error':'issue-validation';

  test(`a failed closed ${kind} save reports its error outside the editor and permits a fresh draft`, async () => {
    const {hooks,nodes,requests}=harness(); hooks.bindEvents(); hooks.state.profiles=[savedProfile];
    hooks[edit](); nodes.get(field).value='First draft';
    const pending=hooks[save]({preventDefault(){}});
    const dialog=nodes.get(`${kind}-dialog`);
    dialog.oncancel?.({preventDefault(){}}); dialog.close(); hooks[edit]();
    assert.equal(dialog.open,false,'opening must wait until the earlier save finishes');
    requests[0].reject(new Error('Earlier save failed')); await pending;
    assert.equal(nodes.get(error).textContent,'');
    assert.match(nodes.get('notice').textContent,/Earlier save failed/);
    hooks[edit](); nodes.get(field).value='New unsaved draft';
    assert.equal(dialog.open,true);
    assert.equal(nodes.get(field).disabled,false);
    assert.equal(nodes.get(error).textContent,'');
  });

  test(`closing a pending ${kind} save keeps the request running and a failure releases its guard`, async () => {
    const {hooks,nodes,requests}=harness(); hooks.bindEvents(); hooks.state.profiles=[savedProfile];
    hooks[edit](); nodes.get(field).value='Draft';
    const pending=hooks[save]({preventDefault(){}});
    assert.equal(nodes.get(field).disabled,true);
    const dialog=nodes.get(`${kind}-dialog`);
    dialog.oncancel?.({preventDefault(){}}); dialog.close();
    assert.match(nodes.get('notice').textContent,/save.*(continue|progress)/i);
    assert.equal(requests[0].options.signal,undefined,'closing must not pretend the server mutation was canceled');
    requests[0].reject(new Error('Save failed')); await pending;
    hooks[edit](); nodes.get(field).value='Retry';
    const retry=hooks[save]({preventDefault(){}});
    assert.equal(requests.length,2);
    requests[1].reject(new Error('Retry failed')); await retry;
    assert.equal(nodes.get(field).disabled,false);
    assert.match(nodes.get(error).textContent,/Retry failed/);
  });
}

test('inactive policy controls are disabled so irrelevant invalid values cannot block submission', () => {
  const {hooks,nodes}=harness(); hooks.editProfile();
  nodes.get('policy-mode').value='custom'; hooks.updatePolicyForm();
  nodes.get('policy-source').value='not a URL';
  nodes.get('policy-mode').value='unknown'; hooks.updatePolicyForm();
  for (const id of ['policy-countries','policy-source','policy-date','policy-regions']) assert.equal(nodes.get(id).disabled,true,id);
  nodes.get('policy-mode').value='worldwide'; hooks.updatePolicyForm();
  assert.equal(nodes.get('policy-countries').disabled,true);
  assert.equal(nodes.get('policy-source').disabled,false);
  nodes.get('policy-mode').value='custom'; hooks.updatePolicyForm();
  assert.equal(nodes.get('policy-countries').disabled,false);
});

test('a stale export response cannot open a dialog after a service switch', async () => {
  const {hooks,nodes,requests}=harness();
  const pending=hooks.previewExport('profile');
  hooks.state.selected='profile-b';
  requests[0].resolve({profile:{id:'profile-a'}}); await pending;
  assert.equal(nodes.get('export-dialog').open,false);
  assert.equal(nodes.get('copy-export').disabled,true);
});
test('template import only previews a new service and does not overwrite or save the current service', async () => {
  const {hooks,nodes,requests}=harness(); hooks.state.profiles=[{id:'profile-a',name:'Private service',notes:'PRIVATE'}];
  hooks.openTemplateImport(); nodes.get('template-json').value=JSON.stringify(sampleTemplate);
  const pending=hooks.previewTemplate();
  assert.equal(requests[0].url,'/api/templates/preview');
  requests[0].resolve(templateDraft); await pending;
  assert.equal(nodes.get('profile-title').textContent,'Review imported service',nodes.get('template-error').textContent);
  assert.equal(nodes.get('profile-name').value,'Example');
  assert.equal(nodes.get('profile-notes').value,'');
  assert.equal(nodes.get('template-review-note').hidden,false);
  assert.equal(hooks.state.profiles.length,1); assert.equal(hooks.state.profiles[0].notes,'PRIVATE');
  assert.equal(requests.length,1);
});
test('closing or editing a pending template cannot reopen its stale preview', async () => {
  for(const cancel of ['close','edit']) {
    const {hooks,nodes,requests}=harness(); hooks.bindEvents(); hooks.openTemplateImport();
    nodes.get('profile-name').value='Unsaved draft'; nodes.get('template-json').value=JSON.stringify(sampleTemplate);
    const pending=hooks.previewTemplate();
    if(cancel==='close') hooks.cancelTemplateImport(true); else { nodes.get('template-json').value='new draft'; nodes.get('template-json').oninput(); }
    requests[0].resolve(templateDraft); await pending;
    assert.equal(nodes.get('profile-name').value,'Unsaved draft');
    assert.equal(nodes.get('template-submit').disabled,false);
  }
});
test('template export uses its own projection even when profile notes were previously selected', async () => {
  const {hooks,nodes,requests,copied}=harness(); nodes.get('export-notes').checked=true;
  const pending=hooks.previewExport('template');
  assert.equal(requests[0].url,'/api/profiles/profile-a/template');
  assert.equal(nodes.get('export-notes-label').hidden,true);
  requests[0].resolve(sampleTemplate); await pending; await hooks.copyExport();
  assert.deepEqual(JSON.parse(copied[0]),sampleTemplate);
});
test('malformed template JSON remains local and keeps the import form usable', async () => {
  const {hooks,nodes,requests}=harness(); hooks.openTemplateImport(); nodes.get('template-json').value='{broken';
  await hooks.previewTemplate();
  assert.match(nodes.get('template-error').textContent,/valid JSON/);
  assert.equal(requests.length,0); assert.equal(nodes.get('template-submit').disabled,false);
});

test('guidance retains partial adverse evidence and prioritizes it before unknowns without mutating reports', () => {
  const {hooks}=harness(), r=diagnosticFixture();
  r.findings.find(f=>f.id==='egress').lower=5;
  const original=JSON.stringify(r), g=hooks.reportGuidance(r);
  assert.deepEqual(Array.from(g.signals,f=>f.id),['egress']);
  assert.deepEqual(Array.from(g.unknown,f=>f.id),['region','abuse','vpn']);
  assert.deepEqual(Array.from(g.clear,f=>f.id),['timezone-system']);
  assert.equal(g.next[0].finding.id,'egress');
  assert.equal(g.next.filter(s=>s.key==='ip').length,1);
  assert.equal(g.next.length,3);
  assert.equal(JSON.stringify(r),original);
});
test('offline report explains the unresolved range, groups evidence and never starts requests', () => {
  const {hooks,nodes,requests}=harness(); mountReport(hooks,diagnosticFixture()); hooks.renderReport();
  const text=flattened(nodes.get('report-output')).map(n=>n.textContent).join('\n');
  assert.match(text,/97 additional points remain unresolved/);
  assert.match(text,/not a confidence percentage/);
  assert.match(text,/Still unknown/);
  assert.match(text,/No adverse signal in these checks/);
  assert.match(text,/Next checks/);
  assert.match(text,/zero lower bound does not establish/);
  assert.equal(requests.length,0);
});
test('resolved zero score is not called safe and reachability cannot become region evidence', () => {
  const {hooks,nodes}=harness(), r=diagnosticFixture();
  r.findings=r.findings.slice(0,1); r.lower=r.upper=0; r.coverage=100;
  r.target={state:'observed',status:403,summary:'Service returned HTTP 403.'};
  assert.equal(hooks.reportGuidance(r).next.length,0);
  mountReport(hooks,r); hooks.renderReport();
  const text=flattened(nodes.get('report-output')).map(n=>n.textContent).join('\n');
  assert.match(text,/does not establish account eligibility/);
  assert.match(text,/403 alone does not prove a region restriction/);
  assert.doesNotMatch(text,/\bSafe\b|\bAll clear\b/);
});
test('guidance settings shortcut does not enable checks, save settings or call an assistant', async () => {
  const {hooks,nodes,requests}=harness(); mountReport(hooks,diagnosticFixture()); hooks.renderReport();
  const button=flattened(nodes.get('report-output')).find(n=>n.textContent==='Review IP settings');
  assert.ok(button); await button.handlers.click();
  assert.equal(hooks.state.view,'settings');
  assert.equal(nodes.get('intelligence-check').checked,false);
  assert.equal(requests.length,0);
});
test('guidance source shortcut only loads local suggestions and waits for explicit fetching', async () => {
  const {hooks,nodes,requests}=harness(); mountReport(hooks,diagnosticFixture()); hooks.renderReport();
  const button=flattened(nodes.get('report-output')).find(n=>n.textContent==='Review official sources');
  button.handlers.click();
  assert.equal(requests.length,1);
  assert.equal(requests[0].url,'/api/profiles/profile-a/research-sources');
  assert.equal(requests[0].options.method,'GET');
  requests[0].resolve({profileId:'profile-a',revision:1,urls:['https://example.com/policy']});
  await new Promise(setImmediate);
  assert.equal(requests.length,1);
  assert.match(nodes.get('research-status').textContent,/No source has been contacted yet/);
});
test('older notes-included response cannot replace a newer redacted export', async () => {
  const {hooks,nodes,requests} = harness();
  nodes.set('export-notes',{checked:true});
  const first = hooks.updateExport();
  nodes.get('export-notes').checked=false;
  const second=hooks.updateExport();
  requests[1].resolve({notes:''}); await second;
  requests[0].resolve({notes:'PRIVATE-NOTES'}); await first;
  assert.equal(nodes.get('export-preview').textContent, JSON.stringify({notes:''},null,2));
  assert.equal(nodes.get('download-export').disabled,false);
});
test('replacement export keeps downloading disabled until it resolves', async () => {
  const {hooks,nodes,requests} = harness();
  const first=hooks.updateExport(); requests[0].resolve({notes:''}); await first;
  const pending=hooks.updateExport();
  assert.equal(nodes.get('download-export').disabled,true);
  requests[1].resolve({notes:'fresh'}); await pending;
  assert.equal(nodes.get('download-export').disabled,false);
});

test('a service switch invalidates an in-flight export', async () => {
  const {hooks,nodes,requests} = harness();
  const pending=hooks.updateExport();
  hooks.state.selected='profile-b';
  requests[0].resolve({notes:'SERVICE-A'}); await pending;
  assert.equal(nodes.get('download-export').disabled,true);
  assert.ok(!nodes.get('export-preview').textContent.includes('SERVICE-A'));
});
test('a canceled diagnostic cannot replace the active report', async () => {
  const {hooks,nodes,requests} = harness();
  hooks.state.profiles=[{id:'profile-a',revision:1}];
  const pending=hooks.runChecks(false);
  hooks.cancelRun();
  requests[0].resolve({id:'late-report'}); await pending;
  assert.equal(hooks.state.current.size,0);
  assert.equal(hooks.state.run,null);
  assert.match(nodes.get('run-progress').textContent,/canceled/);
});

test('editing model identity invalidates locality approval and the active context', () => {
  const {hooks,nodes}=harness(); hooks.bindEvents();
  for(const id of ['model-endpoint','model-id']) {
    nodes.get('local-confirmed').checked=true; hooks.state.context={id:'previous-model'};
    assert.equal(typeof nodes.get(id).oninput,'function'); nodes.get(id).oninput();
    assert.equal(nodes.get('local-confirmed').checked,false);
    assert.equal(hooks.state.context,null);
  }
});
test('connection settings are locked while a save is pending', async () => {
  const {hooks,nodes,requests}=harness(); hooks.bindEvents();
  const pending=hooks.saveSettings();
  assert.equal(nodes.get('model-fields')?.disabled,true);
  requests[0].resolve({provider:'ollama',endpoint:'http://127.0.0.1:11434',model:'example',localConfirmed:false});
  await pending;
  assert.equal(nodes.get('model-fields').disabled,false);
});
test('copy export uses only the latest redacted snapshot', async () => {
  const {hooks,nodes,requests,copied}=harness();
  const pending=hooks.updateExport();
  assert.equal(typeof hooks.copyExport,'function');
  await hooks.copyExport(); assert.equal(copied.length,0);
  requests[0].resolve({notes:''}); await pending; await hooks.copyExport();
  assert.equal(copied[0],JSON.stringify({notes:''},null,2));
  assert.match(nodes.get('export-status').textContent,/copied/);
});
test('clipboard denial leaves a manual copy instruction', async () => {
  const {hooks,nodes,requests,navigator}=harness();
  const pending=hooks.updateExport(); requests[0].resolve({notes:''}); await pending;
  navigator.clipboard.writeText=async()=>{throw new Error('denied');};
  assert.equal(typeof hooks.copyExport,'function'); await hooks.copyExport();
  assert.match(nodes.get('export-status').textContent,/Select and copy/);
});

test('model discovery holds the settings lock until the list returns', async () => {
  const {hooks,nodes,requests}=harness(); hooks.bindEvents();
  const pending=hooks.saveSettings(true);
  requests[0].resolve({provider:'ollama',endpoint:'http://127.0.0.1:11434',model:'',localConfirmed:false});
  await new Promise(setImmediate);
  assert.equal(requests.length,2); assert.equal(nodes.get('model-fields').disabled,true);
  await hooks.saveSettings(); assert.equal(requests.length,2);
  requests[1].resolve([]); await pending;
  assert.equal(nodes.get('model-fields').disabled,false);
});
test('failed settings save releases controls without clearing pending edits', async () => {
  const {hooks,nodes,requests}=harness(); hooks.bindEvents();
  nodes.get('model-endpoint').oninput();
  const pending=hooks.saveSettings(); requests[0].reject(new Error('offline'));
  await assert.rejects(pending,/offline/);
  assert.equal(nodes.get('model-fields').disabled,false);
  assert.equal(hooks.state.settingsDirty,true);
});

test('CLI identity changes clear cloud consent and stale context', () => {
 const {hooks,nodes}=harness(); hooks.bindEvents();
 nodes.get('model-provider').value='claude-cli'; nodes.get('model-provider').onchange();
 assert.equal(nodes.get('model-endpoint').required,false);
 nodes.get('cloud-confirmed').checked=true; hooks.state.context={id:'old'};
 assert.equal(typeof nodes.get('cli-path').oninput,'function'); nodes.get('cli-path').oninput();
 assert.equal(nodes.get('cloud-confirmed').checked,false); assert.equal(hooks.state.context,null);
});
test('saving CLI sends distinct consent and no local endpoint', async () => {
 const {hooks,nodes,requests}=harness(); hooks.bindEvents();
 nodes.get('model-provider').value='codex-cli'; nodes.get('model-provider').onchange();
 nodes.get('cloud-confirmed').checked=true;
 const pending=hooks.saveSettings();
 const body=JSON.parse(requests[0].options.body);
 assert.equal(body.cloudConfirmed,true); assert.equal(body.localConfirmed,false); assert.equal(body.endpoint,'');
 requests[0].resolve(body); await pending;
});
test('IP provider edits clear typed keys and require fresh diagnostic opt-in', () => {
 const {hooks,nodes}=harness(); hooks.bindEvents();
 nodes.get('ip-api-key').value='typed-secret'; nodes.get('intelligence-check').checked=true;
 nodes.get('ip-provider').value='custom';
 assert.equal(typeof nodes.get('ip-provider').onchange,'function'); nodes.get('ip-provider').onchange();
 assert.equal(nodes.get('ip-api-key').value,''); assert.equal(nodes.get('intelligence-check').checked,false);
 assert.equal(nodes.get('intelligence-check').disabled,true);
});

test('Codex handoff copies reviewed context without sending model requests', async () => {
 const {hooks,nodes,requests,copied}=harness();
 hooks.state.current.set('profile-a',{id:'report-a'}); hooks.state.reportId='report-a';
 hooks.state.settings={provider:'codex-cli',model:'default',cloudConfirmed:true};
 const pending=hooks.previewContext();
 requests[0].resolve({id:'context-a',delivery:'manual',systemPrompt:'Fixed instructions',context:'Redacted report',settings:{provider:'codex-cli',model:'default',cloudConfirmed:true}});
 await pending;
 assert.equal(nodes.get('send-chat').disabled,true); assert.equal(nodes.get('copy-context').disabled,false);
 nodes.get('chat-input').value='Explain this'; await hooks.copyContext();
 assert.equal(requests.length,1); assert.match(copied[0],/Redacted report/); assert.match(copied[0],/Explain this/);
 hooks.bindEvents(); nodes.get('model-id').oninput(); await hooks.copyContext(); assert.equal(copied.length,1);
});
test('saved IP key is cleared from the form and requires a new lookup opt-in', async () => {
 const {hooks,nodes,requests}=harness(); hooks.bindEvents();
 nodes.get('ip-provider').value='ipapi'; nodes.get('ip-endpoint').value='https://api.ipapi.is/'; nodes.get('ip-api-key').value='PRIVATE'; hooks.state.ipEditRevision=4;
 const pending=hooks.saveIPSettings(); assert.equal(nodes.get('ip-fields').disabled,true);
 assert.equal(JSON.parse(requests[0].options.body).revision,4);
 requests[0].resolve({provider:'ipapi',endpoint:'https://api.ipapi.is/',revision:5,keyConfigured:true,keySource:'saved on this device',ready:true}); await pending;
 assert.equal(nodes.get('ip-api-key').value,''); assert.equal(nodes.get('intelligence-check').checked,false); assert.equal(nodes.get('ip-fields').disabled,false);
 assert.equal(hooks.state.ipEditRevision,5);
});

test('manual handoff does not claim control over the receiving client tools', () => {
 const {hooks,nodes}=harness(); hooks.state.settings={provider:'codex-cli'}; hooks.renderSelectors();
 assert.doesNotMatch(nodes.get('assistant-boundary').textContent,/Model tools are disabled/);
 assert.match(nodes.get('assistant-boundary').textContent,/receiving client/i);
});

test('direct chat sends only typed issue context and works without a service', async () => {
 const {hooks,nodes,requests}=harness();
 hooks.state.selected=''; hooks.state.settings={provider:'claude-cli',model:'default',cloudConfirmed:true};
 nodes.get('assistant-mode').value='direct'; nodes.get('triage-stage').value='login'; nodes.get('triage-error').value='HTTP 403';
 nodes.get('include-notes').checked=true; nodes.get('assistant-notes').value='PRIVATE STORED NOTES';
 nodes.get('chat-input').value='What should I check?';
 const pending=hooks.sendChat({preventDefault(){}});
 assert.equal(requests.length,1, 'sending starts a direct context without a report');
 assert.deepEqual(JSON.parse(requests[0].options.body),{mode:'direct',stage:'login',errorText:'HTTP 403'});
 requests[0].resolve({id:'direct-a',delivery:'direct',settings:hooks.state.settings,context:'User typed HTTP 403',systemPrompt:'Instructions'});
 await new Promise(setImmediate);
 assert.equal(requests.length,2);
 assert.deepEqual(JSON.parse(requests[1].options.body),{contextId:'direct-a',message:'What should I check?'});
 requests[1].resolve({reply:'Check the response details.'}); await pending;
 assert.equal(nodes.get('chat-input').value,''); assert.equal(nodes.get('send-chat').disabled,false);
});
test('changing context while a direct conversation starts prevents a stale send', async () => {
 const {hooks,nodes,requests}=harness();
 hooks.state.settings={provider:'claude-cli',model:'default',cloudConfirmed:true}; nodes.get('assistant-mode').value='direct'; nodes.get('chat-input').value='Test';
 const pending=hooks.sendChat({preventDefault(){}});
 assert.equal(requests.length,1);
 hooks.resetChat();
 requests[0].resolve({id:'stale',delivery:'direct',settings:hooks.state.settings}); await pending;
 assert.equal(requests.length,1); assert.equal(hooks.state.context,null);
});
test('direct chat never sends without connection consent or through manual Codex handoff', async () => {
 const {hooks,nodes,requests}=harness(); nodes.get('assistant-mode').value='direct';
 for(const settings of [{provider:'claude-cli',model:'default',cloudConfirmed:false},{provider:'ollama',model:'local',localConfirmed:false},{provider:'codex-cli',model:'default',cloudConfirmed:true}]) {
   hooks.state.settings=settings; nodes.get('chat-input').value='Hello'; await hooks.sendChat({preventDefault(){}});
 }
 assert.equal(requests.length,0);
});
test('report comparison matches findings by ID and keeps missing evidence unknown', () => {
 const {hooks}=harness(); assert.equal(typeof hooks.compareReports,'function');
 const before={id:'a',profileId:'p',scoreVersion:'1',profileRevision:1,lower:2,upper:2,coverage:100,policy:{mode:'unknown'},findings:[{id:'vpn',title:'VPN',lower:2,upper:2,weight:2,state:'observed'},{id:'clock',title:'Clock',lower:0,upper:0,weight:3,state:'observed'}],target:{state:'unknown',summary:'Not checked'}};
 const after={...before,id:'b',lower:0,coverage:97,profileRevision:2,findings:[before.findings[1],{...before.findings[0],lower:0,upper:2,state:'unknown'}]};
 const result=hooks.compareReports(before,after);
 const vpn=result.rows.find(r=>r.label==='VPN');
 assert.match(vpn.before,/2–2/); assert.match(vpn.after,/0–2/); assert.match(vpn.after,/unknown/);
 assert.equal(vpn.changed,true); assert.equal(result.rows.find(r=>r.label==='Clock').changed,false);
 assert.ok(result.warnings.some(w=>/coverage/i.test(w))); assert.ok(result.warnings.some(w=>/service.*changed/i.test(w)));
 assert.ok(hooks.compareReports(before,{...after,profileId:'other'}).error);
 assert.ok(hooks.compareReports(before,{...after,scoreVersion:'2'}).error);
 assert.match(hooks.compareReports(before,{...after,target:{}}).rows.find(r=>r.label==='Service reachability').after,/unknown/);
});
test('report selectors start with the latest snapshot so older baselines are available', () => {
 const {hooks}=harness(); hooks.state.profiles=[{id:'profile-a'}];
 hooks.state.reports=[{id:'old',profileId:'profile-a',createdAt:'2026-10-01T10:00:00Z',lower:0,upper:97},{id:'new',profileId:'profile-a',createdAt:'2026-10-02T10:00:00Z',lower:0,upper:87}];
 hooks.renderSelectors(); assert.equal(hooks.state.reportId,'new');
});
test('direct chat does not silently adopt a model changed in another window', async () => {
 const {hooks,nodes,requests}=harness(); hooks.state.settings={provider:'ollama',model:'local-a',endpoint:'http://127.0.0.1:11434',localConfirmed:true};
 nodes.get('assistant-mode').value='direct'; nodes.get('chat-input').value='Private question';
 const pending=hooks.sendChat({preventDefault(){}});
 const rejected=assert.rejects(pending,/another window/i);
 requests[0].resolve({id:'new-model',delivery:'direct',settings:{provider:'claude-cli',model:'default',cloudConfirmed:true}});
 await new Promise(setImmediate);
 if (requests[1]) requests[1].resolve({reply:'Unexpected model response'});
 await rejected;
 assert.equal(requests.length,1); assert.equal(hooks.state.context,null); assert.equal(hooks.state.settingsDirty,true);
 assert.equal(nodes.get('cloud-confirmed').checked,false); assert.equal(nodes.get('send-chat').disabled,true);
 assert.equal(nodes.get('chat-input').value,'Private question');
});

test('public-source summary sends only the selected identity and reviewed URLs', async () => {
 const {hooks,nodes,requests}=harness(); assert.equal(typeof hooks.openResearch,'function');
 hooks.state.profiles=[{id:'profile-a',revision:1,name:'Example',notes:'PRIVATE',issues:[{error:'SECRET'}]}];
 hooks.state.settings={provider:'claude-cli',model:'default',cloudConfirmed:true};
 const opening=hooks.openResearch(); requests[0].resolve({profileId:'profile-a',revision:1,urls:['https://example.com/policy']}); await opening;
 assert.equal(nodes.get('research-urls').value,'https://example.com/policy');
 const pending=hooks.runResearch(true);
 assert.deepEqual(JSON.parse(requests[1].options.body),{profileId:'profile-a',revision:1,urls:['https://example.com/policy'],settings:{provider:'claude-cli',model:'default',cloudConfirmed:true}});
 assert.equal(nodes.get('research-fields').disabled,true);
 requests[1].resolve({profileId:'profile-a',revision:1,sources:[],summary:'Cited result'}); await pending;
 assert.equal(nodes.get('research-summary').textContent,'Cited result'); assert.equal(nodes.get('research-fields').disabled,false);
});
test('canceling public-source research discards late responses', async () => {
 const {hooks,nodes,requests}=harness(); assert.equal(typeof hooks.openResearch,'function');
 hooks.state.profiles=[{id:'profile-a',revision:1,name:'Example'}];
 const opening=hooks.openResearch(); requests[0].resolve({profileId:'profile-a',revision:1,urls:['https://example.com']}); await opening;
 const pending=hooks.runResearch(false); hooks.cancelResearch();
 requests[1].resolve({profileId:'profile-a',revision:1,sources:[],summary:'STALE'}); await pending;
 assert.notEqual(nodes.get('research-summary').textContent,'STALE');
 assert.equal(nodes.get('research-fields').disabled,false);
});
test('source lookup does not send to an unconfirmed model', async () => {
 const {hooks,requests}=harness(); assert.equal(typeof hooks.openResearch,'function');
 hooks.state.profiles=[{id:'profile-a',revision:1,name:'Example'}];
 hooks.state.settings={provider:'claude-cli',model:'default',cloudConfirmed:false};
 const opening=hooks.openResearch(); requests[0].resolve({profileId:'profile-a',revision:1,urls:['https://example.com']}); await opening;
 await hooks.runResearch(true); assert.equal(requests.length,1);
});

test('research refuses a model silently changed after its connection label was displayed', async () => {
 const {hooks,nodes,requests}=harness();
 hooks.state.profiles=[{id:'profile-a',revision:1,name:'Example'}];
 hooks.state.settings={provider:'ollama',model:'local-a',endpoint:'http://127.0.0.1:11434',localConfirmed:true};
 const opening=hooks.openResearch(); requests[0].resolve({profileId:'profile-a',revision:1,urls:['https://example.com']}); await opening;
 assert.match(nodes.get('research-model').textContent,/local-a/);
 hooks.state.settings={provider:'claude-cli',model:'default',cloudConfirmed:true};
 const pending=hooks.runResearch(true);
 if (requests[1]) requests[1].resolve({profileId:'profile-a',revision:1,sources:[],summary:'Wrong destination'});
 await pending;
 assert.equal(requests.length,1); assert.equal(hooks.state.settingsDirty,true);
 assert.equal(nodes.get('cloud-confirmed').checked,false); assert.match(nodes.get('research-status').textContent,/settings changed/i);
});

test('network reputation shares IP guidance and keeps hosting separate from a private service blocklist', () => {
 const {hooks,nodes,requests}=harness(), r=diagnosticFixture();
 r.findings=[{id:'network-reputation',title:'Network reputation',weight:5,lower:3,upper:5,state:'unknown',explanation:'Network proportions are heuristics.',recommendation:'Review network evidence',source:'Fixture'},{id:'datacenter',title:'Reported hosting network',weight:4,lower:4,upper:4,state:'observed',explanation:'Hosting is weak evidence.',recommendation:'Review hosting evidence',source:'Fixture'}];
 const guidance=hooks.reportGuidance(r); assert.equal(guidance.next.length,1); assert.equal(guidance.next[0].key,'ip');
 mountReport(hooks,r); hooks.renderReport();
 const text=flattened(nodes.get('report-output')).map(n=>n.textContent).join('\n');
 assert.match(text,/hosting/i); assert.match(text,/private.*blocklist/i); assert.match(text,/abuse proportions/i); assert.equal(requests.length,0);
});
test('raw network metadata renders as text with separate scopes and leaves old snapshots intact', () => {
 const {hooks,nodes,requests}=harness(), r=diagnosticFixture(), original=JSON.stringify(r);
 const legacy={...r,id:'old'}; mountReport(hooks,legacy); hooks.renderReport();
 let text=flattened(nodes.get('report-output')).map(n=>n.textContent).join('\n'); assert.match(text,/Score v1\.0/); assert.match(text,/0–15 \/ 15/);
 assert.match(hooks.compareReports(legacy,{...r,id:'new',scoreVersion:'1.1'}).error,/different scoring versions/);
 r.evidence={system:{timezone:'UTC',offsetMinutes:0,warnings:[]},browser:{languages:[]},clockSkewSeconds:null,exits:[{path:'browser',family:'ipv4',ip:'8.8.8.8',intelligence:{country:'US',abuse:false,datacenter:true,asn:'15169',asnOrganization:'<img src=x onerror=alert(1)>',asnType:'isp',asnRoute:'8.8.8.0/24',asnAbuseRatio:0.1,companyName:'<script>company</script>',companyType:'hosting',companyNetwork:'8.8.8.0 - 8.8.8.255',companyAbuseRatio:0}}]};
 mountReport(hooks,r); hooks.renderReport();
 const rendered=flattened(nodes.get('report-output')); text=rendered.map(n=>n.textContent).join('\n');
 for(const expected of ['<img src=x onerror=alert(1)>','<script>company</script>','8.8.8.0/24','8.8.8.0 - 8.8.8.255','10%','0%']) assert.ok(text.includes(expected),expected);
 assert.match(text,/ASN-wide/); assert.match(text,/IP-level abuse/); assert.match(text,/Service-private blocklist/);
 assert.equal(rendered.some(n=>n.innerHTML),false); assert.equal(JSON.stringify(legacy),original.replace('report-a','old')); assert.equal(requests.length,0);
});
test('raw abuse proportions distinguish tiny positive evidence from exact zero and unknown', () => {
 for (const [value,expected] of [[0,'0%'],[1e-9,'<0.0001%'],[0.000001,'0.0001%'],[null,'Unknown']]) {
  const {hooks,nodes}=harness(), r=diagnosticFixture();
  r.evidence={system:{warnings:[]},browser:{languages:[]},clockSkewSeconds:null,exits:[{path:'browser',family:'ipv4',ip:'8.8.8.8',intelligence:{companyAbuseRatio:value,asnAbuseRatio:value}}]};
  mountReport(hooks,r); hooks.renderReport();
  const rendered=flattened(nodes.get('report-output'));
  for(const label of ['Company-network abuse proportion','ASN-wide abuse proportion (all ASN routes)']) {
   const index=rendered.findIndex(n=>n.textContent===`browser / ipv4 · ${label}`);
   assert.notEqual(index,-1); assert.equal(rendered[index+1].textContent,expected);
  }
 }
});
