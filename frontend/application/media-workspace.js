// @ts-check

import {cancelPrimeGeneration,confirmPrimeSelection,createLocalGeneration,createPrimeGeneration,createTMDBGeneration,deletePrimeProvider,exportMediaCSV,getMediaReport,getNetflixProvider,getPrimeProvider,setNetflixProfileLabel,uploadPrimeArchive,uploadViewingActivity} from './api.js';
import {element} from './dom.js';
import {MEDIA_COPY} from './media-copy.js';

/** @type {import('./api.js').MediaReport|null} */
let report = null;
/** @type {import('./api.js').PrimeSnapshot|null} */
let prime = null;
let netflix = null;
/** @type {Record<string,string>} */
let filter = {provider:'all',timezone:'UTC',title:'',start_date:'',end_date:'',kind:'all',match_status:'all'};
let view = 'overview';
let busy = false;
let error = '';
let enabled = false;
let pollTimer = 0;
/** @type {AbortController|null} */
let controller = null;
let redraw = () => {};
/** @type {string[]} */
let cursors = [];
let cursor = '';
let titlesCursor = '';
/** @type {string[]} */
let titlesCursors = [];
const objectURLs = new Set();
const pendingConfirmations = new Set();
const VIEWS = ['overview','titles','activity','sources'];
const LOCALE_TO_TMDB = {en:'en-US',es:'es-ES',fr:'fr-FR',ru:'ru-RU'};

export function clearMediaWorkspace() {
  enabled = false;
  for (const dismiss of pendingConfirmations) dismiss();
  controller?.abort();
  controller = null;
  window.clearTimeout(pollTimer);
  pollTimer = 0;
  for (const url of objectURLs) URL.revokeObjectURL(url);
  objectURLs.clear();
  report = null;
  prime = null;
  netflix = null;
  busy = false;
  error = '';
  view = 'overview';
  cursors = [];
  cursor = '';
  titlesCursor = '';
  titlesCursors = [];
  filter = {provider:'all',timezone:'UTC',title:'',start_date:'',end_date:'',kind:'all',match_status:'all'};
}

export async function hydrateMediaWorkspace(signal) {
  enabled = true;
  const providers = await Promise.all([getPrimeProvider(signal),getNetflixProvider(signal)]);
  const currentReport = await getMediaReport(filter,signal);
  if (signal.aborted || !enabled) return;
  [prime,netflix] = providers;
  report = currentReport;
  schedulePoll();
}

function schedulePoll() {
  window.clearTimeout(pollTimer);
  if (!enabled) return;
  const running = prime?.building_generation?.state === 'enriching' || (netflix?.building_generation && !['failed','receiving'].includes(netflix.building_generation.state));
  if (running) pollTimer = window.setTimeout(() => { void refresh().catch(showError); },1000);
}

async function refresh() {
  if (!enabled || busy) { schedulePoll(); return; }
  controller?.abort();
  const request = new AbortController();
  controller = request;
  const providers = await Promise.all([getPrimeProvider(request.signal),getNetflixProvider(request.signal)]);
  const currentReport = await getMediaReport(filter,request.signal,cursor,titlesCursor);
  if (request.signal.aborted || !enabled) return;
  [prime,netflix] = providers;
  report = currentReport;
  error = '';
  redraw();
  schedulePoll();
}

function showError(failure) {
  if (!enabled || failure.name === 'AbortError') return;
  error = failure.code || 'request_failed';
  redraw();
}

function button(text,action,attributes = {}) {
  return element('button',{type:'button',class:'button','data-media-action':action,disabled:busy,...attributes},text);
}
function field(text,id,node) {
  return element('label',{class:'field',for:id},element('span',{text}),node);
}
function textInput(id,name,value,type='text') { return element('input',{id,name,type,value}); }
function providerName(provider) { return provider==='netflix' ? 'Netflix' : 'Prime Video'; }
function statusLabel(status,copy) {
  const names = {not_enriched:copy.notEnriched,receiving:copy.state_receiving,validating:copy.state_validating,awaiting_confirmation:copy.readyPreview,importing:copy.state_importing,enriching:copy.state_enriching,ready:copy.state_ready_private,failed:copy.state_failed,watch_summary:copy.watchEvents,search:copy.searches,purchase:copy.purchases,deleted:copy.deleted,unknown_duration:copy.unknownDuration,zero_duration:`${copy.seconds}: 0`};
  const label = names[status] ?? copy[status];
  if (typeof label!=='string') throw new Error('media classification has no localized label');
  return label;
}

/** Render validated report state and attach intent handlers to this workspace only. */
export function renderMediaWorkspace(common,locale,onRender) {
  const copy = {...common,...MEDIA_COPY[locale]};
  redraw = onRender;
  const root = element('div',{class:'media-workspace'});
  root.append(element('div',{class:'page-heading'},element('div',{},element('a',{class:'back-link',href:'#catalog',text:`← ${copy.back_catalog}`}),element('h1',{text:copy.heading}),element('p',{class:'lede',text:copy.intro})),element('div',{class:'page-heading-actions'},element('a',{class:'button',href:'#app/netflix',text:'Netflix'}),element('a',{class:'button',href:'#guide/amazon',text:copy.primeGuide}),button(copy.exportCombined,'export',{disabled:busy || !report?.overview.source_record_count}))));
  if (error) root.append(element('p',{class:'notice',role:'alert',text:`${copy.error_notice} · ${error}`}));
  root.append(element('p',{class:'sr-only',role:'status','aria-live':'polite',text:busy?copy.loading:copy.filters_applied}));
  const tabs = element('nav',{class:'workspace-tabs',role:'tablist','aria-label':copy.heading});
  const labels = {overview:copy.overview,titles:copy.titlesView,activity:copy.activityView,sources:copy.sourcesView};
  for (const name of VIEWS) tabs.append(element('button',{type:'button',role:'tab',id:`media-tab-${name}`,'aria-controls':`media-panel-${name}`,'aria-selected':name===view,'data-media-view':name,tabindex:name===view?'0':'-1',class:'button',text:labels[name]}));
  root.append(tabs,renderFilters(copy));
  const panel = element('section',{id:`media-panel-${view}`,'data-media-panel':view,role:'tabpanel','aria-labelledby':`media-tab-${view}`});
  if (!report) panel.append(element('p',{text:copy.loading}));
  else if (view==='overview') renderOverview(panel,copy,locale);
  else if (view==='titles') renderTitles(panel,copy);
  else if (view==='activity') renderActivity(panel,copy);
  else renderSources(panel,copy);
  root.append(panel,renderImports(copy,prime.tmdb_configured));
  root.addEventListener('click',event => {
    const target = /** @type {Element} */ (event.target);
    const tab = target.closest('[data-media-view]');
    if (tab) {
      view = tab.getAttribute('data-media-view');
      redraw();
      document.querySelector(`#media-tab-${view}`)?.['focus']();
      return;
    }
    const title = target.closest('[data-media-title]');
    if (title) {
      filter = {...filter,title_id:title.getAttribute('data-media-title'),title:''};
      view = 'activity'; cursor=''; cursors=[]; titlesCursor=''; titlesCursors=[];
      void refresh().catch(showError);
      return;
    }
    const action = target.closest('[data-media-action]');
    if (action && action.getAttribute('type')!=='submit') void perform(action.getAttribute('data-media-action'),root,copy,locale).catch(showError);
  });
  root.addEventListener('submit',event => {
    if (/** @type {Element} */(event.target).id !== 'media-filter-form') return;
    event.preventDefault();
    void perform('apply-filters',root,copy,locale).catch(showError);
  });
  return root;
}

function renderFilters(copy) {
  const form = element('form',{id:'media-filter-form',class:'panel media-filters'});
  const providers = element('select',{id:'media-provider',name:'provider'});
  for (const [id,name] of [['all',copy.allServices],['netflix','Netflix'],['prime-video','Prime Video']]) providers.append(element('option',{value:id,selected:filter.provider===id,text:name}));
  const kinds = element('select',{id:'media-kind',name:'kind'});
  for (const [id,name] of [['all',copy.allServices],['activity',copy.netflixUnit],['playback',copy.primeUnit],['watch_summary',copy.watchEvents],['search',copy.searches],['purchase',copy.purchases],['trailer',copy.trailers]]) kinds.append(element('option',{value:id,selected:filter.kind===id,text:name}));
  const statuses = element('select',{id:'media-match-status',name:'match_status'});
  for (const id of ['all','not_enriched','matched','review','unmatched']) statuses.append(element('option',{value:id,selected:filter.match_status===id,text:id==='all'?copy.all_statuses:statusLabel(id,copy)}));
  form.append(field(copy.provider,'media-provider',providers),field(copy.start_date,'media-start',textInput('media-start','start_date',filter.start_date,'date')),field(copy.end_date,'media-end',textInput('media-end','end_date',filter.end_date,'date')),field(copy.timezone,'media-timezone',textInput('media-timezone','timezone',filter.timezone)),field(copy.titleFilter,'media-title-filter',textInput('media-title-filter','title',filter.title)),field(copy.type,'media-kind',kinds),field(copy.match_status,'media-match-status',statuses),element('button',{type:'submit',class:'button button-primary','data-media-action':'apply-filters',disabled:busy,text:copy.applyFilters}),button(copy.clear_filters,'clear-filters'));
  return form;
}

function table(headers,rows,className='') {
  const result = element('table',{class:className},element('thead',{},element('tr',{},...headers.map(text=>element('th',{scope:'col',text})))),element('tbody',{},...rows.map(row=>element('tr',{},...row.map(value=>element('td',{},value))))));
  return element('div',{class:'table-scroll',tabindex:'0'},result);
}
function metric(text,value,key='') {
  return element('article',{class:'panel kpi-card'},element('p',{class:'kpi-label',text}),element('p',{class:'kpi-value','data-media-kpi':key,text:String(value)}));
}
function countPanel(text,values,copy,translate=false) {
  const maximum = Math.max(1,...values.map(item=>item.count));
  return element('figure',{class:'panel media-chart'},element('figcaption',{text}),element('p',{class:'panel-copy',text:`${copy.count}: ${values.reduce((sum,item)=>sum+item.count,0)}`}),table([copy.title,copy.count],values.map(item=>[translate?statusLabel(item.label,copy):item.label,element('span',{},element('meter',{min:0,max:maximum,value:item.count,'aria-hidden':'true'}),String(item.count))])));
}
function renderOverview(panel,copy,locale) {
  const overview = report.overview;
  if (!overview.source_record_count) panel.append(element('p',{class:'notice',text:copy.empty}));
  panel.append(element('div',{class:'kpi-grid'},metric(copy.activities,overview.activity_count,'activities'),metric(copy.unique_titles,overview.unique_title_count),metric(copy.watchTime,!overview.timed_records && overview.activity_count ? copy.unknown : new Intl.NumberFormat(locale,{maximumFractionDigits:0}).format(overview.recorded_seconds)),metric(copy.source_rows,overview.source_record_count)),element('p',{class:'panel-copy',text:copy.sourceNote}),element('p',{class:'panel-copy',text:`Prime Video: ${copy.watchNote}`}),element('p',{class:'panel-copy',text:copy.timeNote}),element('p',{class:'panel-copy',text:`${copy.recordedCoverage}: ${overview.timed_records} / ${overview.activity_count}. ${copy.unknownDuration}: ${overview.unknown_duration_records}.`}),table([copy.provider,copy.unit,copy.activities,copy.seconds],overview.services.map(service=>[providerName(service.provider),service.provider==='netflix'?copy.netflixUnit:copy.primeUnit,String(service.activities),service.timed_records?String(service.recorded_seconds):copy.unknown])));
  panel.append(element('div',{class:'kpi-grid'},metric(`${copy.unique_titles} · ${copy.matched}`,overview.accepted_title_count),metric(`${copy.unique_titles} · ${copy.review} / ${copy.unmatched} / ${copy.notEnriched}`,overview.unresolved_title_count),metric(copy.unknownTitle,overview.unavailable_title_records)));
  const charts = element('div',{class:'media-chart-grid'});
  charts.append(element('figure',{class:'panel media-chart'},element('figcaption',{text:copy.monthly_activity}),element('p',{class:'panel-copy',text:copy.sourceNote}),table([copy.date,copy.provider,copy.count],overview.months.map(month=>[month.month,providerName(month.provider),String(month.count)]))),element('figure',{class:'panel media-chart'},element('figcaption',{text:copy.top_titles}),table([copy.title,copy.provider,copy.count],overview.top_titles.map(title=>[element('button',{type:'button',class:'back-link','data-media-title':title.id,text:title.title}),title.providers.map(providerName).join(', '),String(title.activities)]))),countPanel(copy.match_coverage,overview.match_coverage,copy,true),countPanel(copy.exclusions,overview.exclusions,copy,true),countPanel(copy.genres,overview.genres,copy),countPanel(copy.devices,overview.devices,copy),countPanel(copy.audioLanguages,overview.audio_languages,copy),countPanel(copy.subtitleLanguages,overview.subtitle_languages,copy));
  const days = ['Sunday','Monday','Tuesday','Wednesday','Thursday','Friday','Saturday'];
  const localizedWeekdays = overview.weekdays.map(item=>({...item,label:new Intl.DateTimeFormat(locale,{weekday:'long',timeZone:'UTC'}).format(new Date(Date.UTC(2026,1,1+days.indexOf(item.label))))}));
  charts.append(countPanel(copy.weekdayActivity,localizedWeekdays,copy));
  panel.append(charts,element('div',{class:'kpi-grid'},metric(copy.rentals,overview.rentals),metric(copy.purchases,overview.purchases),metric(copy.playbackEvidence,overview.purchase_records_with_playback)),element('p',{class:'panel-copy',text:copy.runtimeNote}));
}
function renderTitles(panel,copy) {
  panel.append(element('div',{class:'kpi-grid'},metric(copy.movie,report.overview.movie_titles),metric(copy.series,report.overview.series_titles),metric(copy.episodes,report.overview.episode_count)));
  panel.append(element('p',{class:'panel-copy',text:copy.titleNote}),element('p',{class:'panel-copy',text:copy.episodeNote}),table([copy.title,copy.type,copy.provider,copy.activities,copy.match_status],report.titles.map(title=>[element('button',{type:'button',class:'back-link','data-media-title':title.id,text:title.title || copy.unknownTitle}),statusLabel(title.media_type,copy),title.providers.map(providerName).join(', '),String(title.activities),statusLabel(title.match_status,copy)]),'media-titles'));
  panel.append(element('div',{class:'media-pagination'},button(copy.previous,'previous-titles',{disabled:busy || !titlesCursors.length}),button(copy.next,'next-titles',{disabled:busy || !report.next_titles_cursor})));
}
function booleanLabel(value,copy) { return value===null?copy.unknown:value?copy.yes:copy.no; }
function renderActivity(panel,copy) {
  const rows = report.records.map(record => {
    const kind = {activity:copy.netflixUnit,playback:copy.primeUnit,watch_summary:copy.watchEvents,search:copy.searches,purchase:copy.purchases,trailer:copy.trailers}[record.kind];
    const details = element('details',{},
      element('summary',{text:copy.details}),
      table([copy.title,copy.outcome],[
        [copy.sourceFile,record.source.file],
        [copy.sourceRow,String(record.source.row)],
        [copy.title,record.raw_title],
        [copy.date,record.timestamp || record.date],
        [copy.interval,record.end_timestamp || copy.unknown],
        [copy.series,record.series_title || copy.unknown],
        [copy.seasons,record.season_number ? String(record.season_number) : copy.unknown],
        [copy.episodes,record.episode_title || copy.unknown],
        [copy.type,statusLabel(record.content_type,copy)],
        [copy.profileType,statusLabel(record.profile_type,copy)],
        [copy.autoplay,booleanLabel(record.autoplay,copy)],
        [copy.deleted,booleanLabel(record.deleted,copy)],
        [copy.completion,copy.unknown],
        [copy.runtime,record.metadata?.runtime_minutes===undefined?copy.unknown:String(record.metadata.runtime_minutes)],
        [copy.devices,record.device || copy.unknown],
        [copy.audioLanguages,record.audio_language || copy.unknown],
        [copy.subtitleLanguages,record.subtitle_language || copy.unknown]
      ])
    );
    return [record.date,providerName(record.provider),record.title || copy.unknownTitle,kind,record.recorded_seconds===null?copy.unknown:String(record.recorded_seconds),record.profile_label || copy.unknown,statusLabel(record.match_status,copy),details];
  });
  panel.append(
    element('p',{class:'panel-copy',text:copy.runtimeNote}),
    table([copy.date,copy.provider,copy.title,copy.type,copy.seconds,copy.profileLabel,copy.match_status,copy.details],rows,'media-records'),
    element('div',{class:'media-pagination'},button(copy.previous,'previous',{disabled:busy || !cursors.length}),button(copy.next,'next',{disabled:busy || !report.next_cursor}))
  );
}
function renderSources(panel,copy) {
  panel.append(element('h2',{text:copy.sourceCoverage}),element('p',{class:'panel-copy',text:copy.coverageNote}),element('p',{class:'panel-copy',text:copy.timeNote}),element('p',{class:'panel-copy',text:copy.profileNote}),element('p',{class:'panel-copy',text:`Prime Video: ${copy.watchNote}`}),table([copy.provider,copy.source_rows,copy.start_date,copy.end_date],report.sources.map(source=>[providerName(source.provider),String(source.records),source.start_date,source.end_date])));
  const preview = prime?.active_generation?.preview;
  if (preview) panel.append(element('h3',{text:'Prime Video'}),table([copy.datasets,copy.sourceFile,copy.source_rows,copy.date_range],preview.datasets.map(dataset=>[datasetName(dataset.id,copy),dataset.file,String(dataset.rows),`${dataset.start_date || ''} — ${dataset.end_date || ''}`])));
}
function datasetName(id,copy) {
  return {viewing:copy.primeUnit,playback_details:copy.playbackDetails,watch_events:copy.watchEvents,searches:copy.searches,purchases:copy.purchases,trailers:copy.trailers}[id];
}
function renderImports(copy,tmdbConfigured) {
  const root = element('section',{class:'media-imports','aria-label':copy.import});
  const netflixForm = element('section',{class:'panel'});
  netflixForm.append(element('h2',{text:copy.netflixImport}),element('p',{class:'panel-copy',text:copy.private_import_privacy}),field(copy.choose_csv,'media-netflix-file',element('input',{id:'media-netflix-file',type:'file',accept:'.csv,text/csv'})),field(copy.profileLabel,'media-netflix-label',textInput('media-netflix-label','profile_label','')),button(copy.import,'import-netflix',{class:'button button-primary'}));
  if (netflix?.active_generation) netflixForm.append(button(copy.enrich,'enrich-netflix',{disabled:busy || !tmdbConfigured || Boolean(netflix.building_generation)}));
  if (netflix?.building_generation) netflixForm.append(element('p',{role:'status',text:statusLabel(netflix.building_generation.state,copy)}));
  root.append(netflixForm);
  const primeForm = element('section',{class:'panel'});
  primeForm.append(element('h2',{text:copy.primeImport}),element('p',{class:'panel-copy',text:copy.privacy_footer}),field(copy.primeImport,'media-prime-file',element('input',{id:'media-prime-file',type:'file',accept:'.zip,application/zip'})),field(copy.profileLabel,'media-prime-label',textInput('media-prime-label','profile_label',prime?.active_generation?.profile_label || '')),button(copy.primePreview,'preview-prime',{class:'button button-primary',disabled:busy || Boolean(prime?.building_generation && prime.building_generation.state!=='failed')}));
  const pending = prime?.building_generation;
  if (pending) {
    primeForm.append(element('p',{role:'status',text:`${statusLabel(pending.state,copy)} · ${pending.completed_titles}/${pending.total_titles}`}));
    if (pending.state==='awaiting_confirmation') {
      const preview = element('section',{class:'media-preview'},element('h3',{text:copy.readyPreview}));
      for (const dataset of pending.preview.datasets) preview.append(element('label',{class:'media-dataset'},element('input',{type:'checkbox',name:'dataset',value:dataset.id,checked:['viewing','playback_details'].includes(dataset.id)}),`${datasetName(dataset.id,copy)} · ${dataset.rows} · ${dataset.start_date || ''} — ${dataset.end_date || ''}`));
      preview.append(element('p',{class:'panel-copy',text:`Prime Video: ${copy.watchNote}`}),element('p',{class:'panel-copy',text:copy.coverageNote}));
      if (pending.preview.unsupported_files.length) preview.append(element('p',{text:`${copy.unsupported}: ${pending.preview.unsupported_files.join(', ')}`}));
      preview.append(button(copy.confirmImport,'confirm-prime',{class:'button button-primary'}));
      primeForm.append(preview);
    }
    primeForm.append(button(copy.cancel,'cancel-prime',{disabled:false}));
    if (pending.state==='enriching') primeForm.append(button(copy.retry,'resume-prime',{disabled:busy || !tmdbConfigured}));
  }
  if (prime?.active_generation) primeForm.append(button(copy.enrich,'enrich-prime',{disabled:busy || !tmdbConfigured || Boolean(pending)}),button(copy.confirmDeletePrime,'delete-prime',{class:'button button-danger'}));
  root.append(primeForm);
  return root;
}

async function confirmIntent(title,body,confirmText,copy) {
  return new Promise(resolve=>{
    const trigger = document.activeElement;
    const dialog = element('dialog',{'aria-labelledby':'media-confirm-title'},element('h2',{id:'media-confirm-title',text:title}),element('p',{class:'panel-copy',text:body}));
    const accept = element('button',{type:'button',class:'button button-primary','data-media-confirm':'true',text:confirmText});
    const cancel = element('button',{type:'button',class:'button',text:copy.cancel});
    dialog.append(cancel,accept);
    const dismiss = () => complete(false);
    const complete = accepted => {
      pendingConfirmations.delete(dismiss);
      dialog.close();
      dialog.remove();
      if (trigger instanceof HTMLElement && trigger.isConnected) trigger.focus();
      resolve(accepted);
    };
    accept.addEventListener('click',()=>complete(true));
    cancel.addEventListener('click',()=>complete(false));
    dialog.addEventListener('cancel',event=>{event.preventDefault();complete(false);});
    pendingConfirmations.add(dismiss);
    document.body.append(dialog); dialog.showModal(); cancel.focus();
  });
}

async function perform(action,root,copy,locale) {
  if (busy && action!=='cancel-prime') return;
  if (action==='apply-filters' || action==='clear-filters') {
    const values = new FormData(root.querySelector('#media-filter-form'));
    filter = {provider:'all',timezone:'UTC',title:'',start_date:'',end_date:'',kind:'all',match_status:'all'};
    if (action==='apply-filters') {
      for (const key of Object.keys(filter)) {
        const value = values.get(key);
        if (typeof value!=='string') throw new Error('media filter form is incomplete');
        filter[key]=value;
      }
    }
    cursor=''; cursors=[]; titlesCursor=''; titlesCursors=[]; await refresh(); return;
  }
  if (action==='next' || action==='previous') {
    if (action==='next') { cursors.push(cursor); cursor=report.next_cursor; }
    else cursor=cursors.pop();
    await refresh(); return;
  }
  if (action==='next-titles' || action==='previous-titles') {
    if (action==='next-titles') { titlesCursors.push(titlesCursor); titlesCursor=report.next_titles_cursor; }
    else titlesCursor=titlesCursors.pop();
    await refresh(); return;
  }
  if (action.startsWith('enrich-') || action==='resume-prime') {
    if (!await confirmIntent(copy.enrich_title,`${copy.enrich_disclosure} ${copy.enrich_query_only}`,copy.confirm_enrich,copy)) return;
  }
  if (action==='delete-prime' && !await confirmIntent(copy.deletePrimeTitle,copy.deletePrimeBody,copy.confirmDeletePrime,copy)) return;
  const fileID = action==='import-netflix'?'media-netflix-file':'media-prime-file';
  const fileInput = /** @type {HTMLInputElement} */(root.querySelector(`#${fileID}`));
  const file = fileInput?.files?.[0];
  const label = /** @type {HTMLInputElement} */(root.querySelector(action==='import-netflix'?'#media-netflix-label':'#media-prime-label')).value;
  const selected = [...root.querySelectorAll('input[name="dataset"]:checked')].map(input=>/** @type {HTMLInputElement} */(input).value);
  if ((action==='import-netflix' || action==='preview-prime') && !file) { error='file_required'; redraw(); return; }
  busy=true; error=''; redraw();
  controller?.abort();
  const request = new AbortController(); controller=request;
  try {
    if (action==='import-netflix') {
      const generation = await createLocalGeneration(request.signal);
      await setNetflixProfileLabel(generation.id,label,request.signal);
      await uploadViewingActivity(generation.id,file,request.signal);
    } else if (action==='preview-prime') {
      const generation = await createPrimeGeneration(request.signal);
      await uploadPrimeArchive(generation.id,file,request.signal);
    } else if (action==='confirm-prime') await confirmPrimeSelection(prime.building_generation.id,selected,label,request.signal);
    else if (action==='cancel-prime') await cancelPrimeGeneration(prime.building_generation.id,request.signal);
    else if (action==='delete-prime') await deletePrimeProvider(request.signal);
    else if (action==='enrich-netflix') await createTMDBGeneration(netflix.active_generation.id,LOCALE_TO_TMDB[locale],request.signal);
    else if (action==='enrich-prime' || action==='resume-prime') await createPrimeGeneration(request.signal,{analysis_level:'tmdb',source_generation_id:action==='resume-prime'?prime.building_generation.source_generation_id:prime.active_generation.id,locale:action==='resume-prime'?prime.building_generation.locale:LOCALE_TO_TMDB[locale],tmdb_title_query_consent:'authorize-tmdb-title-queries'});
    else if (action==='export') {
      const blob = await exportMediaCSV(filter,request.signal);
      const url = URL.createObjectURL(blob); objectURLs.add(url);
      const link = element('a',{href:url,download:'viewing-history.csv'});
      document.body.append(link); link.click(); link.remove();
      window.setTimeout(()=>{URL.revokeObjectURL(url);objectURLs.delete(url);},1000);
    }
  } finally { if (controller===request) busy=false; }
  if (request.signal.aborted || !enabled) return;
  cursor=''; cursors=[]; titlesCursor=''; titlesCursors=[];
  await refresh();
}
