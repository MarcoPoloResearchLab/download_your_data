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
      locale,
      tmdb_title_query_consent: 'authorize-tmdb-title-queries'
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
  const response = await fetch(apiURL(path), {
    cache: 'no-store',
    credentials: 'include',
    ...options
  });
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
