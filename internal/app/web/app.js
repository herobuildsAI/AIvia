'use strict';
const $ = id => document.getElementById(id);
const state = {profiles: [], reports: [], settings: {}, selected: '', current: new Map(), reportId: '', view: 'services', generation: 0, chatGeneration: 0, run: null, chat: null, preparing: null, context: null, settingsDirty: false, ipSettings: {}, ipDirty: false, ipEditRevision: 0};
let session, profileEdit, issueEdit, exportData, exportKind, confirmAction;
let profileGeneration = 0, profileSaving = false;
let issueGeneration = 0, issueSaving = false;
let exportGeneration = 0;
let researchGeneration = 0, researchRun = null, researchProfile = null, researchSettings = null;
let templateGeneration = 0, templateRun = null;
const titles = {services: 'My services', diagnostics: 'Diagnostics', assistant: 'Assistant', settings: 'Settings'};
function el(tag, text, className) { const node = document.createElement(tag); if (text !== undefined) node.textContent = text; if (className) node.className = className; return node; }
function action(text, handler, className = 'secondary') { const b = el('button', text, className); b.type = 'button'; b.addEventListener('click', () => perform(handler)); return b; }
function notice(message, error = false) { $('notice').textContent = message; $('notice').className = error ? 'error' : ''; $('notice').hidden = !message; }
async function perform(fn) { try { await fn(); } catch (e) { if (e.name !== 'AbortError') notice(e.message, true); } }
async function api(path, method = 'GET', body, signal) {
  const response = await fetch(path, {method, signal, cache: 'no-store', credentials: 'same-origin', headers: {'X-Session-Token': session?.token || '', ...(body === undefined ? {} : {'Content-Type': 'application/json'})}, body: body === undefined ? undefined : JSON.stringify(body)});
  let data; try { data = await response.json(); } catch { throw new Error('The local server returned an invalid response. Reload the page.'); }
  if (!response.ok) throw new Error(data.error || `Local request failed (${response.status}).`);
  return data;
}
function profile() { return state.profiles.find(p => p.id === state.selected); }
function reports() {
  const saved = state.reports.filter(r => r.profileId === state.selected);
  const current = state.current.get(state.selected);
  const list = current ? [current, ...saved.filter(r => r.id !== current.id)] : saved;
  return list.sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt));
}
function report() { return reports().find(r => r.id === state.reportId); }
function date(raw) { return new Date(raw).toLocaleString('en-US', {dateStyle: 'medium', timeStyle: 'short'}); }
function range(r) { return `${Number(r.lower.toFixed(1))}–${Number(r.upper.toFixed(1))}`; }
function showView(view) {
  if (!titles[view]) view = 'services';
  state.view = view;
  for (const key of Object.keys(titles)) $(key + '-view').hidden = key !== view;
  document.querySelectorAll('[data-view]').forEach(b => { b.classList.toggle('active', b.dataset.view === view); b.setAttribute('aria-current', b.dataset.view === view ? 'page' : 'false'); });
  $('breadcrumb').textContent = `Workspace / ${titles[view]}`;
  if (location.hash !== '#' + view) history.replaceState(null, '', '#' + view);
}
function cancelRun() {
  ++state.generation; state.run?.abort(); state.run = null;
  $('run-progress').textContent = 'Check canceled. Previous report retained.'; updateRunButtons();
}
function directChat() { return $('assistant-mode').value === 'direct'; }
function assistantReady(settings = state.context?.settings || state.settings) {
  return !state.settingsDirty && settings.provider !== 'codex-cli' && !!settings.model && (isCLI(settings) ? settings.cloudConfirmed : settings.localConfirmed);
}
function updateChatControls() {
  const direct = directChat(), busy = !!(state.chat || state.preparing);
  $('assistant-report-selectors').hidden = direct; $('report-notes-controls').hidden = direct;
  $('context-description').textContent = direct ? 'Only your messages and the optional issue below are sent. Stored reports and service notes are not attached.' : 'The selected redacted report, reviewed optional notes, your issue, and conversation messages are sent after you preview the context.';
  $('send-chat').disabled = busy || !assistantReady() || (!direct && !state.context);
  $('preview-context').disabled = busy || state.settingsDirty || (!direct && !report());
  $('stop-chat').hidden = !busy; $('chat-input').disabled = busy;
  if (!state.context && !busy) $('chat-progress').textContent = state.settingsDirty ? 'Save your changed assistant settings first.' : state.settings.provider === 'codex-cli' ? 'Preview and copy the prompt to your Codex client. This app does not send it.' : !assistantReady() ? 'Configure an assistant and review its connection consent in Settings.' : direct ? 'Ready. Your messages and optional issue will be sent to the selected assistant.' : 'Preview the selected report before sending.';
}
function resetChat(preserveDraft = false) {
  ++state.chatGeneration; state.chat?.abort(); state.preparing?.abort(); state.chat = null; state.preparing = null; state.context = null;
  $('context-details').hidden = true; $('context-preview').textContent = ''; $('copy-context').disabled = true; $('copy-context-status').textContent = '';
  if (!preserveDraft) $('chat-input').value = '';
  $('chat-history').replaceChildren(el('p', 'Start a conversation. A diagnostic report is optional. Messages stay in memory.', 'muted'));
  updateChatControls();
}
function selectProfile(id) {
  if (state.selected !== id) cancelResearch();
  if (state.selected !== id) { if (state.run) cancelRun(); resetChat(); state.selected = id; state.reportId = ''; $('include-notes').checked = false; $('assistant-notes').hidden = true; $('assistant-notes').value = ''; }
  renderServices(); renderDetail(); renderSelectors(); renderReport();
}
function renderSelectors() {
  if (!state.profiles.some(p => p.id === state.selected)) state.selected = state.profiles[0]?.id || '';
  for (const id of ['diagnostic-service', 'assistant-service']) {
    const select = $(id); select.replaceChildren();
    if (!state.profiles.length) select.append(new Option('Add a service first', ''));
    state.profiles.forEach(p => select.append(new Option(p.name, p.id))); select.value = state.selected;
  }
  const list = reports();
  if (!list.some(r => r.id === state.reportId)) state.reportId = list[0]?.id || '';
  for (const id of ['diagnostic-report', 'assistant-report']) {
    const select = $(id); select.replaceChildren();
    if (!list.length) select.append(new Option('No report yet', ''));
    list.forEach(r => select.append(new Option(`${date(r.createdAt)} · ${range(r)} · ${state.reports.some(v => v.id === r.id) ? 'Saved' : 'In memory'}`, r.id))); select.value = state.reportId;
  }
  updateChatControls();
  $('target-check').disabled = !profile()?.origin;
  if (!profile()?.origin) $('target-check').checked = false;
  $('destinations').textContent = `Network checks contact api.ipify.org (IPv4), api6.ipify.org (IPv6), and www.cloudflare.com (clock comparison). Optional STUN: ${session?.stun || 'not configured'}. Optional service origin: ${profile()?.origin || 'none'}. Each destination sees the network address used to reach it.`;
  $('model-summary').textContent = `${state.settings.provider || 'Ollama'} · ${state.settings.model || 'No model selected'} · ${state.settings.provider === 'codex-cli' ? 'Manual handoff · clipboard only' : isCLI() ? 'Installed CLI · may use cloud inference' : state.settings.endpoint || ''}`;
  $('assistant-boundary').textContent = state.settings.provider === 'codex-cli' ? 'Manual handoff: the receiving client’s tools, permissions, and data policies apply. This app does not control them.' : isCLI() ? 'Model tools are disabled by the connector. Your CLI administrator policies and hooks may still apply. Answers may be inaccurate.' : 'The assistant cannot run commands, change your VPN, or create accounts. Answers may be inaccurate.';
  updateRunButtons();
}
function updateRunButtons() { for (const id of ['run-local', 'run-network']) $(id).disabled = !profile() || !!state.run; $('cancel-run').hidden = !state.run; }
async function refresh() {
  const [data, ip] = await Promise.all([api('/api/state'), api('/api/ip-settings')]);
  if (state.ipSettings.revision !== undefined && state.ipSettings.revision !== ip.revision) $('intelligence-check').checked = false;
  state.ipSettings = ip; renderIPSummary(); state.profiles = data.profiles || []; state.reports = data.reports || []; state.settings = data.settings;
  renderSelectors(); renderServices(); renderDetail(); renderReport();
}
function renderServices() {
  const target = $('service-list'); target.replaceChildren();
  const query = $('search').value.trim().toLowerCase(), status = $('status-filter').value;
  const list = state.profiles.filter(p => p.name.toLowerCase().includes(query) && (!status || p.status === status));
  $('service-count').textContent = `${state.profiles.length} service${state.profiles.length === 1 ? '' : 's'}`;
  if (!list.length) {
    const empty = el('div', undefined, 'empty panel'); empty.append(el('span', '+', 'empty-icon'), el('h2', state.profiles.length ? 'No matching services' : 'Your workspace starts here'), el('p', 'Add a website or app, record an access issue, and investigate the environment around it.'));
    empty.append(action('Add your first service', () => editProfile(), 'primary')); target.append(empty); return;
  }
  for (const p of list) {
    const card = el('article', undefined, `service-card${state.selected === p.id ? ' selected' : ''}`);
    const top = el('div', undefined, 'service-top'); top.append(el('span', Array.from(p.name)[0].toUpperCase(), 'service-letter'), el('span', p.status, `pill ${p.status}`));
    card.append(top, el('h2', p.name), el('p', p.origin || `${p.kind === 'app' ? 'App' : 'Website'} · No origin configured`));
    const bottom = el('div', undefined, 'card-bottom'); bottom.append(el('span', `${p.issues.filter(i => !i.resolved).length} open issues`), action('Open service ↗', () => selectProfile(p.id), 'text-button')); card.append(bottom); target.append(card);
  }
}
function renderDetail() {
  const p = profile(), box = $('service-detail'); box.hidden = !p; box.replaceChildren(); if (!p) return;
  const heading = el('div', undefined, 'panel-title'); const title = el('div'); title.append(el('p', 'SELECTED SERVICE', 'eyebrow'), el('h2', p.name)); const controls = el('div', undefined, 'actions');
  controls.append(action('Edit', () => editProfile(p)), action('Export', () => previewExport('profile')), action('Delete', () => confirmDelete('Delete service?', `This removes ${p.name}, ${p.issues.length} issues, and ${state.reports.filter(r => r.profileId === p.id).length} saved reports from the active store. Export first if needed.`, async () => { await api(`/api/profiles/${p.id}?revision=${p.revision}`, 'DELETE'); state.current.delete(p.id); selectProfile(''); await refresh(); notice('Service deleted from the active store. The previous-store backup may still contain it.'); }), 'text-button'));
  heading.append(title, controls); box.append(heading);
  box.append(el('p', p.notes || 'No notes yet.', 'detail-notes'), el('p', `Policy: ${p.policy.mode === 'builtin' ? p.policy.builtin + ' snapshot' : p.policy.mode} · Updated ${date(p.updatedAt)}`, 'small'));
  const actions = el('div', undefined, 'actions'); actions.append(action('Run diagnostics ↗', () => showView('diagnostics'), 'primary'), action('Research access rules ↗', openResearch), action('Share service template', () => previewExport('template')), action('+ Record issue', () => editIssue())); box.append(actions, el('h3', 'Issue journal'));
  if (!p.issues.length) box.append(el('p', 'Record registration, login, or access problems here.', 'small'));
  for (const issue of [...p.issues].reverse()) {
    const row = el('div', undefined, 'issue-row'); row.append(el('div', `${issue.stage} · ${date(issue.observedAt)} · ${issue.resolved ? 'Resolved' : 'Open'}`, 'small-row'), el('p', issue.error || 'No error message recorded.'), el('p', issue.notes, 'detail-notes'));
    const actions = el('div', undefined, 'actions'); actions.append(action('Edit issue', () => editIssue(issue), 'text-button'), action('Remove', () => confirmDelete('Remove issue?', 'This removes the selected journal entry from the active store.', async () => { const updated = structuredClone(p); updated.issues = updated.issues.filter(i => i.id !== issue.id); await api('/api/profiles', 'POST', updated); await refresh(); }), 'text-button')); row.append(actions); box.append(row);
  }
}
function editProfile(p, imported = false) {
  if (profileSaving || issueSaving) {
    notice('A save or refresh is still in progress. Wait for it to finish before opening another editor.');
    return;
  }
  ++profileGeneration;
  profileEdit = p ? structuredClone(p) : {name: '', kind: 'website', status: 'unchecked', origin: '', notes: '', policy: {mode: 'unknown'}, issues: []};
  $('profile-title').textContent = imported ? 'Review imported service' : p ? 'Edit service' : 'Add service';
  $('template-review-note').hidden = !imported;
  for (const key of ['name', 'kind', 'origin', 'status', 'notes']) $('profile-' + key).value = profileEdit[key];
  const policy = profileEdit.policy;
  $('policy-mode').value = policy.mode === 'builtin' ? policy.builtin : policy.mode;
  $('policy-countries').value = (policy.countries || []).join(', '); $('policy-source').value = policy.source || ''; $('policy-date').value = policy.checkedAt || '';
  $('policy-regions').value = Object.entries(policy.excludedRegions || {}).map(([c, r]) => `${c}: ${r.join(', ')}`).join('\n');
  updateProfileControls();
  $('profile-error').textContent = '';
  $('profile-dialog').showModal();
}
function updateProfileControls() {
  for (const id of ['profile-name', 'profile-kind', 'profile-origin', 'profile-status', 'profile-notes', 'policy-mode', 'profile-submit']) {
    $(id).disabled = profileSaving;
  }
  updatePolicyForm();
  $('profile-cancel').textContent = profileSaving ? 'Close editor' : 'Cancel';
  $('profile-save-status').textContent = profileSaving ? 'Saving service. Closing this editor does not cancel the save.' : '';
}
function closeProfileEditor() {
  ++profileGeneration;
  $('profile-dialog').close();
  if (profileSaving) notice('The service save will continue after this editor closes.');
}
function cancelTemplateImport(close = false) {
  ++templateGeneration; templateRun?.abort(); templateRun = null;
  $('template-submit').disabled = false; $('template-error').textContent = '';
  if (close) $('template-dialog').close();
}
function openTemplateImport() {
  if (profileSaving || issueSaving) {
    notice('A save or refresh is still in progress. Wait for it to finish before importing a service.');
    return;
  }
  cancelTemplateImport(); $('template-json').value = ''; $('template-dialog').showModal();
}
async function previewTemplate() {
  if (templateRun) return;
  let input;
  try { input = JSON.parse($('template-json').value); }
  catch { $('template-error').textContent = 'Paste one valid JSON service template.'; return; }
  const generation = ++templateGeneration, controller = new AbortController(); templateRun = controller;
  $('template-submit').disabled = true; $('template-error').textContent = '';
  try {
    const draft = await api('/api/templates/preview', 'POST', input, controller.signal);
    if (generation !== templateGeneration || controller.signal.aborted) return;
    $('template-dialog').close(); editProfile(draft, true);
  } catch(e) { if (generation === templateGeneration && e.name !== 'AbortError') $('template-error').textContent = e.message; }
  finally { if (generation === templateGeneration) { templateRun = null; $('template-submit').disabled = false; } }
}
function updateResearchControls() {
  const settings = researchSettings || state.settings;
  $('research-fields').disabled = !!researchRun || !researchProfile;
  $('research-summarize').disabled = !researchSettings || !assistantReady(settings);
  $('research-stop').hidden = !researchRun;
  $('research-model').textContent = assistantReady(settings) ? `Summary assistant: ${settings.provider} · ${settings.model}. Only this service's name, origin, and public extracts are sent.` : 'Configure an assistant and save its connection consent in Settings to generate a summary. Fetching sources works without a model.';
}
function cancelResearch() {
  ++researchGeneration; researchRun?.abort(); researchRun = null;
  $('research-status').textContent = 'Request canceled. Any question already sent cannot be recalled.';
  updateResearchControls();
}
async function openResearch() {
  const p = profile(); if (!p) return;
  cancelResearch(); researchProfile = null; researchSettings = {...state.settings}; $('research-title').textContent = `${p.name} · Access research`;
  $('research-urls').value = ''; $('research-summary').textContent = ''; $('research-sources').replaceChildren();
  $('research-status').textContent = 'Loading suggested sources…'; $('research-dialog').showModal();
  const generation = researchGeneration, controller = new AbortController(); researchRun = controller; updateResearchControls();
  try {
    const result = await api(`/api/profiles/${p.id}/research-sources`, 'GET', undefined, controller.signal);
    if (generation !== researchGeneration || state.selected !== p.id) return;
    if (result.revision !== p.revision) throw new Error('The service changed in another window. Reload this page before researching it.');
    researchProfile = {id:p.id, revision:p.revision}; $('research-urls').value = result.urls.join('\n');
    $('research-status').textContent = result.urls.length ? 'Review these public sources, then fetch them. No source has been contacted yet.' : 'Add this app’s official policy, help, or status URL. Its name alone is not enough to identify the correct service.';
  } catch(e) { if (generation === researchGeneration && e.name !== 'AbortError') $('research-status').textContent = e.message; }
  finally { if (generation === researchGeneration) { researchRun = null; updateResearchControls(); } }
}
async function runResearch(summarize) {
  const p = profile();
  if (researchRun || !researchProfile || !p || p.id !== researchProfile.id || p.revision !== researchProfile.revision) return;
  if (summarize && (!researchSettings || !sameAssistant(researchSettings, state.settings))) {
    populateSettings(); modelIdentityChanged(true, true); updateResearchControls();
    $('research-status').textContent = 'Assistant settings changed. Close this panel, review and save the connection in Settings, then reopen it. No question was sent.'; return;
  }
  if (summarize && !assistantReady(researchSettings)) return;
  const urls = $('research-urls').value.split('\n').map(v=>v.trim()).filter(Boolean);
  if (!urls.length || urls.length > 3) { $('research-status').textContent = 'Enter one to three public source URLs, one per line.'; return; }
  const payload = {profileId:p.id, revision:p.revision, urls};
  if (summarize) payload.settings = {...researchSettings};
  const generation = ++researchGeneration, controller = new AbortController(); researchRun = controller; updateResearchControls();
  $('research-summary').textContent = ''; $('research-sources').replaceChildren();
  $('research-status').textContent = summarize ? 'Fetching public sources, then asking your assistant to summarize…' : 'Fetching public sources…';
  try {
    const result = await api('/api/research', 'POST', payload, controller.signal);
    if (generation !== researchGeneration || controller.signal.aborted || state.selected !== p.id || profile()?.revision !== p.revision) return;
    $('research-summary').textContent = result.summary || result.summaryError || 'Sources fetched. No AI summary was requested.';
    result.sources.forEach((source,i) => {
      const item = el('article', undefined, 'research-source'), link = el('a', `[${i+1}] ${source.title || source.url}`);
      link.href = source.url; link.target = '_blank'; link.rel = 'noopener noreferrer';
      item.append(link, el('p', `${source.url} · Fetched ${date(source.fetchedAt)}`, 'small'));
      if (source.error) item.append(el('p', source.error, 'comparison-warning'));
      if (source.text) { const details = el('details'); details.append(el('summary', source.truncated ? 'Read extracted text · truncated' : 'Read extracted text'), el('pre', source.text)); item.append(details); }
      $('research-sources').append(item);
    });
    $('research-status').textContent = 'Finished. This is a source summary, not a diagnosis of your account. Results stay in memory; saved rules and scores are unchanged.';
  } catch(e) { if (generation === researchGeneration && e.name !== 'AbortError') $('research-status').textContent = e.message; }
  finally { if (generation === researchGeneration) { researchRun = null; updateResearchControls(); } }
}
function updatePolicyForm() {
  const mode = $('policy-mode').value, custom = ['custom', 'worldwide'].includes(mode);
  $('custom-policy').hidden = !custom;
  $('countries-label').hidden = mode === 'worldwide';
  for (const id of ['policy-source', 'policy-date', 'policy-regions']) $(id).disabled = profileSaving || !custom;
  $('policy-countries').disabled = profileSaving || mode !== 'custom';
}
async function saveProfile(event) {
  event.preventDefault();
  if (profileSaving) return;
  const generation = ++profileGeneration, selected = state.selected;
  profileSaving = true;
  updateProfileControls();
  $('profile-error').textContent = '';
  let persisted = false;
  try {
    const p = structuredClone(profileEdit);
    for (const key of ['name', 'kind', 'origin', 'status', 'notes']) p[key] = $('profile-' + key).value.trim();
    const mode = $('policy-mode').value;
    p.policy = mode.startsWith('claude-') ? {mode: 'builtin', builtin: mode} : {mode};
    if (['custom', 'worldwide'].includes(mode)) {
      p.policy.source = $('policy-source').value.trim();
      p.policy.checkedAt = $('policy-date').value;
      p.policy.countries = mode === 'custom' ? [...new Set($('policy-countries').value.toUpperCase().split(/[\s,]+/).filter(Boolean))] : [];
      p.policy.excludedRegions = {};
      for (const line of $('policy-regions').value.split('\n').filter(v => v.trim())) {
        const at = line.indexOf(':'); if (at < 0) throw new Error('Use one country per line, followed by a colon and comma-separated region names.');
        const code = line.slice(0, at).trim().toUpperCase(); p.policy.excludedRegions[code] = line.slice(at + 1).split(',').map(v => v.trim()).filter(Boolean);
      }
    }
    const saved = await api('/api/profiles', 'POST', p);
    persisted = true;
    if (generation === profileGeneration) {
      $('profile-dialog').close();
      resetChat();
    }
    await refresh();
    if (generation === profileGeneration && state.selected === selected) selectProfile(saved.id);
    notice('Service saved locally.');
  } catch (e) {
    if (persisted) notice(`Service saved, but the display could not refresh. Reload before making more changes. ${e.message}`, true);
    else if (generation === profileGeneration) $('profile-error').textContent = e.message;
    else notice(`Earlier service save failed: ${e.message}`, true);
  } finally {
    profileSaving = false;
    updateProfileControls();
  }
}
function editIssue(issue) {
  if (profileSaving || issueSaving) {
    notice('A save or refresh is still in progress. Wait for it to finish before opening another editor.');
    return;
  }
  ++issueGeneration;
  issueEdit = {profile: structuredClone(profile()), issue: issue ? structuredClone(issue) : {stage: 'registration', error: '', notes: '', resolved: false, observedAt: new Date().toISOString()}};
  for (const key of ['stage', 'error', 'notes']) $('issue-' + key).value = issueEdit.issue[key];
  $('issue-date').value = new Date(issueEdit.issue.observedAt).toISOString().slice(0, 16);
  $('issue-resolved').checked = issueEdit.issue.resolved;
  $('issue-validation').textContent = '';
  updateIssueControls();
  $('issue-dialog').showModal();
}
function updateIssueControls() {
  for (const id of ['issue-stage', 'issue-date', 'issue-error', 'issue-notes', 'issue-resolved', 'issue-submit']) $(id).disabled = issueSaving;
  $('issue-cancel').textContent = issueSaving ? 'Close editor' : 'Cancel';
  $('issue-save-status').textContent = issueSaving ? 'Saving issue. Closing this editor does not cancel the save.' : '';
}
function closeIssueEditor() {
  ++issueGeneration;
  $('issue-dialog').close();
  if (issueSaving) notice('The issue save will continue after this editor closes.');
}
async function saveIssue(event) {
  event.preventDefault();
  if (issueSaving) return;
  const generation = ++issueGeneration;
  issueSaving = true;
  updateIssueControls();
  $('issue-validation').textContent = '';
  let persisted = false;
  try {
    const {profile: p, issue} = structuredClone(issueEdit);
    for (const key of ['stage', 'error', 'notes']) issue[key] = $('issue-' + key).value;
    issue.observedAt = new Date($('issue-date').value + ':00Z').toISOString();
    issue.resolved = $('issue-resolved').checked;
    const index = p.issues.findIndex(i => i.id && i.id === issue.id);
    if (index < 0) p.issues.push(issue); else p.issues[index] = issue;
    await api('/api/profiles', 'POST', p);
    persisted = true;
    if (generation === issueGeneration) {
      $('issue-dialog').close();
      resetChat();
    }
    await refresh();
    notice('Issue saved locally.');
  } catch (e) {
    if (persisted) notice(`Issue saved, but the display could not refresh. Reload before making more changes. ${e.message}`, true);
    else if (generation === issueGeneration) $('issue-validation').textContent = e.message;
    else notice(`Earlier issue save failed: ${e.message}`, true);
  } finally {
    issueSaving = false;
    updateIssueControls();
  }
}
function confirmDelete(title, text, fn) { $('confirm-title').textContent = title; $('confirm-text').textContent = text; confirmAction = fn; $('confirm-dialog').showModal(); }
async function previewExport(kind) {
  exportKind = kind; $('export-notes').checked = false; $('export-notes-label').hidden = kind !== 'profile';
  $('export-description').textContent = kind === 'template' ? 'Share a reusable service definition. Templates exclude local IDs, status, notes, issues, reports, and settings. Review the name, origin, and policy text for private information. A source link does not verify the rules.' : 'Review this JSON before sharing. User-authored names, URLs, and policy text may identify your service.';
  if (await updateExport()) $('export-dialog').showModal();
}
async function updateExport() {
  const generation = ++exportGeneration, kind = exportKind;
  const profileId = state.selected, reportId = state.reportId, includeNotes = $('export-notes').checked;
  exportData = null; $('download-export').disabled = true; $('copy-export').disabled = true; $('export-status').textContent = '';
  $('export-preview').textContent = 'Preparing export…';
  const data = await api(kind === 'template' ? `/api/profiles/${profileId}/template` : kind === 'profile' ? `/api/profiles/${profileId}/export?includeNotes=${includeNotes}` : `/api/reports/${reportId}`);
  if (generation !== exportGeneration || kind !== exportKind || profileId !== state.selected || reportId !== state.reportId || includeNotes !== $('export-notes').checked) return false;
  exportData = data; $('export-preview').textContent = JSON.stringify(data, null, 2); $('download-export').disabled = false; $('copy-export').disabled = false;
  return true;
}
function downloadExport() {
  if (!exportData || $('download-export').disabled) return;
  const url = URL.createObjectURL(new Blob([JSON.stringify(exportData, null, 2)], {type: 'application/json'}));
  const link = el('a'); link.href = url; link.download = exportKind === 'template' ? 'service-template.json' : exportKind === 'profile' ? 'service-profile.json' : 'diagnostic-report.json'; link.click(); setTimeout(() => URL.revokeObjectURL(url), 1000);
}
async function copyExport() {
  if (!exportData || $('copy-export').disabled) return;
  const generation = exportGeneration;
  try {
    await navigator.clipboard.writeText(JSON.stringify(exportData, null, 2));
    if (generation === exportGeneration) $('export-status').textContent = 'JSON copied to your clipboard. Review it before sharing.';
  } catch {
    if (generation === exportGeneration) $('export-status').textContent = 'Clipboard unavailable. Select and copy the preview text manually.';
  }
}
function reportGuidance(r) {
  const signals = r.findings.filter(f => f.lower > 0).sort((a,b) => b.lower - a.lower);
  const unknown = r.findings.filter(f => f.lower === 0 && f.upper > 0).sort((a,b) => b.upper - a.upper);
  const clear = r.findings.filter(f => f.upper === 0);
  const next = [], seen = new Set();
  for (const finding of [...signals, ...unknown]) {
    const key = ['abuse','tor','proxy','vpn','datacenter','network-reputation'].includes(finding.id) ? 'ip' : ['egress','ipv6','webrtc'].includes(finding.id) ? 'routing' : ['timezone-ip','timezone-system','clock'].includes(finding.id) ? 'time' : finding.id;
    if (!seen.has(key)) { seen.add(key); next.push({key, finding}); }
  }
  return {signals, unknown, clear, next:next.slice(0,3)};
}
function findingAdvice(f) {
  if (f.id === 'network-reputation' || f.id === 'datacenter') return f.lower > 0
    ? 'Review hosting status and the company-network / ASN-wide abuse proportions alongside IP-level flags. These signals do not reveal the service’s private blocklist; check official access rules or ask service support.'
    : 'Enable IP intelligence for a network check, then review the validated network scopes and abuse proportions. Missing evidence and the service’s private blocklist remain unknown.';
  if (['abuse','tor','proxy','vpn'].includes(f.id)) return f.lower > 0
    ? 'Review this provider flag alongside the service’s official rules. A flag alone does not establish misuse or an account restriction.'
    : 'Review your IP intelligence configuration, then explicitly enable it for a network check. Failed lookups or missing provider fields remain unknown.';
  return f.recommendation;
}
function renderGuidance(r, guidance) {
  const section = el('section', undefined, 'report-guidance'); section.append(el('h3', 'What this check tells you'));
  const unresolved = Number((r.upper-r.lower).toFixed(1));
  section.append(el('p', `${Number(r.lower.toFixed(1))} points come from observed signals; up to ${unresolved} additional points remain unresolved. ${r.lower === 0 && unresolved > 0 ? 'A zero lower bound does not establish low risk.' : 'This does not establish account eligibility.'}`));
  section.append(el('p', 'Evidence coverage measures resolved rule weights, not a confidence percentage or the chance that registration will succeed.', 'small'));
  if (r.target?.summary) section.append(el('p', `Service response: ${r.target.summary} A reachable page does not confirm registration; HTTP 403 alone does not prove a region restriction.`, 'small'));
  if (!guidance.next.length) { section.append(el('p', 'No scored adverse signal was found. If access still fails, review the exact error and the service’s account requirements.', 'small')); return section; }
  section.append(el('h3', 'Next checks'), el('p', 'Observed signals come first, then the largest evidence gaps. Related checks share one step. Shortcuts open a panel; they do not run a check or send a question.', 'small'));
  const steps = el('ol', undefined, 'next-checks');
  for (const {key, finding:f} of guidance.next) {
    const step = el('li'); step.append(el('strong', f.title), el('p', `${f.lower > 0 ? 'Signal found' : 'Missing evidence'} · ${range(f)} / ${f.weight} points`, 'small'), el('p', findingAdvice(f)));
    if (key === 'region') step.append(action('Review official sources', openResearch, 'text-button'));
    if (key === 'ip') step.append(action('Review IP settings', () => { showView('settings'); $('ip-provider').focus(); $('ip-settings-form').scrollIntoView({block:'start'}); }, 'text-button'));
    steps.append(step);
  }
  section.append(steps); return section;
}
function renderFindingGroup(title, items, kind) {
  const section = el(kind === 'clear' ? 'details' : 'section', undefined, `finding-group ${kind}`);
  section.append(el(kind === 'clear' ? 'summary' : 'h3', `${title} (${items.length})`));
  const findings = el('div', undefined, 'findings');
  for (const f of items) {
    const item = el('article', undefined, 'finding'), head = el('div', undefined, 'panel-title'); head.append(el('h4', f.title), el('span', `${range(f)} / ${f.weight}`, 'points'));
    item.append(head, el('span', f.lower > 0 ? f.lower < f.upper ? 'Signal found · partly unresolved' : 'Signal found' : f.upper > 0 ? 'Unknown' : 'No adverse signal', 'pill'), el('p', f.explanation));
    if (f.upper > 0) item.append(el('p', findingAdvice(f)));
    if (f.source.startsWith('https://')) { const link = el('a', 'Evidence source ↗'); link.href = f.source; link.target = '_blank'; link.rel = 'noopener noreferrer'; item.append(link); } else item.append(el('small', f.source));
    findings.append(item);
  }
  section.append(findings); return section;
}
function renderReport() {
  renderComparison();
  const box = $('report-output'), r = report(); box.replaceChildren(); box.className = 'panel';
  if (!r) { box.className += ' empty'; box.append(el('span', '↗', 'empty-icon'), el('h2', 'Start with what you can observe'), el('p', 'Select a service, then run a check. No network request runs automatically.')); return; }
  const summary = el('div', undefined, 'score-summary'), score = el('div'); score.append(el('div', 'ACCESS RISK RANGE', 'score-label')); const number = el('div', range(r), 'score-number'); number.append(el('span', ' / 100', 'score-max')); score.append(number);
  const text = el('div'); text.append(el('h2', r.grade), el('p', 'Higher means more observed risk. The range includes missing evidence; it is not a ban probability.', 'small'));
  const coverage = el('div', undefined, 'coverage'), progress = el('progress'); progress.max = 100; progress.value = r.coverage; progress.setAttribute('aria-label', 'Evidence coverage'); coverage.append(progress, el('span', `${Math.round(r.coverage)}% evidence coverage`)); text.append(coverage); summary.append(score, text); box.append(summary);
  box.append(el('p', `${date(r.createdAt)} · Score v${r.scoreVersion} · Service revision ${r.profileRevision} · ${state.reports.some(v => v.id === r.id) ? 'Saved redacted snapshot' : 'In memory — not saved'}`, 'small'));
  if (r.profileRevision !== profile()?.revision) box.append(el('p', 'This is a historical snapshot. The service has changed since this check.', 'error-text'));
  const actions = el('div', undefined, 'actions');
  const saved = state.reports.some(v => v.id === r.id);
  actions.append(action(saved ? 'Saved locally' : 'Save redacted report', async () => { await api('/api/reports', 'POST', {profileId: r.profileId, reportId: r.id}); await refresh(); notice('Redacted report saved. Raw evidence was excluded.'); }, 'secondary')); actions.firstChild.disabled = saved;
  actions.append(action('Preview export', () => previewExport('report')), action('Discuss with assistant ↗', () => { $('assistant-mode').value = 'report'; resetChat(); showView('assistant'); }, 'primary'));
  if (saved) actions.append(action('Delete saved report', () => confirmDelete('Delete saved report?', 'This removes the saved summary from the active store. A current in-memory check may remain until restart.', async () => { await api(`/api/reports/${r.id}`, 'DELETE'); resetChat(); await refresh(); }), 'text-button'));
  box.append(actions);
  const guidance = reportGuidance(r); box.append(renderGuidance(r, guidance));
  if (guidance.signals.length) box.append(renderFindingGroup('Risk signals found', guidance.signals, 'signals'));
  if (guidance.unknown.length) box.append(renderFindingGroup('Still unknown', guidance.unknown, 'unknown'));
  if (guidance.clear.length) box.append(renderFindingGroup('No adverse signal in these checks', guidance.clear, 'clear'));
  const evidence = el('details', undefined, 'evidence'); evidence.append(el('summary', r.evidence ? 'Local evidence · raw IPs are not saved or sent to the assistant' : 'Snapshot details · raw evidence was not retained'));
  const dl = el('dl'); const row = (key, value) => dl.append(el('dt', key), el('dd', value || 'Unknown'));
  row('Operating system', `${r.os} / ${r.arch}`); row('Policy', `${r.policy.mode} · ${r.policy.provenance || 'Unknown'} · reviewed ${r.policy.checkedAt || 'never'}`);
  if (r.evidence) {
    const e = r.evidence; row('System timezone', `${e.system.timezone} · UTC offset ${e.system.offsetMinutes ?? 'unknown'} minutes`); row('System locale', e.system.locale); row('Browser timezone', e.browser.timezone); row('Browser languages', e.browser.languages.join(', ')); row('Proxy', e.system.proxy); row('DNS', e.system.dns); row('Default route', e.system.route);
    const flag = value => value === true ? 'Reported' : value === false ? 'Not reported' : 'Unknown';
    const proportion = value => {
      if (typeof value !== 'number' || !Number.isFinite(value) || value < 0 || value > 1) return 'Unknown';
      if (value > 0 && value < 0.000001) return '<0.0001%';
      return `${Number((value * 100).toFixed(4))}%`;
    };
    (e.exits || []).forEach(x => {
      const label = `${x.path} / ${x.family}`;
      row(label, x.ip ? `${x.ip} · ${x.intelligence?.country || 'country unknown'}` : x.error || 'Unavailable');
      if (!x.intelligence) return;
      const v = x.intelligence;
      row(`${label} · IP-level abuse flag`, flag(v.abuse));
      row(`${label} · Hosting / datacenter flag`, flag(v.datacenter));
      row(`${label} · Company / type`, [v.companyName, v.companyType].filter(Boolean).join(' · '));
      row(`${label} · Validated company network`, v.companyNetwork);
      row(`${label} · Company-network abuse proportion`, proportion(v.companyAbuseRatio));
      row(`${label} · ASN / organization / type`, [v.asn, v.asnOrganization, v.asnType].filter(Boolean).join(' · '));
      row(`${label} · Validated ASN route containing exit`, v.asnRoute);
      row(`${label} · ASN-wide abuse proportion (all ASN routes)`, proportion(v.asnAbuseRatio));
    });
    row('Service-private blocklist', 'Unknown — IP intelligence does not report the selected service’s private denylist.');
    row('Clock skew', e.clockSkewSeconds === null ? 'Unknown' : `${e.clockSkewSeconds.toFixed(1)} seconds`);
    row('Collection notes', [...(e.warnings || []), ...(e.system.warnings || [])].join(' '));
  }
  row('Service reachability', r.target.summary || 'Not checked'); evidence.append(dl); box.append(evidence);
}
function compareReports(before, after) {
  if (!before || !after || before.id === after.id || before.profileId !== after.profileId) return {error: 'Choose two different reports from the same service.'};
  if (before.scoreVersion !== after.scoreVersion) return {error: 'These reports use different scoring versions. Their scores cannot be compared directly.'};
  const warnings = [], rows = [];
  if (before.coverage !== after.coverage) warnings.push('Evidence coverage changed. A lower score may reflect missing checks, not an improvement.');
  if (before.profileRevision !== after.profileRevision) warnings.push('The service configuration changed between these checks.');
  if (JSON.stringify(before.policy) !== JSON.stringify(after.policy)) warnings.push('The region policy or its review date changed between these checks.');
  const row = (label, a, b) => rows.push({label, before: a, after: b, changed: a !== b});
  row('Risk range', range(before), range(after));
  row('Evidence coverage', `${before.coverage}%`, `${after.coverage}%`);
  const old = new Map(before.findings.map(f => [f.id, f])), next = new Map(after.findings.map(f => [f.id, f]));
  const describe = f => f ? `${range(f)} / ${f.weight} · ${f.state}` : 'Not present · unknown';
  for (const id of new Set([...old.keys(), ...next.keys()])) {
    const a = old.get(id), b = next.get(id);
    row(b?.title || a.title, describe(a), describe(b));
    if (a && b && a.source !== b.source) warnings.push(`Evidence source changed for ${b.title}.`);
  }
  const probe = value => value ? `${value.state || 'unknown'} · ${value.summary || 'No details'}${value.status ? ` · HTTP ${value.status}` : ''}${value.milliseconds ? ` · ${value.milliseconds} ms` : ''}` : 'Not checked · unknown';
  row('Service reachability', probe(before.target), probe(after.target));
  return {warnings, rows};
}
function renderComparison() {
  const current = report(), panel = $('report-comparison'), select = $('comparison-report'), output = $('comparison-output');
  panel.hidden = !current; output.replaceChildren();
  if (!current) return;
  const selected = select.value;
  const candidates = reports().filter(r => r.id !== current.id && Date.parse(r.createdAt) <= Date.parse(current.createdAt)).sort((a,b) => Date.parse(b.createdAt) - Date.parse(a.createdAt));
  select.replaceChildren(new Option('Choose an earlier report', ''));
  candidates.forEach(r => select.append(new Option(`${date(r.createdAt)} · ${range(r)} · ${r.coverage}% coverage`, r.id)));
  select.value = candidates.some(r => r.id === selected) ? selected : ''; select.disabled = !candidates.length;
  const before = candidates.find(r => r.id === select.value);
  if (!before) { output.append(el('p', candidates.length ? 'Choose a baseline to see what changed.' : 'Save this report, then run another check to compare. No previous snapshot is available.', 'small')); return; }
  const result = compareReports(before, current);
  if (result.error) { output.append(el('p', result.error, 'error-text')); return; }
  result.warnings.forEach(message => output.append(el('p', message, 'comparison-warning')));
  output.append(el('p', `${result.rows.filter(r => r.changed).length} comparison rows changed. This does not establish the cause of an access problem.`, 'small'));
  const wrap = el('div', undefined, 'comparison-table'), table = el('table'), caption = el('caption', `Before: ${date(before.createdAt)} → After: ${date(current.createdAt)}`), head = el('thead'), header = el('tr');
  for (const title of ['Signal', 'Before', 'After', 'Change']) { const th = el('th', title); th.scope = 'col'; header.append(th); }
  head.append(header); table.append(caption, head);
  const body = el('tbody');
  for (const r of result.rows) { const tr = el('tr'), label = el('th', r.label); label.scope = 'row'; tr.append(label, el('td', r.before), el('td', r.after), el('td', r.changed ? 'Changed' : 'Unchanged')); body.append(tr); }
  table.append(body); wrap.append(table); output.append(wrap);
}
function browserEvidence() { return {timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || '', offsetMinutes: -new Date().getTimezoneOffset(), languages: Array.from(navigator.languages || [navigator.language]).slice(0, 20)}; }
async function browserExit(family, signal) {
  const endpoint = family === 'ipv4' ? 'https://api.ipify.org?format=json' : 'https://api6.ipify.org?format=json';
  try {
    const response = await fetch(endpoint, {signal: AbortSignal.any([signal, AbortSignal.timeout(8000)]), credentials: 'omit', cache: 'no-store', redirect: 'error', referrerPolicy: 'no-referrer'});
    if (!response.ok) throw new Error(); const data = await response.json();
    if (typeof data.ip !== 'string' || data.ip.length > 64) throw new Error();
    return {path: 'browser', family, ip: data.ip};
  } catch { return {path: 'browser', family, error: 'Browser probe unavailable or timed out.'}; }
}
// Ignore host/private and mDNS candidates; the server validates public addresses again.
function candidateAddress(raw) {
  if (/^\d+\.\d+\.\d+\.\d+$/.test(raw)) {
    const [a,b,c,d] = raw.split('.').map(Number);
    if ([a,b,c,d].some(v => v > 255) || a === 0 || a === 10 || a === 127 || a >= 224 || a === 100 && b >= 64 && b <= 127 || a === 169 && b === 254 || a === 172 && b >= 16 && b <= 31 || a === 192 && (b === 168 || b === 0 || b === 88 && c === 99) || a === 198 && (b === 18 || b === 19 || b === 51 && c === 100) || a === 203 && b === 0 && c === 113) return '';
    return 'ipv4';
  }
  if (/^[23][0-9a-f]{3}:[0-9a-f:]+$/i.test(raw) && !/^(2001:(db8|0|:)|2002:)/i.test(raw)) return 'ipv6';
  return '';
}
async function rtcExits(signal) {
  if (!window.RTCPeerConnection || signal.aborted) return [];
  const pc = new RTCPeerConnection({iceServers: [{urls: session.stun}]}); const found = [];
  return new Promise(resolve => {
    let finished = false;
    const done = () => { if (finished) return; finished = true; clearTimeout(timer); signal.removeEventListener('abort', done); pc.close(); resolve(found); };
    const timer = setTimeout(done, 5000); signal.addEventListener('abort', done, {once: true});
    pc.onicecandidate = event => {
      if (!event.candidate) { done(); return; }
      const ip = event.candidate.address || event.candidate.candidate.split(' ')[4], family = candidateAddress(ip || '');
      if (family && found.length < 4 && !found.some(x => x.ip === ip)) found.push({path: 'webrtc', family, ip});
    };
    pc.createDataChannel('diagnostics'); pc.createOffer().then(offer => pc.setLocalDescription(offer)).catch(done);
  });
}
async function runChecks(network) {
  const p = profile(); if (!p || state.run) return;
  const controller = new AbortController(), generation = ++state.generation; state.run = controller; updateRunButtons(); notice('');
  const timer = setTimeout(() => controller.abort(), 55000);
  $('run-progress').textContent = network ? 'Observing browser exits…' : 'Reading local environment…';
  try {
    const input = {profileId: p.id, revision: p.revision, browser: browserEvidence(), exits: [], network, intelligence: network && $('intelligence-check').checked, ipSettingsRevision: state.ipSettings.revision || 0, webrtc: network && $('webrtc-check').checked, target: network && $('target-check').checked};
    if (network) {
      const results = await Promise.all([browserExit('ipv4', controller.signal), browserExit('ipv6', controller.signal), input.webrtc ? rtcExits(controller.signal) : []]);
      input.exits = [results[0], results[1], ...results[2]];
    }
    if (controller.signal.aborted || generation !== state.generation) return;
    $('run-progress').textContent = 'Checking system and agent evidence…';
    const result = await api('/api/run', 'POST', input, controller.signal);
    if (generation !== state.generation || p.id !== state.selected || controller.signal.aborted) return;
    state.current.set(p.id, result); state.reportId = result.id; resetChat(); renderSelectors(); renderReport(); $('run-progress').textContent = 'Check complete. Review unknowns before drawing conclusions.';
  } catch (e) { if (generation === state.generation) { $('run-progress').textContent = controller.signal.aborted ? 'Check stopped or timed out. No partial report saved.' : 'Check failed.'; if (!controller.signal.aborted) notice(e.message, true); } }
  finally { clearTimeout(timer); if (generation === state.generation) { state.run = null; updateRunButtons(); } }
}
function populateSettings() { $('model-provider').value = state.settings.provider; $('model-endpoint').value = state.settings.endpoint; $('model-id').value = state.settings.model; $('local-confirmed').checked = state.settings.localConfirmed; $('cli-path').value = state.settings.cliPath || ''; $('cloud-confirmed').checked = !!state.settings.cloudConfirmed; updateModelFields(); }
function isCLI(settings = state.settings) { return ['claude-cli', 'codex-cli'].includes(settings.provider); }
function updateModelFields() {
  const cli = isCLI({provider: $('model-provider').value}), manual = $('model-provider').value === 'codex-cli';
  $('model-endpoint-label').hidden = cli; $('model-endpoint').required = !cli;
  $('cli-options').hidden = !cli; $('local-consent').hidden = cli; $('cli-consent').hidden = !cli || manual; $('cli-path-label').hidden = manual; $('model-id-label').hidden = manual; $('list-models').hidden = manual;
  $('model-help').textContent = manual ? 'Codex uses manual handoff: preview and copy the report prompt, then paste it into your own Codex client. Automatic Codex execution is unavailable because tool isolation is not verified. No context is sent by this app.' : cli ? 'Uses an installed CLI. An empty model ID uses the CLI default. CLI version and policy compatibility are checked before sending content.' : 'Only loopback endpoints are accepted. Compatible servers may use /v1. No model is downloaded.';
  $('list-models').textContent = cli ? 'Save & check CLI compatibility' : 'Save connection & list models';
  $('model-id').placeholder = cli ? 'default, or an explicit model ID' : 'Choose an installed model';
}
async function detectCLIs() {
  if ($('model-fields').disabled) return;
  $('model-fields').disabled = true; $('cli-results').textContent = 'Checking installed CLI versions…';
  try {
    const results = await api('/api/cli/discover', 'POST', {}); $('cli-results').replaceChildren();
    for (const item of results) $('cli-results').append(el('p', `${item.provider}: ${item.version || 'Version unavailable'} · ${item.path || 'Not found'} · ${item.detail || (item.available ? 'Available' : 'Unavailable')}`));
  } finally { $('model-fields').disabled = false; }
}
function renderIPSummary() {
  const ip = state.ipSettings || {}, ready = !!ip.ready && !state.ipDirty;
  $('intelligence-check').disabled = !ready;
  if (!ready) $('intelligence-check').checked = false;
  $('key-state').textContent = state.ipDirty ? 'Save IP settings first' : ready ? (ip.provider === 'custom' ? 'Custom endpoint configured' : 'Provider key configured') : 'Configure IP intelligence in Settings';
  $('ip-destination').textContent = `IP intelligence sends observed public IPs to ${ip.endpoint || 'the configured provider'} and may consume provider quota.`;
  $('ip-key-status').textContent = `Saved configuration: ${ip.keyConfigured ? 'Key configured' : 'No key configured'} · Source: ${ip.keySource || 'none'}.`;
}
function populateIPSettings() {
  const ip = state.ipSettings || {};
  $('ip-provider').value = ip.provider || 'ipapi'; $('ip-endpoint').value = ip.endpoint || 'https://api.ipapi.is/';
  $('ip-endpoint').readOnly = $('ip-provider').value === 'ipapi';
  $('ip-api-key').value = ''; $('ip-clear-key').checked = false;
  state.ipEditRevision = ip.revision || 0; state.ipDirty = false; renderIPSummary();
}
function ipIdentityChanged(providerChanged) {
  if (providerChanged) { $('ip-endpoint').value = $('ip-provider').value === 'ipapi' ? 'https://api.ipapi.is/' : ''; $('ip-endpoint').readOnly = $('ip-provider').value === 'ipapi'; }
  $('ip-api-key').value = ''; $('ip-clear-key').checked = false;
  markIPDirty(); $('ip-status').textContent = 'Destination changed. Enter a new key if required, then save. The previous key will not be reused for a different endpoint.';
}
function markIPDirty() { state.ipDirty = true; $('intelligence-check').checked = false; renderIPSummary(); }
async function saveIPSettings() {
  if ($('ip-fields').disabled) return;
  $('ip-fields').disabled = true; $('ip-status').textContent = 'Saving IP settings…';
  try {
    state.ipSettings = await api('/api/ip-settings', 'POST', {provider: $('ip-provider').value, endpoint: $('ip-endpoint').value.trim(), apiKey: $('ip-api-key').value, clearKey: $('ip-clear-key').checked, revision: state.ipEditRevision || 0});
    populateIPSettings(); $('intelligence-check').checked = false;
    $('ip-status').textContent = 'Saved. Enable IP intelligence in Diagnostics when you are ready to send a lookup.';
  } catch(e) { $('ip-status').textContent = 'Could not save. If another window changed settings, reload this page before retrying.'; throw e; }
  finally { $('ip-fields').disabled = false; }
}

function modelIdentityChanged(connectionChanged, preserveDraft = false) {
  state.settingsDirty = true; $('local-confirmed').checked = false; $('cloud-confirmed').checked = false; resetChat(preserveDraft);
  if (connectionChanged) { $('available-models').replaceChildren(); $('model-list').replaceChildren(); }
  $('model-status').textContent = 'Settings changed. Review the connection consent and save before chatting.';
}
async function saveSettings(discover = false) {
  if ($('model-fields').disabled) return;
  $('model-fields').disabled = true;
  $('model-status').textContent = discover ? 'Saving and connecting…' : 'Saving…';
  try {
    const cli = isCLI({provider: $('model-provider').value});
    state.settings = await api('/api/settings', 'POST', {provider: $('model-provider').value, endpoint: cli ? '' : $('model-endpoint').value.trim(), model: $('model-id').value.trim(), cliPath: cli ? $('cli-path').value.trim() : '', localConfirmed: !cli && $('local-confirmed').checked, cloudConfirmed: cli && $('cloud-confirmed').checked});
    state.settingsDirty = false; resetChat(); renderSelectors();
    $('model-status').textContent = 'Settings saved.';
    notice('Assistant settings saved. Direct chat is ready after connection consent; report attachments require a preview.');
    if (discover) {
      const models = await api('/api/models'); $('available-models').replaceChildren(); $('model-list').replaceChildren();
      for (const m of models) { if (!m.locality.startsWith('Remote')) $('available-models').append(new Option(m.id, m.id)); $('model-list').append(el('p', `${m.id} — ${m.locality}`)); }
      $('model-status').textContent = isCLI() ? 'CLI compatible. Use default or enter a model ID; account access has not been checked.' : models.length ? `${models.length} models found. Choose an ID below.` : 'No models available. Load a model in your runtime first.';
    }
  } catch (e) { $('model-status').textContent = 'Request failed. Check the settings and your local runtime.'; throw e; }
  finally { $('model-fields').disabled = false; }
}
async function listModels() { await saveSettings(true); }
function sameAssistant(a, b) {
  return ['provider', 'endpoint', 'model', 'cliPath'].every(key => (a[key] || '') === (b[key] || '')) && !!a.localConfirmed === !!b.localConfirmed && !!a.cloudConfirmed === !!b.cloudConfirmed;
}
async function previewContext(expand = true) {
  if (state.settingsDirty) throw new Error('Save the changed model settings before previewing context.');
  if (state.chat || state.preparing || (!directChat() && !report())) return;
  const direct = directChat();
  const input = {mode: direct ? 'direct' : 'report', stage: $('triage-stage').value, errorText: $('triage-error').value};
  if (!direct) Object.assign(input, {profileId: state.selected, reportId: state.reportId, notes: $('include-notes').checked ? $('assistant-notes').value : ''});
  resetChat(true); const generation = state.chatGeneration, controller = new AbortController(); state.preparing = controller; updateChatControls();
  try {
    const context = await api('/api/context', 'POST', input, controller.signal);
    if (generation !== state.chatGeneration || controller.signal.aborted) return;
    if (!sameAssistant(context.settings, state.settings)) {
      state.settings = context.settings; populateSettings(); modelIdentityChanged(true, true); renderSelectors();
      throw new Error('Assistant settings changed in another window. Review the connection and save in Settings before sending.');
    }
    state.context = context; $('context-preview').textContent = JSON.stringify({model: context.settings, systemPrompt: context.systemPrompt, context: context.context}, null, 2); $('context-details').hidden = false; $('context-details').open = expand;
    $('copy-context').disabled = false;
    $('chat-progress').textContent = context.delivery === 'manual' ? 'Copy the reviewed prompt into your Codex client. This app does not send it.' : assistantReady() ? 'Context ready. Your question will be sent to this assistant.' : 'Configure the assistant and review its consent in Settings first.';
    return context;
  } finally { if (generation === state.chatGeneration) { state.preparing = null; updateChatControls(); } }
}
async function copyContext() {
  if (!state.context || $('copy-context').disabled) return;
  const context = state.context, generation = state.chatGeneration;
  const prompt = `${context.systemPrompt}\n\nReviewed conversation context:\n${context.context}\n\nQuestion:\n${$('chat-input').value.trim() || 'Explain the findings and suggest manual next steps.'}`;
  try { await navigator.clipboard.writeText(prompt); if (generation === state.chatGeneration) $('copy-context-status').textContent = 'Reviewed prompt copied. Paste it into the assistant you choose; that service’s data policy applies.'; }
  catch { if (generation === state.chatGeneration) $('copy-context-status').textContent = 'Clipboard unavailable. Copy the displayed context manually.'; }
}
function addMessage(role, text) { if (!$('chat-history').querySelector('.message')) $('chat-history').replaceChildren(); const node = el('div', undefined, `message ${role}`); node.append(el('strong', role === 'user' ? 'You' : 'Assistant'), document.createTextNode(text)); $('chat-history').append(node); }
async function sendChat(event) {
  event.preventDefault(); if (state.chat || state.preparing || !assistantReady()) return;
  const question = $('chat-input').value.trim(); if (!question) return;
  if (!state.context) {
    if (!directChat()) return;
    const context = await previewContext(false);
    if (!context || state.context !== context || !assistantReady()) return;
  }
  const controller = new AbortController(), generation = state.chatGeneration, context = state.context; state.chat = controller;
  updateChatControls(); $('chat-progress').textContent = 'Waiting for assistant…';
  const started = Date.now(), timer = setInterval(() => { if (generation === state.chatGeneration) $('chat-progress').textContent = `Waiting for assistant… ${Math.floor((Date.now() - started) / 1000)}s`; }, 1000);
  try {
    const response = await api('/api/chat', 'POST', {contextId: context.id, message: question}, controller.signal);
    if (generation !== state.chatGeneration || controller.signal.aborted) return;
    addMessage('user', question); addMessage('assistant', response.reply); $('chat-input').value = ''; $('chat-progress').textContent = 'Response complete. Conversation is not saved.';
  } catch (e) { if (generation === state.chatGeneration) { resetChat(true); notice(controller.signal.aborted ? 'Response stopped. Start a fresh conversation.' : e.message, !controller.signal.aborted); } }
  finally { clearInterval(timer); if (generation === state.chatGeneration) { state.chat = null; updateChatControls(); } }
}
function bindEvents() {
  document.querySelectorAll('[data-view]').forEach(b => b.addEventListener('click', () => showView(b.dataset.view)));
  document.querySelectorAll('[data-close]').forEach(b => b.addEventListener('click', () => {
    if (b.dataset.close === 'profile-dialog') closeProfileEditor();
    else if (b.dataset.close === 'issue-dialog') closeIssueEditor();
    else $(b.dataset.close).close();
  }));
  window.addEventListener('hashchange', () => showView(location.hash.slice(1)));
  $('add-service').onclick = () => editProfile(); $('profile-form').onsubmit = saveProfile; $('issue-form').onsubmit = saveIssue; $('policy-mode').onchange = updatePolicyForm;
  $('profile-dialog').oncancel = event => { event.preventDefault(); closeProfileEditor(); };
  $('issue-dialog').oncancel = event => { event.preventDefault(); closeIssueEditor(); };
  $('import-template').onclick = openTemplateImport; $('template-submit').onclick = () => perform(previewTemplate);
  $('template-close').onclick = () => cancelTemplateImport(true); $('template-cancel').onclick = () => cancelTemplateImport(true);
  $('template-json').oninput = () => cancelTemplateImport(); $('template-dialog').oncancel = () => cancelTemplateImport();
  $('search').oninput = renderServices; $('status-filter').onchange = renderServices;
  $('confirm-cancel').onclick = () => $('confirm-dialog').close(); $('confirm-action').onclick = () => perform(async () => { $('confirm-dialog').close(); await confirmAction(); });
  $('export-notes').onchange = () => perform(updateExport); $('download-export').onclick = downloadExport; $('copy-export').onclick = () => perform(copyExport);
  for (const id of ['diagnostic-service', 'assistant-service']) $(id).onchange = () => selectProfile($(id).value);
  for (const id of ['diagnostic-report', 'assistant-report']) $(id).onchange = () => { resetChat(); state.reportId = $(id).value; renderSelectors(); renderReport(); };
  $('run-local').onclick = () => perform(() => runChecks(false)); $('run-network').onclick = () => perform(() => runChecks(true)); $('cancel-run').onclick = cancelRun;
  $('settings-form').onsubmit = event => { event.preventDefault(); perform(saveSettings); }; $('list-models').onclick = () => perform(listModels);
  $('model-provider').onchange = () => { $('model-endpoint').value = $('model-provider').value === 'ollama' ? 'http://127.0.0.1:11434' : 'http://127.0.0.1:1234/v1'; $('model-id').value = ''; $('cli-path').value = ''; updateModelFields(); modelIdentityChanged(true); };
  $('model-endpoint').oninput = () => modelIdentityChanged(true); $('cli-path').oninput = () => modelIdentityChanged(true); $('detect-cli').onclick = () => perform(detectCLIs); $('model-id').oninput = () => modelIdentityChanged(false);
  for (const id of ['local-confirmed','cloud-confirmed']) $(id).onchange = () => { state.settingsDirty = true; resetChat(); };
  $('ip-provider').onchange = () => ipIdentityChanged(true); $('ip-endpoint').oninput = () => ipIdentityChanged(false); $('ip-api-key').oninput = markIPDirty; $('ip-clear-key').onchange = () => { if ($('ip-clear-key').checked) $('ip-api-key').value = ''; markIPDirty(); };
  $('ip-settings-form').onsubmit = event => { event.preventDefault(); perform(saveIPSettings); };
  $('include-notes').onchange = () => { resetChat(); $('assistant-notes').hidden = !$('include-notes').checked; if ($('include-notes').checked) { const p = profile(); $('assistant-notes').value = [p?.notes, ...(p?.issues || []).filter(i => !i.resolved).map(i => `${i.stage}: ${i.error}\n${i.notes}`)].filter(Boolean).join('\n\n').slice(0, 8192); } };
  $('copy-context').onclick = () => perform(copyContext);
  $('assistant-notes').oninput = () => resetChat(true); $('preview-context').onclick = () => perform(previewContext); $('chat-form').onsubmit = event => perform(() => sendChat(event));
  $('stop-chat').onclick = () => { resetChat(true); notice('Response stopped. Start a fresh conversation, or preview the report again.'); };
  $('assistant-mode').onchange = () => resetChat(true); $('new-chat').onclick = () => resetChat();
  $('triage-stage').onchange = () => resetChat(true); $('triage-error').oninput = () => resetChat(true);
  $('comparison-report').onchange = renderComparison;
  $('research-fetch').onclick = () => perform(() => runResearch(false)); $('research-summarize').onclick = () => perform(() => runResearch(true));
  $('research-stop').onclick = cancelResearch; $('research-close').onclick = () => { cancelResearch(); $('research-dialog').close(); };
  $('research-dialog').oncancel = cancelResearch;
  $('research-urls').oninput = () => { cancelResearch(); $('research-summary').textContent = ''; $('research-sources').replaceChildren(); $('research-status').textContent = 'Source URLs changed. Fetch again to update the summary.'; };
}
async function init() {
  bindEvents(); showView(location.hash.slice(1) || 'services'); session = await api('/api/session');
  $('transport').textContent = session.transport;
  await refresh(); populateSettings(); populateIPSettings();
}
perform(init);
