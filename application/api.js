// @ts-check

const PROVIDER_STATES = new Set([
  'empty',
  'building',
  'ready_local',
  'ready_tmdb',
  'action_needed',
  'deleting'
]);

const GENERATION_STATES = new Set([
  'receiving',
  'validating',
  'importing',
  'enriching',
  'ready',
  'failed'
]);

const ANALYSIS_LEVELS = new Set(['local', 'tmdb']);
const MATCH_STATUSES = new Set(['matched', 'review', 'unmatched']);
const OPENAI_STATES = new Set(['empty', 'index_required', 'ready']);
const OPENAI_SEARCH_MODES = new Set(['hybrid', 'semantic', 'lexical']);

let csrfToken = '';

function apiURL(path) {
  const configuredOrigin = document.documentElement.dataset.apiOrigin;
  if (!configuredOrigin) {
    throw new Error('data-api-origin is required');
  }
  const baseURL = new URL(configuredOrigin);
  if (
    !['http:', 'https:'].includes(baseURL.protocol) ||
    baseURL.username ||
    baseURL.password ||
    (baseURL.pathname !== '/' && baseURL.pathname !== '') ||
    baseURL.search ||
    baseURL.hash
  ) {
    throw new Error('data-api-origin must be an HTTP origin');
  }
  return new URL(path, baseURL).href;
}

export class APIError extends Error {
  constructor(status, payload) {
    const code = payload?.error?.code || `http_${status}`;
    super(code);
    this.name = 'APIError';
    this.status = status;
    this.code = code;
    this.generationID = payload?.error?.generation_id || '';
    this.row = payload?.error?.row || 0;
  }
}

export async function initializeAPI(signal) {
  const capabilities = await requestJSON('/api/capabilities', {signal});
  assertObject(capabilities, 'capabilities');
  assertString(capabilities.csrf_token, 'capabilities.csrf_token');
  if (!capabilities.csrf_token) {
    throw new Error('capabilities.csrf_token is required');
  }
  assertObject(capabilities.providers, 'capabilities.providers');
  assertObject(capabilities.providers.openai, 'capabilities.providers.openai');
  assertBoolean(
    capabilities.providers.openai.semantic_search,
    'capabilities.providers.openai.semantic_search'
  );
  assertBoolean(
    capabilities.providers.openai.browser_upload,
    'capabilities.providers.openai.browser_upload'
  );
  assertObject(capabilities.providers.netflix, 'capabilities.providers.netflix');
  assertObject(capabilities.providers.netflix.tmdb, 'capabilities.providers.netflix.tmdb');
  assertBoolean(
    capabilities.providers.netflix.tmdb.configured,
    'capabilities.providers.netflix.tmdb.configured'
  );
  csrfToken = capabilities.csrf_token;
  return capabilities;
}

export function resetAPI() {
  csrfToken = '';
}

/** @typedef {{file:string,row:number,generation_id:string}} MediaSource */
/** @typedef {{id:string,provider:string,kind:string,title:string,raw_title:string,title_status:string,date:string,date_precision:string,timestamp?:string,end_timestamp?:string,interval_status?:string,recorded_seconds:number|null,completion:string,content_type:string,profile_type:string,profile_label?:string,autoplay:boolean|null,deleted:boolean|null,device?:string,audio_language?:string,subtitle_language?:string,offer_type?:string,description?:string,source:MediaSource,search_title:string,title_identity:string,match_status:string,series_title?:string,season_number?:number,episode_title?:string,episode_identity?:string,metadata?:{tmdb_id:number,media_type:string,title:string,runtime_minutes?:number}}} MediaActivity */
/** @typedef {{label:string,count:number}} MediaCount */
/** @typedef {{id:string,title:string,media_type:string,match_status:string,activities:number,providers:string[]}} MediaTitle */
/** @typedef {{period:string,label:string,count:number}} MediaPeriodCount */
/** @typedef {{contract:string,filter:Record<string,string>,overview:{activity_count:number,source_record_count:number,unique_title_count:number,accepted_title_count:number,unresolved_title_count:number,movie_titles:number,series_titles:number,episode_count:number,unavailable_title_records:number,recorded_seconds:number,timed_records:number,unknown_duration_records:number,zero_duration_records:number,rentals:number,purchases:number,purchase_records_with_playback:number,services:{provider:string,unit:string,activities:number,recorded_seconds:number,timed_records:number}[],months:{month:string,provider:string,count:number}[],media_types:MediaCount[],monthly_media:MediaPeriodCount[],genres_by_weekday:MediaPeriodCount[],genres_by_year:MediaPeriodCount[],original_languages:MediaCount[],weekdays:MediaCount[],top_titles:MediaTitle[],genres:MediaCount[],match_coverage:MediaCount[],exclusions:MediaCount[],devices:MediaCount[],audio_languages:MediaCount[],subtitle_languages:MediaCount[]},titles:MediaTitle[],sources:{provider:string,generation_id:string,records:number,start_date:string,end_date:string}[],records:MediaActivity[],next_cursor:string,next_titles_cursor:string,revision:string}} MediaReport */
/** @typedef {{id:string,file:string,rows:number,start_date?:string,end_date?:string}} PrimeDataset */
/** @typedef {{id:string,state:string,analysis_level:string,datasets:string[],profile_label:string,record_count:number,completed_titles:number,total_titles:number,source_generation_id?:string,locale?:string,failure?:string,preview?:{datasets:PrimeDataset[],unsupported_files:string[],source_hash:string}}} PrimeGeneration */
/** @typedef {{active_generation:PrimeGeneration|null,building_generation:PrimeGeneration|null,max_upload_bytes:number,max_expanded_bytes:number,tmdb_configured:boolean}} PrimeSnapshot */

const PRIME_PATH = '/api/providers/prime-video';
const HISTORY_PATH = '/api/viewing-history';
const PRIME_STATES = new Set(['receiving','validating','awaiting_confirmation','importing','enriching','ready','failed']);
const MEDIA_PROVIDERS = new Set(['netflix','prime-video']);
const MEDIA_KINDS = new Set(['activity','playback','watch_summary','search','purchase','trailer']);
const PRIME_DATASETS = new Set(['viewing','playback_details','watch_events','searches','purchases','trailers']);
const MEDIA_MATCHES = new Set(['not_enriched',...MATCH_STATUSES]);
const MEDIA_CONTENT_TYPES = new Set(['content','promotion','trailer','live','unknown']);
const MEDIA_PROFILE_TYPES = new Set(['adult','child','unknown']);
const MEDIA_TITLE_TYPES = new Set(['movie','series','unknown']);
const MEDIA_WEEKDAYS = new Set(['Monday','Tuesday','Wednesday','Thursday','Friday','Saturday','Sunday']);
const MEDIA_TITLE_ID = /^(tmdb:(movie|series):[1-9][0-9]{0,18}|(netflix|prime-video):[a-f0-9]{64})$/;

/** @param {string} value @param {Set<string>} values @param {string} subject */
function requireMember(value, values, subject) {
  if (!values.has(value)) throw new Error(`invalid ${subject}`);
}

function assertCount(value, subject) {
  assertInteger(value,subject);
  if (value<0) throw new Error(`invalid ${subject}`);
}

function validateMediaMetadata(value) {
  assertObject(value,'media metadata');
  requireMember(value.media_type,new Set(['movie','series']),'media metadata type');
  assertCount(value.tmdb_id,'TMDB identity');
  if (!value.tmdb_id) throw new Error('invalid TMDB identity');
  assertString(value.title,'media metadata title');
  assertArray(value.genres,'media metadata genres');
  for (const genre of value.genres) assertString(genre,'media genre');
  if (value.runtime_minutes!==undefined) assertCount(value.runtime_minutes,'media runtime');
  for (const key of ['imdb_id','release_date','original_language']) if (value[key]!==undefined) assertString(value[key],`media metadata ${key}`);
  if (value.imdb_id!==undefined && !/^tt[0-9]{7,12}$/.test(value.imdb_id)) throw new Error('invalid IMDb identity');
}

function validateMediaTitle(title) {
  assertObject(title,'media title');
  assertString(title.id,'media title identity');
  if (!MEDIA_TITLE_ID.test(title.id)) throw new Error('invalid media title identity');
  assertString(title.title,'media title');
  requireMember(title.media_type,MEDIA_TITLE_TYPES,'media title type');
  requireMember(title.match_status,MEDIA_MATCHES,'media title match');
  assertArray(title.providers,'media title providers');
  if (title.providers.length<1 || title.providers.length>2 || new Set(title.providers).size!==title.providers.length) throw new Error('invalid media title providers');
  for (const provider of title.providers) requireMember(provider,MEDIA_PROVIDERS,'media title provider');
  assertCount(title.activities,'media title activities');
  if (title.metadata!==undefined) validateMediaMetadata(title.metadata);
}

/** @returns {Promise<PrimeSnapshot>} */
export async function getPrimeProvider(signal) {
  const payload = await requestJSON(PRIME_PATH, {signal});
  assertObject(payload, 'Prime snapshot');
  for (const key of ['active_generation','building_generation']) {
    if (payload[key] !== null) validatePrimeGeneration(payload[key]);
  }
  assertInteger(payload.max_upload_bytes, 'Prime upload limit');
  assertInteger(payload.max_expanded_bytes, 'Prime expanded limit');
  assertBoolean(payload.tmdb_configured,'Prime TMDB capability');
  return payload;
}

function validatePrimeGeneration(value) {
  assertObject(value, 'Prime generation');
  if (!/^pg_[a-f0-9]{32}$/.test(value.id) || !PRIME_STATES.has(value.state) || !ANALYSIS_LEVELS.has(value.analysis_level)) throw new Error('invalid Prime generation');
  for (const key of ['record_count','completed_titles','total_titles']) assertCount(value[key], `Prime ${key}`);
  if (value.completed_titles>value.total_titles) throw new Error('invalid Prime progress');
  assertString(value.profile_label, 'Prime label');
  assertArray(value.datasets, 'Prime selection');
  for (const dataset of value.datasets) requireMember(dataset,PRIME_DATASETS,'Prime dataset');
  for (const key of ['created_at','updated_at']) {
    assertString(value[key],`Prime ${key}`);
    if (!Number.isFinite(Date.parse(value[key]))) throw new Error('invalid Prime generation date');
  }
  assertArray(value.events,'Prime events');
  value.events.forEach((event,index)=>{
    assertObject(event,'Prime event');
    if (event.sequence!==index+1) throw new Error('invalid Prime event sequence');
    requireMember(event.state,PRIME_STATES,'Prime event state');
    assertString(event.at,'Prime event date');
  });
  if (value.analysis_level==='tmdb') {
    if (!/^pg_[a-f0-9]{32}$/.test(value.source_generation_id) || value.title_queries_authorized!==true) throw new Error('invalid Prime enrichment source');
    assertString(value.locale,'Prime enrichment locale');
  }
  if (value.preview) {
    assertObject(value.preview,'Prime preview');
    assertArray(value.preview.datasets,'Prime datasets');
    assertArray(value.preview.unsupported_files,'Prime unsupported files');
    assertString(value.preview.source_hash,'Prime source hash');
    for (const dataset of value.preview.datasets) {
      assertObject(dataset,'Prime dataset');
      assertString(dataset.id,'Prime dataset ID');
      requireMember(dataset.id,PRIME_DATASETS,'Prime preview dataset');
      assertString(dataset.file,'Prime dataset file');
      assertCount(dataset.rows,'Prime dataset rows');
      for (const key of ['start_date','end_date']) if (dataset[key]!==undefined) assertString(dataset[key],`Prime dataset ${key}`);
    }
    for (const name of value.preview.unsupported_files) assertString(name,'Prime unsupported file');
  }
  return value;
}

/** @returns {Promise<PrimeGeneration>} */
export async function createPrimeGeneration(signal, enrichment = null) {
  const payload = await mutateJSON(`${PRIME_PATH}/generations`,'POST', enrichment || {analysis_level:'local'},signal);
  return validatePrimeGeneration(payload.generation);
}

/** @returns {Promise<PrimeGeneration>} */
export async function uploadPrimeArchive(id, file, signal) {
  if (!/^pg_[a-f0-9]{32}$/.test(id) || !(file instanceof File)) throw new Error('invalid Prime upload');
  const payload = await requestJSON(`${PRIME_PATH}/generations/${id}/archive`,{method:'PUT',headers:mutationHeaders({'Content-Type':'application/zip'}),body:file,signal});
  return validatePrimeGeneration(payload.generation);
}

/** @returns {Promise<PrimeGeneration>} */
export async function confirmPrimeSelection(id,datasets,profileLabel,signal) {
  if (!/^pg_[a-f0-9]{32}$/.test(id)) throw new Error('invalid Prime generation ID');
  const payload = await mutateJSON(`${PRIME_PATH}/generations/${id}/selection`,'PUT',{datasets,profile_label:profileLabel},signal);
  return validatePrimeGeneration(payload.generation);
}

export async function cancelPrimeGeneration(id,signal) {
  if (!/^pg_[a-f0-9]{32}$/.test(id)) throw new Error('invalid Prime generation ID');
  await requestJSON(`${PRIME_PATH}/generations/${id}`,{method:'DELETE',headers:mutationHeaders(),signal});
}

export async function deletePrimeProvider(signal) {
  await mutateJSON(PRIME_PATH,'DELETE',{confirmation:'delete-prime-video-provider'},signal);
}

export async function setNetflixProfileLabel(id,label,signal) {
  requireGenerationID(id);
  const payload = await mutateJSON(`/api/providers/netflix/generations/${encodeURIComponent(id)}/profile-label`,'PUT',{label},signal);
  assertObject(payload,'profile label');
  assertString(payload.label,'profile label');
}

/** @returns {Promise<MediaReport>} */
export async function getMediaReport(filter,signal,cursor = '',titlesCursor = '') {
  const parameters = new URLSearchParams(filter);
  if (cursor) parameters.set('cursor',cursor);
  if (titlesCursor) parameters.set('titles_cursor',titlesCursor);
  const payload = await requestJSON(`${HISTORY_PATH}?${parameters}`,{signal});
  assertObject(payload,'media report');
  if (payload.contract !== 'viewing-history-report-v2') throw new Error('invalid media report contract');
  assertObject(payload.filter,'media filter');
  for (const key of ['provider','timezone','start_date','end_date','title','title_id','kind','match_status','media_type']) assertString(payload.filter[key],`media filter ${key}`);
  assertObject(payload.overview,'media overview');
  for (const key of ['activity_count','source_record_count','unique_title_count','accepted_title_count','unresolved_title_count','movie_titles','series_titles','episode_count','unavailable_title_records','timed_records','unknown_duration_records','zero_duration_records','rentals','purchases','purchase_records_with_playback']) assertCount(payload.overview[key],`media ${key}`);
  assertFiniteNumber(payload.overview.recorded_seconds,'media recorded seconds');
  for (const key of ['services','months','media_types','monthly_media','genres_by_weekday','genres_by_year','original_languages','weekdays','top_titles','genres','match_coverage','exclusions','devices','audio_languages','subtitle_languages']) assertArray(payload.overview[key],`media ${key}`);
  for (const service of payload.overview.services) {
    assertObject(service,'media service');
    requireMember(service.provider,MEDIA_PROVIDERS,'media service provider');
    assertString(service.unit,'media service unit');
    for (const key of ['activities','timed_records']) assertCount(service[key],`media service ${key}`);
    assertFiniteNumber(service.recorded_seconds,'media service seconds');
  }
  for (const month of payload.overview.months) {
    assertObject(month,'media month');
    requireMember(month.provider,MEDIA_PROVIDERS,'media month provider');
    assertString(month.month,'media month');
    assertCount(month.count,'media month count');
  }
  for (const key of ['media_types','original_languages','weekdays','genres','match_coverage','exclusions','devices','audio_languages','subtitle_languages']) {
    for (const count of payload.overview[key]) {
      assertObject(count,`media ${key} count`);
      assertString(count.label,`media ${key} label`);
      assertCount(count.count,`media ${key} count`);
    }
  }
  for (const key of ['monthly_media','genres_by_weekday','genres_by_year']) {
    for (const item of payload.overview[key]) {
      assertObject(item,`media ${key}`);
      assertString(item.period,`media ${key} period`);
      assertString(item.label,`media ${key} label`);
      assertCount(item.count,`media ${key} count`);
    }
  }
  for (const item of payload.overview.media_types) requireMember(item.label,MEDIA_TITLE_TYPES,'media content type');
  for (const item of payload.overview.monthly_media) {
    requireMember(item.label,MEDIA_TITLE_TYPES,'monthly content type');
    if (!/^\d{4}-(0[1-9]|1[0-2])$/.test(item.period)) throw new Error('invalid media month period');
  }
  for (const item of payload.overview.genres_by_weekday) requireMember(item.period,MEDIA_WEEKDAYS,'genre weekday');
  for (const item of payload.overview.genres_by_year) if (!/^\d{4}$/.test(item.period)) throw new Error('invalid genre year');
  for (const item of payload.overview.original_languages) if (!/^[a-z]{2,3}$/.test(item.label)) throw new Error('invalid original language');
  for (const title of payload.overview.top_titles) validateMediaTitle(title);
  for (const key of ['titles','sources','records']) assertArray(payload[key],`media ${key}`);
  assertString(payload.next_cursor,'media cursor');
  assertString(payload.next_titles_cursor,'media title cursor');
  assertString(payload.revision,'media revision');
  for (const source of payload.sources) {
    assertObject(source,'media source coverage');
    requireMember(source.provider,MEDIA_PROVIDERS,'media source provider');
    for (const key of ['generation_id','start_date','end_date']) assertString(source[key],`media source ${key}`);
    assertCount(source.records,'media source records');
  }
  for (const record of payload.records) {
    assertObject(record,'media record');
    if (!MEDIA_PROVIDERS.has(record.provider) || !MEDIA_KINDS.has(record.kind) || !['calendar_date','timestamp'].includes(record.date_precision) || !['not_enriched',...MATCH_STATUSES].includes(record.match_status)) throw new Error('invalid media record classification');
    for (const key of ['id','title','raw_title','date','completion','content_type','profile_type','search_title','title_identity']) assertString(record[key],`media record ${key}`);
    requireMember(record.content_type,MEDIA_CONTENT_TYPES,'media content type');
    requireMember(record.profile_type,MEDIA_PROFILE_TYPES,'media profile type');
    if (record.completion!=='unknown' || !['present','unavailable'].includes(record.title_status)) throw new Error('invalid media source state');
    for (const key of ['source_date','timestamp','end_timestamp','interval_status','profile_label','series_title','episode_title','episode_identity','device','audio_language','subtitle_language','offer_type','description']) if (record[key]!==undefined) assertString(record[key],`media record ${key}`);
    if (record.season_number!==undefined) assertCount(record.season_number,'media season');
    if (record.metadata!==undefined) validateMediaMetadata(record.metadata);
    if ((record.match_status==='matched')!==(record.metadata!==undefined)) throw new Error('invalid media match metadata');
    if (record.recorded_seconds!==null) assertFiniteNumber(record.recorded_seconds,'media record seconds');
    for (const key of ['autoplay','deleted']) if (record[key]!==null) assertBoolean(record[key],`media record ${key}`);
    assertObject(record.source,'media record source');
    assertString(record.source.file,'media source file');
    assertString(record.source.generation_id,'media source generation');
    assertInteger(record.source.row,'media source row');
  }
  for (const title of payload.titles) validateMediaTitle(title);
  return payload;
}

/** @returns {Promise<Blob>} */
export async function exportMediaCSV(filter,signal) {
  const sharedUI = Reflect.get(window,'MPRUI');
  const response = await sharedUI.authenticatedFetch(document.querySelector('#app-header'),apiURL(`${HISTORY_PATH}/export?${new URLSearchParams(filter)}`),{cache:'no-store',credentials:'include',signal});
  if (!response.ok) throw new APIError(response.status,await response.json());
  if (!response.headers.get('Content-Type')?.startsWith('text/csv')) throw new Error('invalid media export');
  return response.blob();
}

export async function getNetflixProvider(signal) {
  const snapshot = await requestJSON('/api/providers/netflix', {signal});
  return validateSnapshot(snapshot);
}

export async function getOpenAIProvider(signal) {
  const snapshot = await requestJSON('/api/providers/openai', {signal});
  return validateOpenAISnapshot(snapshot);
}

export async function searchOpenAI(search, signal) {
  assertObject(search, 'OpenAI search');
  const payload = await mutateJSON(
    '/api/providers/openai/search',
    'POST',
    {
      query: search.query,
      mode: search.mode,
      limit: search.limit,
      excerpts: search.excerpts,
      include_archived: search.includeArchived
    },
    signal
  );
  return validateOpenAISearchResponse(payload);
}

export async function createLocalGeneration(signal) {
  const payload = await mutateJSON(
    '/api/providers/netflix/generations',
    'POST',
    {analysis_level: 'local'},
    signal
  );
  return validateGenerationResponse(payload);
}

export async function uploadViewingActivity(generationID, file, signal) {
  requireGenerationID(generationID);
  if (!(file instanceof File)) {
    throw new Error('viewing activity upload requires a File');
  }
  const payload = await requestJSON(
    `/api/providers/netflix/generations/${encodeURIComponent(generationID)}/viewing-activity`,
    {
      method: 'PUT',
      headers: mutationHeaders({'Content-Type': 'text/csv; charset=utf-8'}),
      body: file,
      signal
    }
  );
  return validateGenerationResponse(payload);
}

export async function createTMDBGeneration(sourceGenerationID, locale, signal) {
  requireGenerationID(sourceGenerationID);
  const payload = await mutateJSON(
    '/api/providers/netflix/generations',
    'POST',
    {
      analysis_level: 'tmdb',
      source_generation_id: sourceGenerationID,
      locale
    },
    signal
  );
  return validateGenerationResponse(payload);
}

export async function cancelGeneration(generationID, signal) {
  requireGenerationID(generationID);
  await requestJSON(
    `/api/providers/netflix/generations/${encodeURIComponent(generationID)}`,
    {
      method: 'DELETE',
      headers: mutationHeaders(),
      signal
    }
  );
}

export async function deleteNetflixProvider(signal) {
  await mutateJSON(
    '/api/providers/netflix',
    'DELETE',
    {confirmation: 'delete-netflix-provider'},
    signal
  );
}

export async function getGenerationEvents(generationID, after, signal) {
  requireGenerationID(generationID);
  if (!Number.isSafeInteger(after) || after < 0) {
    throw new Error('event sequence must be a non-negative integer');
  }
  const events = await requestJSON(
    `/api/providers/netflix/generations/${encodeURIComponent(generationID)}/events?after=${after}`,
    {signal}
  );
  assertObject(events, 'events');
  assertString(events.generation_id, 'events.generation_id');
  assertArray(events.events, 'events.events');
  assertInteger(events.last_sequence, 'events.last_sequence');
  for (const event of events.events) {
    assertObject(event, 'events.events[]');
    assertInteger(event.sequence, 'event.sequence');
    if (!GENERATION_STATES.has(event.state)) {
      throw new Error(`event.state is invalid: ${String(event.state)}`);
    }
    assertInteger(event.progress_percent, 'event.progress_percent');
  }
  return events;
}

export async function getAnalytics(generationID, filter, signal) {
  requireGenerationID(generationID);
  const query = filterQuery(filter);
  const analytics = await requestJSON(
    `/api/providers/netflix/generations/${encodeURIComponent(generationID)}/analytics${query}`,
    {signal}
  );
  assertObject(analytics, 'analytics');
  assertString(analytics.generation_id, 'analytics.generation_id');
  validateFilter(analytics.filter);
  assertObject(analytics.data, 'analytics.data');
  for (const field of [
    'activity_count',
    'unique_title_count',
    'metadata_activity_count',
    'metadata_title_count'
  ]) {
    assertInteger(analytics.data[field], `analytics.data.${field}`);
  }
  for (const field of [
    'match_status_activities',
    'match_status_titles',
    'media_types',
    'genres',
    'viewing_years',
    'genres_by_viewing_year',
    'month_labels',
    'monthly_media',
    'languages',
    'origin_countries',
    'release_years',
    'rating_bands',
    'runtime_bands',
    'season_counts',
    'episode_bands',
    'weekday_labels',
    'genres_by_weekday',
    'top_titles'
  ]) {
    assertArray(analytics.data[field], `analytics.data.${field}`);
  }
  return analytics;
}

export async function getRecords(generationID, filter, cursor, limit, signal) {
  requireGenerationID(generationID);
  if (!Number.isSafeInteger(limit) || limit < 1) {
    throw new Error('record limit must be a positive integer');
  }
  const parameters = new URLSearchParams();
  parameters.set('limit', String(limit));
  appendFilter(parameters, filter);
  if (cursor) {
    parameters.set('cursor', cursor);
  }
  const page = await requestJSON(
    `/api/providers/netflix/generations/${encodeURIComponent(generationID)}/records?${parameters}`,
    {signal}
  );
  assertObject(page, 'record page');
  assertString(page.generation_id, 'record page.generation_id');
  validateFilter(page.filter);
  assertArray(page.records, 'record page.records');
  if (page.next_cursor !== undefined) {
    assertString(page.next_cursor, 'record page.next_cursor');
  }
  for (const record of page.records) {
    validateRecord(record);
  }
  return page;
}

export function exportGenerationURL(generationID) {
  requireGenerationID(generationID);
  return apiURL(
    `/api/providers/netflix/generations/${encodeURIComponent(generationID)}/export`
  );
}

async function mutateJSON(path, method, body, signal) {
  return requestJSON(path, {
    method,
    headers: mutationHeaders({'Content-Type': 'application/json; charset=utf-8'}),
    body: JSON.stringify(body),
    signal
  });
}

async function requestJSON(path, options = {}) {
  const sharedUI = Reflect.get(window, 'MPRUI');
  const response = await sharedUI.authenticatedFetch(
    document.querySelector('#app-header'),
    apiURL(path),
    {cache: 'no-store', credentials: 'include', ...options},
    {mutationReplay: 'authorization-before-domain-work'}
  );
  if (response.status === 204) {
    if (!response.ok) {
      throw new APIError(response.status, null);
    }
    return null;
  }
  let payload = null;
  try {
    payload = await response.json();
  } catch (error) {
    if (!response.ok) {
      throw new APIError(response.status, null);
    }
    throw new Error(`invalid JSON response from ${path}: ${error.message}`);
  }
  if (!response.ok) {
    throw new APIError(response.status, payload);
  }
  return payload;
}

function mutationHeaders(additional = {}) {
  if (!csrfToken) {
    throw new Error('API is not initialized');
  }
  return {
    'X-CSRF-Token': csrfToken,
    ...additional
  };
}

function validateGenerationResponse(payload) {
  assertObject(payload, 'generation response');
  return validateGeneration(payload.generation);
}

function validateSnapshot(snapshot) {
  assertObject(snapshot, 'Netflix snapshot');
  if (snapshot.provider !== 'netflix') {
    throw new Error('Netflix snapshot.provider must be netflix');
  }
  if (!PROVIDER_STATES.has(snapshot.state)) {
    throw new Error(`Netflix snapshot.state is invalid: ${String(snapshot.state)}`);
  }
  for (const key of [
    'active_generation',
    'building_generation',
    'latest_failed_generation'
  ]) {
    if (snapshot[key] !== undefined) {
      snapshot[key] = validateGeneration(snapshot[key]);
    }
  }
  assertObject(snapshot.capabilities, 'Netflix snapshot.capabilities');
  assertBoolean(snapshot.capabilities.local_import, 'capabilities.local_import');
  assertBoolean(snapshot.capabilities.tmdb_configured, 'capabilities.tmdb_configured');
  for (const key of [
    'max_upload_bytes',
    'max_rows',
    'max_unique_titles',
    'max_record_page_size'
  ]) {
    assertInteger(snapshot.capabilities[key], `capabilities.${key}`);
  }
  assertObject(snapshot.capabilities.tmdb_attribution, 'capabilities.tmdb_attribution');
  assertString(snapshot.capabilities.tmdb_attribution.name, 'TMDB attribution.name');
  assertString(snapshot.capabilities.tmdb_attribution.website, 'TMDB attribution.website');
  assertString(snapshot.capabilities.tmdb_attribution.notice, 'TMDB attribution.notice');
  return snapshot;
}

function validateOpenAISnapshot(snapshot) {
  assertObject(snapshot, 'OpenAI snapshot');
  if (snapshot.provider !== 'openai') {
    throw new Error('OpenAI snapshot.provider must be openai');
  }
  if (!OPENAI_STATES.has(snapshot.state)) {
    throw new Error(`OpenAI snapshot.state is invalid: ${String(snapshot.state)}`);
  }
  assertObject(snapshot.statistics, 'OpenAI snapshot.statistics');
  for (const key of ['imports', 'conversations', 'messages']) {
    assertInteger(snapshot.statistics[key], `OpenAI snapshot.statistics.${key}`);
  }
  assertObject(snapshot.capabilities, 'OpenAI snapshot.capabilities');
  assertBoolean(snapshot.capabilities.browser_upload, 'OpenAI capabilities.browser_upload');
  assertArray(snapshot.capabilities.search_modes, 'OpenAI capabilities.search_modes');
  if (
    snapshot.capabilities.search_modes.length !== OPENAI_SEARCH_MODES.size ||
    snapshot.capabilities.search_modes.some((mode) => !OPENAI_SEARCH_MODES.has(mode))
  ) {
    throw new Error('OpenAI capabilities.search_modes are invalid');
  }
  for (const key of ['max_query_bytes', 'max_results', 'max_excerpts']) {
    assertInteger(snapshot.capabilities[key], `OpenAI capabilities.${key}`);
    if (snapshot.capabilities[key] < 1) {
      throw new Error(`OpenAI capabilities.${key} must be positive`);
    }
  }
  assertString(
    snapshot.capabilities.inference_boundary,
    'OpenAI capabilities.inference_boundary'
  );
  if (snapshot.search_index !== undefined) {
    assertObject(snapshot.search_index, 'OpenAI snapshot.search_index');
    for (const key of [
      'id',
      'dimensions',
      'document_count',
      'eligible_document_count',
      'conversation_count',
      'eligible_conversation_count'
    ]) {
      assertInteger(snapshot.search_index[key], `OpenAI search_index.${key}`);
    }
    assertString(snapshot.search_index.name, 'OpenAI search_index.name');
    assertString(snapshot.search_index.model, 'OpenAI search_index.model');
  }
  if (
    (snapshot.state === 'ready' && snapshot.search_index === undefined) ||
    (snapshot.state !== 'ready' && snapshot.search_index !== undefined)
  ) {
    throw new Error('OpenAI snapshot state and search index disagree');
  }
  return snapshot;
}

function validateOpenAISearchResponse(payload) {
  assertObject(payload, 'OpenAI search response');
  assertArray(payload.results, 'OpenAI search response.results');
  assertBoolean(
    payload.query_embedding_cached,
    'OpenAI search response.query_embedding_cached'
  );
  payload.results.forEach((result, resultIndex) => {
    assertObject(result, `OpenAI search result ${resultIndex}`);
    assertString(result.conversation_id, `OpenAI search result ${resultIndex}.conversation_id`);
    assertString(
      result.conversation_title,
      `OpenAI search result ${resultIndex}.conversation_title`
    );
    assertFiniteNumber(result.score, `OpenAI search result ${resultIndex}.score`);
    assertFiniteNumber(
      result.semantic_score,
      `OpenAI search result ${resultIndex}.semantic_score`
    );
    assertFiniteNumber(
      result.lexical_score,
      `OpenAI search result ${resultIndex}.lexical_score`
    );
    assertArray(result.excerpts, `OpenAI search result ${resultIndex}.excerpts`);
    result.excerpts.forEach((excerpt, excerptIndex) => {
      assertObject(excerpt, `OpenAI result ${resultIndex} excerpt ${excerptIndex}`);
      for (const key of ['message_id', 'role', 'text']) {
        assertString(
          excerpt[key],
          `OpenAI result ${resultIndex} excerpt ${excerptIndex}.${key}`
        );
      }
      assertFiniteNumber(
        excerpt.semantic_score,
        `OpenAI result ${resultIndex} excerpt ${excerptIndex}.semantic_score`
      );
      assertFiniteNumber(
        excerpt.lexical_score,
        `OpenAI result ${resultIndex} excerpt ${excerptIndex}.lexical_score`
      );
      assertArray(
        excerpt.detection_methods,
        `OpenAI result ${resultIndex} excerpt ${excerptIndex}.detection_methods`
      );
      excerpt.detection_methods.forEach((method) => {
        assertString(method, 'OpenAI excerpt detection method');
      });
    });
  });
  return payload;
}

function validateGeneration(generation) {
  assertObject(generation, 'generation');
  requireGenerationID(generation.id);
  if (!ANALYSIS_LEVELS.has(generation.analysis_level)) {
    throw new Error(`generation.analysis_level is invalid: ${String(generation.analysis_level)}`);
  }
  if (!GENERATION_STATES.has(generation.state)) {
    throw new Error(`generation.state is invalid: ${String(generation.state)}`);
  }
  for (const key of [
    'activity_count',
    'unique_title_count',
    'completed_title_count',
    'matched_title_count',
    'review_title_count',
    'unmatched_title_count',
    'cache_hit_title_count',
    'progress_percent'
  ]) {
    assertInteger(generation[key], `generation.${key}`);
  }
  if (generation.failure !== undefined) {
    assertObject(generation.failure, 'generation.failure');
    assertString(generation.failure.code, 'generation.failure.code');
  }
  return generation;
}

function validateFilter(filter) {
  assertObject(filter, 'activity filter');
  if (filter.start_date !== undefined) {
    assertString(filter.start_date, 'filter.start_date');
  }
  if (filter.end_date !== undefined) {
    assertString(filter.end_date, 'filter.end_date');
  }
  if (
    filter.match_status !== undefined &&
    !MATCH_STATUSES.has(filter.match_status)
  ) {
    throw new Error(`filter.match_status is invalid: ${String(filter.match_status)}`);
  }
}

function validateRecord(record) {
  assertObject(record, 'record');
  assertInteger(record.index, 'record.index');
  for (const key of [
    'title',
    'date',
    'date_iso',
    'derived_title',
    'title_identity',
    'title_identity_version'
  ]) {
    assertString(record[key], `record.${key}`);
  }
  if (record.match !== undefined) {
    assertObject(record.match, 'record.match');
    if (!MATCH_STATUSES.has(record.match.status)) {
      throw new Error(`record.match.status is invalid: ${String(record.match.status)}`);
    }
    assertObject(record.match.evidence, 'record.match.evidence');
  }
  if (record.metadata !== undefined) {
    assertObject(record.metadata, 'record.metadata');
    assertString(record.metadata.media_type, 'record.metadata.media_type');
    assertArray(record.metadata.genres, 'record.metadata.genres');
    assertArray(record.metadata.origin_countries, 'record.metadata.origin_countries');
    if (record.match?.status !== 'matched') {
      throw new Error('accepted metadata requires a matched outcome');
    }
    if (record.metadata.imdb_id !== undefined) {
      assertString(record.metadata.imdb_id, 'record.metadata.imdb_id');
      if (record.metadata.imdb_id.length > 32 ||
          !/^tt[0-9]{7,}$/.test(record.metadata.imdb_id) ||
          record.metadata.imdb_id_source !== 'tmdb-external-ids') {
        throw new Error('invalid IMDb title identity');
      }
    } else if (record.metadata.imdb_id_source !== undefined) {
      throw new Error('IMDb source requires a title ID');
    }
  }
}

function filterQuery(filter) {
  const parameters = new URLSearchParams();
  appendFilter(parameters, filter);
  const encoded = parameters.toString();
  return encoded ? `?${encoded}` : '';
}

function appendFilter(parameters, filter) {
  if (!filter || typeof filter !== 'object') {
    throw new Error('activity filter is required');
  }
  if ((filter.startDate && !filter.endDate) || (!filter.startDate && filter.endDate)) {
    throw new Error('start and end dates are required together');
  }
  if (filter.startDate) {
    parameters.set('start_date', filter.startDate);
    parameters.set('end_date', filter.endDate);
  }
  if (filter.matchStatus) {
    if (!MATCH_STATUSES.has(filter.matchStatus)) {
      throw new Error('match-status filter is invalid');
    }
    parameters.set('match_status', filter.matchStatus);
  }
}

function requireGenerationID(value) {
  assertString(value, 'generation id');
  if (!/^ng_[a-f0-9]{32}$/.test(value)) {
    throw new Error('generation id does not match the current contract');
  }
}

function assertObject(value, path) {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${path} must be an object`);
  }
}

function assertArray(value, path) {
  if (!Array.isArray(value)) {
    throw new Error(`${path} must be an array`);
  }
}

function assertString(value, path) {
  if (typeof value !== 'string') {
    throw new Error(`${path} must be a string`);
  }
}

function assertBoolean(value, path) {
  if (typeof value !== 'boolean') {
    throw new Error(`${path} must be a boolean`);
  }
}

function assertInteger(value, path) {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new Error(`${path} must be a non-negative integer`);
  }
}

function assertFiniteNumber(value, path) {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error(`${path} must be a finite number`);
  }
}
