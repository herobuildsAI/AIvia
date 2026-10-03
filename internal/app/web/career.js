'use strict';
window.AIviaCareer = (() => {
  let api, el, settings, data, active = false, generation = 0, operation = null, writing = false, pendingEnter = false;
  let reviewed = null, studentDirty = false, companyID = '', pasteKind = 'jobs', deletePath = '', returnFocus, savedFocus;
  let mutationButtons = [], noteEditButtons = [];
  const selectedJobs = new Set(), selectedUpdates = new Set(), pages = {jobs: 0, updates: 0, usage: 0};
  const $ = id => document.getElementById(id);
  const fields = ['stage', 'skills', 'projects', 'roles', 'locations'];
  const examples = [
    {name:'Mistral', website:'https://mistral.ai/', careersUrl:'https://jobs.ashbyhq.com/mistral.ai', newsUrls:[]},
    {name:'Anthropic', website:'https://www.anthropic.com/', careersUrl:'https://job-boards.greenhouse.io/anthropic', newsUrls:[]},
    {name:'Cohere', website:'https://cohere.com/', careersUrl:'https://jobs.ashbyhq.com/cohere', newsUrls:[]},
    {name:'OpenAI', website:'https://openai.com/', careersUrl:'https://jobs.ashbyhq.com/openai', newsUrls:['https://openai.com/news/rss.xml']}
  ];
  function status(text) { $('career-status').textContent = text; for (const name of ['company','paste','delete']) if ($('career-'+name+'-dialog').open) $('career-'+name+'-status').textContent = text; }
  function button(text, handler, cls = 'secondary') {
    const node = el('button', text, cls); node.type = 'button'; node.onclick = handler; return node;
  }
  function link(text, raw) {
    try { const url = new URL(raw); if (url.protocol !== 'https:' || url.username || url.password) throw new Error();
      const node = el('a', text); node.href = url.href; node.target = '_blank'; node.rel = 'noopener noreferrer'; return node;
    } catch { return el('span', `${text}: unavailable / unsafe URL`, 'small'); }
  }
  function stop(contextOnly = false) {
    if (contextOnly && operation?.kind !== 'context') return;
    ++generation; operation?.controller.abort(); if (!writing) operation = null; controls();
  }
  function invalidate() {
    stop(true); reviewed = null; $('career-send-consent').checked = false;
    $('career-preview-details').hidden = true; $('career-context').textContent = ''; controls();
  }
  function destination(s) {
    if (!s) return 'Save changed assistant settings before previewing.';
    if (s.provider === 'codex-cli') return `Codex manual handoff · ${s.model || 'client default'}. This app does not send context.`;
    if (s.provider === 'claude-cli') return `Installed Claude CLI · ${s.model || 'CLI default'} · provider account and policies apply. Cloud consent: ${s.cloudConfirmed ? 'confirmed' : 'required in Settings'}.`;
    return `${s.provider || 'Unconfigured'} · ${s.endpoint || 'no destination'} · ${s.model || 'no model'} · local consent: ${s.localConfirmed ? 'confirmed' : 'required in Settings'}.`;
  }
  function ready(s) { return !!s && (s.provider === 'claude-cli' ? !!s.cloudConfirmed : ['ollama','compatible'].includes(s.provider) && !!s.model && !!s.localConfirmed); }
  function controls() {
    const busy = writing || !!operation, locked = writing || operation?.kind === 'read', analyzing = operation?.path === '/api/career/analyze';
    $('career-cancel').hidden = !operation;
    $('career-preview').disabled = busy || studentDirty || !data;
    $('career-copy').disabled = busy || !reviewed;
    $('career-analyze').disabled = busy || !reviewed || reviewed.delivery === 'manual' || !ready(reviewed.settings) || !$('career-send-consent').checked;
    $('career-model').textContent = destination(settings?.());
    $('career-selection-count').textContent = `${selectedJobs.size + selectedUpdates.size} / 6 evidence records selected`;
    for (const id of ['career-company-fields','career-paste-fields','career-student-fields','career-company-save','career-paste-save','career-student-save','career-key-save','career-key-clear','career-usage-fetch','career-note-save','career-delete-confirm','career-add-company','career-examples','career-reload','career-paste-job','career-paste-update']) $(id).disabled = locked;
    $('career-note-text').disabled = analyzing;
    for (const node of mutationButtons) node.disabled = locked;
    for (const node of noteEditButtons) node.disabled = locked || analyzing;
  }
  async function request(path, method, body, kind, accept) {
    if (!active || writing || operation) return;
    const controller = new AbortController(), own = {controller, kind, path}, token = ++generation;
    operation = own; if (kind === 'write') writing = true; controls(); status(kind === 'context' ? 'Preparing reviewed context…' : 'Working…');
    try {
      const result = await api(path, method, body, controller.signal);
      if (!active || token !== generation || controller.signal.aborted) return;
      status(kind === 'write' ? 'Saved locally. Review source status below.' : 'Ready.'); accept(result);
    } catch (error) { if (active && token === generation && !controller.signal.aborted) status(error.message || 'Request failed. Previous successful results retained.'); }
    finally { if (operation === own) operation = null; if (kind === 'write') writing = false; controls(); if (active && savedFocus) { savedFocus.focus(); savedFocus = null; } if (kind === 'write' && pendingEnter && active) { pendingEnter = false; void enter(); } }
  }
  function apply(next, populate = false) {
    data = next; reviewed = null; $('career-send-consent').checked = false; $('career-preview-details').hidden = true; $('career-context').textContent = '';
    for (const id of selectedJobs) if (!data.jobs.some(x => x.id === id)) selectedJobs.delete(id);
    for (const id of selectedUpdates) if (!data.updates.some(x => x.id === id)) selectedUpdates.delete(id);
    if (populate) { for (const field of fields) $('career-student-' + field).value = data.student[field] || ''; studentDirty = false; }
    render();
  }
  function mutationReady() {
    if (!active || !data || writing) return false;
    if (operation?.kind === 'read') { status('Wait for the local reload to finish before saving or refreshing.'); return false; }
    return true;
  }
  async function mutate(path, body, method = 'POST', done) {
    if (!mutationReady()) return;
    invalidate();
    return request(path, method, {revision:data.revision, ...body}, 'write', next => { apply(next); done?.(); });
  }
  async function enter() {
    active = true; stop(); invalidate();
    if (writing) { pendingEnter = true; status('Waiting for the canceled save to settle, then reloading local state…'); return; }
    pendingEnter = false;
    return request('/api/career', 'GET', undefined, 'read', next => apply(next, true));
  }
  function leave() { active = false; pendingEnter = false; stop(); invalidate(); $('career-key').value = ''; savedFocus = null; for (const id of ['career-company-dialog','career-paste-dialog','career-delete-dialog']) $(id).close(); }
  function settingsChanged() { invalidate(); }
  function companyName(id) { return data.companies.find(x => x.id === id)?.name || 'Unknown company'; }
  function freshness(company, kind) {
    const sources = (company.refreshes || []).filter(x => !kind || x.kind === kind);
    if (!sources.length) return el('p', 'Not refreshed · freshness unknown', 'small');
    const node = el('div');
    for (const source of sources) {
      const stale = !source.lastSuccess || Date.now() - Date.parse(source.lastSuccess) > 7 * 86400000;
      node.append(el('p', `${source.kind} · ${source.error ? 'Failed: ' + source.error : source.lastSuccess ? (source.complete ? 'Complete board' : source.kind === 'updates' ? 'Successful bounded sample / snapshot' : 'Incomplete / capped board or page snapshot') : 'Not retrieved'} · checked ${source.checkedAt || 'unknown'} · last success ${source.lastSuccess || 'unknown'}${stale ? ' · stale / freshness unknown' : ''}`, 'small'), link('Source', source.sourceUrl));
    }
    return node;
  }
  function renderCompanies() {
    const target = $('career-companies'); target.replaceChildren();
    if (!data.companies.length) target.append(el('p', 'No companies saved. Add a company or review editable examples.', 'empty'));
    for (const company of data.companies) {
      const card = el('article', undefined, 'career-card'); card.append(el('h3', company.name), link('Website', company.website), el('p', 'Recruiting source', 'small'), link('Recruiting', company.careersUrl));
      for (const url of company.newsUrls || []) card.append(el('p', 'News source', 'small'), link('News', url));
      card.append(el('p', `Model-prefix labels: ${(company.modelPrefixes || []).join(', ') || 'none'} (user-entered; not an employer ranking)`, 'small'), freshness(company));
      const actions = el('div', undefined, 'actions');
      actions.append(button('Edit company', () => editCompany(company)), button('Refresh jobs', () => mutate('/api/career/refresh',{companyId:company.id,kind:'jobs'})), button('Refresh updates', () => mutate('/api/career/refresh',{companyId:company.id,kind:'updates'})), button('Delete company', () => confirmDelete('/api/career/companies/' + encodeURIComponent(company.id), `Delete ${company.name}, its evidence and notes citing it?`), 'danger'));
      mutationButtons.push(...actions.children);
      card.append(actions); target.append(card);
    }
  }
  function selectEvidence(item, kind) {
    const set = kind === 'jobs' ? selectedJobs : selectedUpdates, label = el('label', undefined, 'check-label'), check = el('input');
    check.type = 'checkbox'; check.checked = set.has(item.id);
    check.onchange = () => {
      if (check.checked && !set.has(item.id) && selectedJobs.size + selectedUpdates.size >= 6) { check.checked = false; status('Select at most six evidence records.'); return; }
      check.checked ? set.add(item.id) : set.delete(item.id); invalidate();
    };
    label.append(check, document.createTextNode('Include this evidence in advice')); return label;
  }
  function renderEvidence(kind) {
    const isJob = kind === 'jobs', target = $('career-' + kind); target.replaceChildren();
    const list = data[kind].filter(item => !isJob || ((!$('career-title-filter').value || item.title.toLowerCase().includes($('career-title-filter').value.trim().toLowerCase())) && (!$('career-location-filter').value || (item.location || 'unknown').toLowerCase().includes($('career-location-filter').value.trim().toLowerCase())) && (!$('career-level-filter').value || item.level === $('career-level-filter').value) && (!$('career-remote-filter').value || item.workplace === $('career-remote-filter').value)));
    const total = Math.max(1, Math.ceil(list.length / 50)); pages[kind] = Math.min(pages[kind], total - 1);
    $('career-' + kind + '-count').textContent = `${list.length} records · page ${pages[kind] + 1} / ${total} · up to 50 per page`;
    $('career-' + kind + '-prev').disabled = pages[kind] === 0; $('career-' + kind + '-next').disabled = pages[kind] >= total - 1;
    if (!list.length) target.append(el('p', 'No matching evidence. Refresh saved sources or paste reviewed excerpts.', 'empty'));
    for (const item of list.slice(pages[kind] * 50, (pages[kind] + 1) * 50)) {
      const card = el('article', undefined, 'career-card'); card.append(selectEvidence(item, kind), el('h3', item.title), el('p', companyName(item.companyId), 'small'));
      if (isJob) card.append(el('p', `${item.location || 'unknown'} · ${item.workplace || 'unknown'} · ${item.employmentType || 'unknown'}`, 'small'), el('p', `${item.level || 'unknown'} · ${item.levelBasis === 'title-inferred' ? 'level inferred from title; verify eligibility' : item.levelBasis || 'unknown level basis'}`, 'pill'), el('p', item.listingState === 'listed' ? 'Listed when retrieved; check current opening and eligibility.' : item.listingState === 'no-longer-listed' ? 'No longer listed on a complete board refresh; not proof the role closed.' : 'Listing unknown · not a confirmed opening', 'small'), el('p', `Provider: ${item.provider || 'unknown'}${item.provider === 'paste' ? ' · pasted user evidence' : ''} · date basis: ${item.dateBasis || 'unknown'}`, 'small'), el('p', `Published: ${item.publishedAt || 'unknown'} · updated: ${item.updatedAt || 'unknown'}`, 'small'));
      else card.append(el('p', `${item.kind || 'unknown'}${item.kind === 'pasted' ? ' · pasted user evidence' : ' · bounded sample / snapshot'} · published: ${item.publishedAt || 'unknown'}`, 'small'));
      card.append(el('p', `Retrieved: ${item.fetchedAt || 'unknown'}${!item.fetchedAt || Date.now() - Date.parse(item.fetchedAt) > 7 * 86400000 ? ' · stale / freshness unknown' : ''}${item.truncated ? ' · excerpt truncated' : ''}`, 'small'), el('p', item.text || 'No excerpt available', 'career-excerpt'), link('Original record', item.url), el('p', 'Source identity', 'small'), link('Retrieved source', item.sourceUrl));
      const company = data.companies.find(x => x.id === item.companyId); if (company) card.append(freshness(company, kind));
      target.append(card);
    }
  }
  function renderUsage() {
    const target = $('career-usage'), usage = data.usage || {rows:[]}; target.replaceChildren();
    $('career-key-status').textContent = data.keyConfigured ? 'Key configured (value never returned).' : 'No saved key configured.';
    if (!usage.fetchedAt) { target.append(el('p', 'No optional usage snapshot. Fetch only when you choose.', 'small')); return; }
    target.append(el('p', `OpenRouter daily model usage · as of ${usage.asOf || 'unknown'} · UTC window ${usage.startDate || 'unknown'} to ${usage.endDate || 'unknown'} · version ${usage.version || 'unknown'} · retrieved ${usage.fetchedAt}`, 'small'), el('p', `Source: OpenRouter (openrouter.ai/rankings), as of ${usage.asOf || 'unknown'}. Licensed under CC BY 4.0. Tokens reflect OpenRouter usage; no market share or employer ranking is implied.`, 'small'), link('Dataset source', usage.sourceUrl), link('CC BY 4.0', 'https://creativecommons.org/licenses/by/4.0/'));
    const rows = usage.rows || [], total = Math.max(1, Math.ceil(rows.length / 50)); pages.usage = Math.min(pages.usage, total - 1);
    const table = el('table'), header = el('tr'); for (const text of ['UTC date','Model','Total tokens','User-entered association']) header.append(el('th',text)); const head=el('thead'); head.append(header); table.append(head); const body=el('tbody');
    for (const row of rows.slice(pages.usage * 50,(pages.usage + 1) * 50)) {
      const names = row.model === 'other' ? [] : data.companies.filter(c => (c.modelPrefixes || []).some(prefix => row.model.startsWith(prefix))).map(c => c.name);
      const tr = el('tr'); for (const value of [row.date,row.model,row.totalTokens, names.length ? names.join(', ') + ' (user-entered)' : row.model === 'other' ? 'Aggregate other; no employer association' : 'Unassociated']) tr.append(el('td',value)); body.append(tr);
    }
    table.append(body); const scroll = el('div',undefined,'comparison-table'); scroll.append(table); target.append(scroll);
    const prev=button('Previous usage',()=>{pages.usage--;renderUsage();}), next=button('Next usage',()=>{pages.usage++;renderUsage();}); prev.disabled=pages.usage===0;next.disabled=pages.usage>=total-1;
    const actions=el('div',undefined,'actions');actions.append(prev,el('span',`Page ${pages.usage+1} / ${total}`),next);target.append(actions);
  }
  function renderNotes() {
    const target = $('career-notes'); target.replaceChildren();
    for (const note of data.advice) {
      const card = el('article', undefined, 'career-card'); card.append(el('h3', 'Saved note'), el('p', `Saved ${note.createdAt} · selected provider ${note.provider || 'unknown'} · model ${note.model || 'default / unknown'}. User-saved text; authorship unverified.`, 'small'), el('p', note.text, 'career-excerpt'));
      for (const source of note.sources || []) card.append(link(source.title,source.url), el('p', `Published ${source.publishedAt || 'unknown'} · retrieved ${source.fetchedAt || 'unknown'}`, 'small'));
      const edit = button('Edit in draft',()=>{if ($('career-note-text').disabled) return; $('career-note-text').value=note.text;status('Copied saved note to draft. Saving creates a new note with your current selection.');}), remove = button('Delete saved note',()=>confirmDelete('/api/career/advice/'+encodeURIComponent(note.id),'Delete this saved note?'),'danger');
      noteEditButtons.push(edit); mutationButtons.push(remove); card.append(edit, remove);target.append(card);
    }
  }
  function render() { mutationButtons = []; noteEditButtons = []; renderCompanies(); renderEvidence('jobs'); renderEvidence('updates'); renderUsage(); renderNotes(); controls(); }
  function openDialog(id) { if (writing) { status('Wait for the current save to finish.'); return false; } returnFocus = document.activeElement; $(id.replace('-dialog','-status')).textContent = ''; $(id).showModal(); return true; }
  function closeDialog(id) { $(id).close(); if (writing) { savedFocus = returnFocus; status('The save continues after this editor closes. Wait for it to finish before another write.'); } else returnFocus?.focus(); }
  function fillCompany(company) {
    companyID = company.id || ''; $('career-company-name').value=company.name || ''; $('career-company-website').value=company.website || ''; $('career-company-careers').value=company.careersUrl || ''; $('career-company-news').value=(company.newsUrls || []).join('\n'); $('career-company-prefixes').value=(company.modelPrefixes || []).join('\n');
  }
  function closeSavedDialog(id) { $(id).close(); savedFocus = returnFocus; }
  function editCompany(company = {}, example = false) {
    if (!openDialog('career-company-dialog')) return; invalidate(); fillCompany(company); $('career-example-label').hidden=!example; $('career-company-title').textContent=company.id ? 'Edit company' : example ? 'Review an example company' : 'Add company'; $('career-company-name').focus();
  }
  const lines = value => value.split('\n').map(x=>x.trim()).filter(Boolean);
  function saveCompany(event) {
    event?.preventDefault(); return mutate('/api/career/companies',{company:{id:companyID,name:$('career-company-name').value.trim(),website:$('career-company-website').value.trim(),careersUrl:$('career-company-careers').value.trim(),newsUrls:lines($('career-company-news').value),modelPrefixes:lines($('career-company-prefixes').value)}},'POST',()=>closeSavedDialog('career-company-dialog'));
  }
  function openPaste(kind) {
    if (!data?.companies.length) { status('Save a company before pasting evidence.'); return; }
    if (!openDialog('career-paste-dialog')) return; pasteKind=kind;
    const select=$('career-paste-company');select.replaceChildren();for(const company of data.companies){const option=el('option',company.name);option.value=company.id;select.append(option);}select.value=data.companies[0].id;
    for(const id of ['career-paste-name','career-paste-url','career-paste-text']) $(id).value=''; $('career-paste-title').textContent=kind==='jobs'?'Paste job evidence':'Paste update evidence';$('career-paste-name').focus();
  }
  function savePaste(event) { event?.preventDefault();return mutate('/api/career/paste',{companyId:$('career-paste-company').value,kind:pasteKind,title:$('career-paste-name').value.trim(),url:$('career-paste-url').value.trim(),text:$('career-paste-text').value},'POST',()=>closeSavedDialog('career-paste-dialog')); }
  function confirmDelete(path,text) { if(!openDialog('career-delete-dialog'))return;invalidate();deletePath=path;$('career-delete-text').textContent=text;$('career-delete-cancel').focus(); }
  function selection() { return {jobIds:[...selectedJobs],updateIds:[...selectedUpdates],includeStudent:$('career-include-student').checked,question:$('career-question').value.trim()}; }
  function sameSettings(a, b) { return !!a && !!b && ['provider','endpoint','model','cliPath'].every(key=>(a[key] || '')===(b[key] || '')) && !!a.localConfirmed===!!b.localConfirmed && !!a.cloudConfirmed===!!b.cloudConfirmed; }
  async function preview() {
    if (!settings()) { status('Save assistant settings before previewing.'); return; } if (studentDirty) { status('Save student profile edits before previewing.'); return; } if (!data) return;
    invalidate();return request('/api/career/preview','POST',{revision:data.revision,selection:selection()},'context',result=>{
      if(!sameSettings(result.settings,settings())){status('Assistant settings changed. Save settings and preview again.');return;}
      reviewed=result;$('career-context').textContent=JSON.stringify({messages:result.messages,sources:result.sources,settings:result.settings,delivery:result.delivery},null,2);$('career-preview-details').hidden=false;$('career-preview-details').open=true;
    });
  }
  async function analyze() {
    if ($('career-analyze').disabled || !reviewed || !$('career-send-consent').checked || !ready(reviewed.settings)) return;
    const context=reviewed;return request('/api/career/analyze','POST',{revision:data.revision,selection:selection(),previewHash:context.hash,settings:context.settings},'context',result=>{$('career-note-text').value=result.reply;});
  }
  async function copy() {
    if(!reviewed || $('career-copy').disabled)return;const token=generation;
    try { await navigator.clipboard.writeText(reviewed.messages.map(message=>`${message.role}:\n${message.content}`).join('\n\n'));if(active&&token===generation)status('Reviewed messages copied. Paste into your chosen client; its data policy applies.'); }
    catch { if(active&&token===generation)status('Clipboard unavailable. Copy the displayed messages manually.'); }
  }
  function saveStudent(event) { event?.preventDefault();const student={};for(const field of fields)student[field]=$('career-student-'+field).value;return mutate('/api/career/student',{student},'PUT',()=>{studentDirty=false;controls();}); }
  function saveKey() {
    if(!mutationReady())return;const key=$('career-key').value,clearKey=$('career-key-clear').checked;$('career-key').value='';return mutate('/api/career/key',{key,clearKey},'POST',()=>{$('career-key-clear').checked=false;});
  }
  function saveNote() { const s=settings();if(studentDirty || !s){status('Save profile and assistant settings before saving a note.');return;}return mutate('/api/career/advice',{text:$('career-note-text').value,selection:selection(),settings:s}); }
  function init(deps) {
    ({api,el,settings}=deps);
    for(const panel of ['companies','jobs','updates','advice'])$('career-tab-'+panel).onclick=()=>{for(const name of ['companies','jobs','updates','advice']){$('career-panel-'+name).hidden=name!==panel;$('career-tab-'+name).setAttribute('aria-current',name===panel?'page':'false');}};
    $('career-reload').onclick=enter;$('career-cancel').onclick=()=>{stop();invalidate();status('Operation canceled. Previous evidence retained. Reload local state before retrying if a save may have completed.');};
    $('career-add-company').onclick=()=>editCompany();$('career-examples').onclick=()=>{$('career-example').value='0';editCompany(examples[0],true);};$('career-example').onchange=()=>{invalidate();fillCompany(examples[Number($('career-example').value)]);};
    $('career-company-save').type='button';$('career-company-save').onclick=saveCompany;$('career-company-form').onsubmit=saveCompany;
    $('career-paste-save').type='button';$('career-paste-save').onclick=savePaste;$('career-paste-form').onsubmit=savePaste;
    $('career-student-save').type='button';$('career-student-save').onclick=saveStudent;$('career-student-form').onsubmit=saveStudent;
    for(const name of ['company','paste'])for(const suffix of ['close','cancel'])$('career-'+name+'-'+suffix).onclick=()=>closeDialog('career-'+name+'-dialog');
    for(const name of ['company','paste','delete'])$('career-'+name+'-dialog').oncancel=event=>{event.preventDefault();closeDialog('career-'+name+'-dialog');};
    $('career-delete-cancel').onclick=()=>closeDialog('career-delete-dialog');$('career-delete-confirm').onclick=()=>mutate(deletePath,{},'DELETE',()=>closeSavedDialog('career-delete-dialog'));
    $('career-paste-job').onclick=()=>openPaste('jobs');$('career-paste-update').onclick=()=>openPaste('updates');
    for(const id of ['title','location','level','remote']){const node=$('career-'+id+'-filter');node.oninput=node.onchange=()=>{pages.jobs=0;if(data)renderEvidence('jobs');};}
    for(const kind of ['jobs','updates'])for(const [suffix,delta] of [['prev',-1],['next',1]])$('career-'+kind+'-'+suffix).onclick=()=>{pages[kind]+=delta;renderEvidence(kind);};
    for(const field of fields)$('career-student-'+field).oninput=()=>{studentDirty=true;invalidate();};
    for(const id of ['name','website','careers','news','prefixes'])$('career-company-'+id).oninput=invalidate;
    $('career-question').oninput=invalidate;$('career-include-student').onchange=invalidate;$('career-send-consent').onchange=controls;
    $('career-preview').onclick=preview;$('career-analyze').onclick=analyze;$('career-copy').onclick=copy;$('career-note-save').onclick=saveNote;
    $('career-key-save').onclick=saveKey;$('career-key-clear').onchange=()=>{if($('career-key-clear').checked)$('career-key').value='';};$('career-usage-fetch').onclick=()=>mutate('/api/career/usage',{});
    controls();
  }
  return {init,enter,leave,settingsChanged};
})();
