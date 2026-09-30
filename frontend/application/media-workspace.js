// @ts-check

import {
  cancelGeneration,
  cancelPrimeGeneration,
  confirmPrimeSelection,
  createLocalGeneration,
  createPrimeGeneration,
  createTMDBGeneration,
  deleteNetflixProvider,
  deletePrimeProvider,
  exportMediaCSV,
  getMediaReport,
  getNetflixProvider,
  getPrimeProvider,
  uploadPrimeArchive,
  uploadViewingActivity
} from './api.js';
import {element} from './dom.js';
import {MEDIA_COPY} from './media-copy.js';
import {dashboardCharts, observeDashboardCharts} from './media-charts.js';

/** @type {import('./api.js').MediaReport|null} */
let report = null;
/** @type {import('./api.js').PrimeSnapshot|null} */
let prime = null;
let netflix = null;
let filter = newFilter();
let view = 'overview',
  busy = false,
  error = '',
  enabled = false,
  uploadOpen = false,
  manageOpen = false,
  moreOpen = false,
  aboutOpen = false;
let pollTimer = 0,
  filterTimer = 0;
/** @type {AbortController|null} */
let controller = null;
let redraw = () => {},
  disposeCharts = () => {};
let locale = 'en';
let cursor = '';
/** @type {string[]} */
let cursors = [];
const attempted = new Set();
const suspended = new Set();
const resumed = new Set();
const objectURLs = new Set();
const dialogs = new Set();
const LOCALE_TO_TITLE_LANGUAGE = {
  en: 'en-US',
  es: 'es-ES',
  fr: 'fr-FR',
  ru: 'ru-RU'
};
const VIEWS = ['overview', 'history'];
function newFilter() {
  return {
    provider: 'all',
    timezone: 'UTC',
    title: '',
    start_date: '',
    end_date: '',
    kind: 'all',
    match_status: 'all',
    media_type: 'all'
  };
}
function resetPagination() {
  cursor = '';
  cursors = [];
}
function providerName(provider) {
  return provider === 'netflix' ? 'Netflix' : 'Prime Video';
}
function updateProviders(providers) {
  if (
    prime?.active_generation?.id !== providers[0].active_generation?.id ||
    netflix?.active_generation?.id !== providers[1].active_generation?.id
  )
    resetPagination();
  [prime, netflix] = providers;
}
export function clearMediaWorkspace() {
  enabled = false;
  controller?.abort();
  controller = null;
  window.clearTimeout(pollTimer);
  window.clearTimeout(filterTimer);
  disposeCharts();
  for (const dismiss of dialogs) dismiss();
  for (const url of objectURLs) URL.revokeObjectURL(url);
  objectURLs.clear();
  report = null;
  prime = null;
  netflix = null;
  busy = false;
  error = '';
  view = 'overview';
  filter = newFilter();
  resetPagination();
  attempted.clear();
  suspended.clear();
  resumed.clear();
  uploadOpen = false;
  manageOpen = false;
  moreOpen = false;
  aboutOpen = false;
}
export async function hydrateMediaWorkspace(signal, currentLocale) {
  enabled = true;
  locale = currentLocale;
  const providers = await Promise.all([
    getPrimeProvider(signal),
    getNetflixProvider(signal)
  ]);
  if (signal.aborted || !enabled) return;
  updateProviders(providers);
  const currentReport = await getMediaReport(filter, signal);
  if (signal.aborted || !enabled) return;
  report = currentReport;
  try {
    await advanceAnalysis(signal);
  } catch (failure) {
    if (failure.name === 'AbortError') throw failure;
    error = 'analysis_failed';
  }
  schedulePoll();
}
function running() {
  return Boolean(
    (prime?.building_generation &&
      prime.building_generation.state !== 'failed') ||
      (netflix?.building_generation &&
        netflix.building_generation.state !== 'failed')
  );
}
function schedulePoll() {
  window.clearTimeout(pollTimer);
  if (enabled && running())
    pollTimer = window.setTimeout(() => {
      void refresh().catch(showError);
    }, 750);
}
async function advanceAnalysis(signal) {
  const pending = prime?.building_generation;
  if (pending?.state === 'awaiting_confirmation') {
    const datasets = pending.preview.datasets
      .filter((dataset) => ['viewing', 'playback_details'].includes(dataset.id))
      .map((dataset) => dataset.id);
    if (!datasets.includes('viewing'))
      throw new Error('viewing dataset is required');
    await confirmPrimeSelection(pending.id, datasets, '', signal);
    updateProviders(
      await Promise.all([getPrimeProvider(signal), getNetflixProvider(signal)])
    );
  } else if (pending?.state === 'enriching' && !resumed.has(pending.id)) {
    resumed.add(pending.id);
    await createPrimeGeneration(signal, {
      analysis_level: 'tmdb',
      source_generation_id: pending.source_generation_id,
      locale: pending.locale
    });
  }
  for (const provider of ['netflix', 'prime-video']) {
    const snapshot = provider === 'netflix' ? netflix : prime;
    const active = snapshot?.active_generation;
    const failed =
      provider === 'netflix'
        ? snapshot?.latest_failed_generation
        : snapshot?.building_generation;
    if (
      !active ||
      active.analysis_level !== 'local' ||
      snapshot.building_generation ||
      suspended.has(provider) ||
      attempted.has(active.id)
    )
      continue;
    if (
      failed?.state === 'failed' &&
      failed.source_generation_id === active.id
    ) {
      error = 'analysis_failed';
      continue;
    }
    attempted.add(active.id);
    const configured =
      provider === 'netflix'
        ? snapshot.capabilities.tmdb_configured
        : snapshot.tmdb_configured;
    if (!configured) {
      error = 'analysis_failed';
      continue;
    }
    try {
      if (provider === 'netflix')
        await createTMDBGeneration(
          active.id,
          LOCALE_TO_TITLE_LANGUAGE[locale],
          signal
        );
      else {
        const generation = await createPrimeGeneration(signal, {
          analysis_level: 'tmdb',
          source_generation_id: active.id,
          locale: LOCALE_TO_TITLE_LANGUAGE[locale]
        });
        resumed.add(generation.id);
      }
    } catch (failure) {
      if (failure.name === 'AbortError') throw failure;
      error = 'analysis_failed';
    }
  }
  updateProviders(
    await Promise.all([getPrimeProvider(signal), getNetflixProvider(signal)])
  );
}
async function refresh() {
  if (!enabled || busy) {
    schedulePoll();
    return;
  }
  controller?.abort();
  const request = new AbortController();
  controller = request;
  const providers = await Promise.all([
    getPrimeProvider(request.signal),
    getNetflixProvider(request.signal)
  ]);
  if (request.signal.aborted || !enabled) return;
  updateProviders(providers);
  try {
    await advanceAnalysis(request.signal);
  } catch (failure) {
    if (failure.name === 'AbortError') throw failure;
    error = 'analysis_failed';
  }
  if (request.signal.aborted || !enabled) return;
  let currentReport;
  try {
    currentReport = await getMediaReport(filter, request.signal, cursor);
  } catch (failure) {
    if (failure.code !== 'stale_cursor' || !cursor) throw failure;
    resetPagination();
    currentReport = await getMediaReport(filter, request.signal);
  }
  if (request.signal.aborted || !enabled) return;
  report = currentReport;
  if (error === 'request_failed') error = '';
  redraw();
  schedulePoll();
}
function showError(failure) {
  if (!enabled || failure.name === 'AbortError') return;
  error = 'request_failed';
  redraw();
  schedulePoll();
}
function button(text, action, attributes = {}) {
  return element(
    'button',
    {
      type: 'button',
      class: 'button',
      'data-media-action': action,
      disabled: busy,
      ...attributes
    },
    text
  );
}
function field(text, id, node) {
  return element(
    'label',
    {class: 'field', for: id},
    element('span', {text}),
    node
  );
}
function input(id, name, value, type = 'text') {
  return element('input', {id, name, type, value});
}
function table(headers, rows, className = '') {
  return element(
    'div',
    {class: 'table-scroll', tabindex: '0'},
    element(
      'table',
      {class: className},
      element(
        'thead',
        {},
        element(
          'tr',
          {},
          ...headers.map((text) => element('th', {scope: 'col', text}))
        )
      ),
      element(
        'tbody',
        {},
        ...rows.map((row) =>
          element('tr', {}, ...row.map((value) => element('td', {}, value)))
        )
      )
    )
  );
}
function duration(seconds) {
  const minutes = Math.floor(seconds / 60);
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}
function number(value) {
  return new Intl.NumberFormat(locale).format(value);
}
function metric(text, value, key, note = '') {
  return element(
    'article',
    {},
    element('p', {class: 'kpi-label', text}),
    element('p', {
      class: 'kpi-value',
      'data-media-kpi': key,
      text: String(value)
    }),
    note ? element('small', {text: note}) : null
  );
}
export function renderMediaWorkspace(common, currentLocale, onRender) {
  locale = currentLocale;
  const copy = {...common, ...MEDIA_COPY[locale]};
  disposeCharts();
  redraw = () => {
    const active = document.activeElement;
    const restore =
      active instanceof HTMLInputElement &&
      Boolean(active.closest('.media-workspace'));
    const selection = restore
      ? [active.selectionStart, active.selectionEnd]
      : null;
    onRender();
    if (restore) {
      const replacement = /** @type {HTMLInputElement|null} */ (
        document.getElementById(active.id)
      );
      replacement?.focus({preventScroll: true});
      if (replacement && selection[0] !== null && selection[1] !== null)
        replacement.setSelectionRange(selection[0], selection[1]);
    }
  };
  const root = element('div', {class: 'media-workspace'});
  root.append(
    element(
      'div',
      {class: 'page-heading'},
      element(
        'div',
        {},
        element('a', {
          class: 'back-link',
          href: '#catalog',
          text: `← ${copy.back_catalog}`
        }),
        element('h1', {text: copy.heading})
      ),
      element(
        'div',
        {class: 'page-heading-actions'},
        button(copy.addFiles, 'add-files', {
          class: 'button button-primary',
          disabled: false
        }),
        button(copy.exportCombined, 'export', {
          disabled: busy || !report?.overview.source_record_count
        })
      )
    )
  );
  if (error) {
    const text =
      error === 'analysis_failed'
        ? copy.analysisFailed
        : error === 'invalid_files'
          ? copy.invalidFiles
          : error === 'request_failed'
            ? copy.error_notice
            : copy.importFailed;
    root.append(element('p', {class: 'notice', role: 'alert', text}));
    if (error === 'analysis_failed')
      root.append(button(copy.retry, 'retry-analysis'));
  }
  const failed =
    prime?.building_generation?.state === 'failed' ||
    Boolean(
      netflix?.latest_failed_generation &&
        netflix.latest_failed_generation.source_generation_id ===
          netflix?.active_generation?.id
    );
  if (failed && !error)
    root.append(
      element('p', {
        class: 'notice',
        role: 'alert',
        text: copy.analysisFailed
      }),
      button(copy.retry, 'retry-analysis')
    );
  if (uploadOpen || !report?.sources.length) root.append(renderUpload(copy));
  if (busy || running()) {
    const progress = element(
      'section',
      {
        class: 'panel media-processing',
        role: 'status',
        'aria-live': 'polite'
      },
      element('strong', {text: copy.analyzing}),
      element('progress', {'aria-label': copy.analyzing})
    );
    for (const [provider, snapshot] of [
      ['netflix', netflix],
      ['prime-video', prime]
    ])
      if (snapshot?.building_generation)
        progress.append(
          button(
            `${copy.cancel} · ${providerName(provider)}`,
            `cancel-${provider}`,
            {disabled: false}
          )
        );
    root.append(progress);
  }
  if (report?.sources.length) {
    root.append(
      element(
        'div',
        {class: 'media-import-status'},
        element('span', {
          text: report.sources
            .map((source) => providerName(source.provider))
            .join(' + ')
        }),
        button(copy.manageFiles, 'manage-files', {
          class: 'button button-quiet'
        })
      )
    );
    if (manageOpen) root.append(renderManagement(copy));
    root.append(renderFilters(copy));
    const tabs = element('nav', {
      class: 'workspace-tabs',
      role: 'tablist',
      'aria-label': copy.heading
    });
    for (const name of VIEWS)
      tabs.append(
        element('button', {
          type: 'button',
          role: 'tab',
          id: `media-tab-${name}`,
          'aria-controls': `media-panel-${name}`,
          'aria-selected': name === view,
          'data-media-view': name,
          tabindex: name === view ? '0' : '-1',
          class: 'button',
          text: name === 'overview' ? copy.overview : copy.history
        })
      );
    root.append(tabs);
    const panel = element('section', {
      id: `media-panel-${view}`,
      'data-media-panel': view,
      role: 'tabpanel',
      'aria-labelledby': `media-tab-${view}`
    });
    if (view === 'overview') {
      const overview = report.overview;
      const metrics = element(
        'div',
        {class: 'media-metrics', 'aria-live': 'polite'},
        metric(copy.activities, number(overview.activity_count), 'activities'),
        metric(
          copy.unique_titles,
          number(overview.unique_title_count),
          'titles'
        )
      );
      if (
        filter.provider !== 'netflix' &&
        report.sources.some((source) => source.provider === 'prime-video')
      )
        metrics.append(
          metric(
            copy.watchTime,
            duration(overview.recorded_seconds),
            'watch-time',
            'Prime Video'
          )
        );
      panel.append(metrics, dashboardCharts(overview, copy, locale));
    } else renderHistory(panel, copy);
    root.append(panel, renderAbout(copy));
  }
  root.addEventListener('click', (event) => {
    const target = /** @type {Element} */ (event.target);
    const tab = target.closest('[data-media-view]');
    if (tab) {
      view = tab.getAttribute('data-media-view');
      redraw();
      document.getElementById(`media-tab-${view}`)?.focus();
      return;
    }
    const provider = target.closest('[data-media-provider]');
    if (provider) {
      filter = {
        ...filter,
        provider: provider.getAttribute('data-media-provider')
      };
      resetPagination();
      void refresh().catch(showError);
      return;
    }
    const action = target.closest('[data-media-action]');
    if (action)
      void perform(action.getAttribute('data-media-action'), copy).catch(
        showError
      );
  });
  root.addEventListener('input', (event) => {
    const target = /** @type {HTMLInputElement} */ (event.target);
    if (!target.matches('[data-media-filter]')) return;
    filter = {...filter, [target.name]: target.value};
    delete filter['title_id'];
    resetPagination();
    window.clearTimeout(filterTimer);
    filterTimer = window.setTimeout(() => {
      void refresh().catch(showError);
    }, 250);
  });
  root.addEventListener('change', (event) => {
    const target = /** @type {HTMLInputElement} */ (event.target);
    if (target.id === 'media-files' && target.files?.length) {
      const files = Array.from(target.files);
      void uploadFiles(files).catch(showError);
    } else if (target.matches('select[data-media-filter]')) {
      filter = {...filter, [target.name]: target.value};
      resetPagination();
      void refresh().catch(showError);
    }
  });
  root.querySelector('.media-more')?.addEventListener('toggle', (event) => {
    moreOpen = /** @type {HTMLDetailsElement} */ (event.target).open;
  });
  root.querySelector('.media-about')?.addEventListener('toggle', (event) => {
    aboutOpen = /** @type {HTMLDetailsElement} */ (event.target).open;
  });
  queueMicrotask(() => {
    if (root.isConnected && enabled)
      disposeCharts = observeDashboardCharts(root);
  });
  return root;
}
function renderUpload(copy) {
  const files = element('input', {
    id: 'media-files',
    type: 'file',
    accept: '.csv,.zip,text/csv,application/zip',
    multiple: true,
    disabled: busy
  });
  return element(
    'section',
    {class: 'panel media-upload', 'aria-label': copy.uploadTitle},
    element(
      'div',
      {class: 'media-upload-heading'},
      element('h2', {text: copy.uploadTitle}),
      report?.sources.length
        ? button(copy.close, 'close-upload', {class: 'button button-quiet'})
        : null
    ),
    field(copy.uploadHint, 'media-files', files),
    element(
      'div',
      {class: 'media-guides'},
      element('a', {href: '#guide/netflix', text: copy.netflixGuide}),
      element('a', {href: '#guide/amazon', text: copy.primeGuide})
    )
  );
}
function renderFilters(copy) {
  const root = element('section', {
    class: 'media-filters',
    'aria-label': copy.filters
  });
  const services = element('div', {
    class: 'media-services',
    'aria-label': copy.provider
  });
  for (const [id, name] of [
    ['all', copy.allServices],
    ['netflix', 'Netflix'],
    ['prime-video', 'Prime Video']
  ])
    services.append(
      element('button', {
        type: 'button',
        class: 'button',
        'data-media-provider': id,
        'aria-pressed': filter.provider === id,
        text: name
      })
    );
  const title = input('media-title-filter', 'title', filter.title, 'search');
  title.setAttribute('data-media-filter', '');
  title.setAttribute('placeholder', copy.titleFilter);
  const dates = element(
    'details',
    {class: 'media-more', open: moreOpen ? true : undefined},
    element('summary', {text: copy.moreFilters})
  );
  const type = element('select', {
    id: 'media-type',
    name: 'media_type',
    'data-media-filter': ''
  });
  for (const [id, name] of [
    ['all', copy.allTitles],
    ['movie', copy.films],
    ['series', copy.series],
    ['unknown', copy.unknown]
  ])
    type.append(
      element('option', {
        value: id,
        selected: filter.media_type === id,
        text: name
      })
    );
  const start = input('media-start', 'start_date', filter.start_date, 'date'),
    end = input('media-end', 'end_date', filter.end_date, 'date');
  start.setAttribute('data-media-filter', '');
  end.setAttribute('data-media-filter', '');
  const timezone = input('media-timezone', 'timezone', filter.timezone);
  timezone.setAttribute('data-media-filter', '');
  dates.append(
    element(
      'div',
      {class: 'media-advanced'},
      field(copy.start_date, 'media-start', start),
      field(copy.end_date, 'media-end', end),
      field(copy.type, 'media-type', type),
      field(copy.timezone, 'media-timezone', timezone)
    )
  );
  root.append(
    services,
    field(copy.titleFilter, 'media-title-filter', title),
    dates
  );
  if (
    filter.title ||
    filter.start_date ||
    filter.end_date ||
    filter.media_type !== 'all' ||
    filter.timezone !== 'UTC'
  )
    root.append(
      button(copy.clear_filters, 'clear-filters', {
        class: 'button button-quiet'
      })
    );
  return root;
}
function renderHistory(panel, copy) {
  panel.append(
    table(
      [copy.date, copy.provider, copy.title, copy.watchTime, copy.details],
      report.records.map((record) => [
        record.date,
        providerName(record.provider),
        record.metadata?.title || record.title || copy.unknownTitle,
        record.recorded_seconds === null
          ? copy.unknown
          : duration(record.recorded_seconds),
        element(
          'details',
          {},
          element('summary', {text: copy.details}),
          table(
            [copy.title, copy.outcome],
            [
              [copy.sourceFile, record.source.file],
              [copy.sourceRow, String(record.source.row)],
              [copy.title, record.raw_title],
              [copy.devices, record.device || copy.unknown],
              [copy.audioLanguages, record.audio_language || copy.unknown],
              [copy.subtitleLanguages, record.subtitle_language || copy.unknown]
            ]
          )
        )
      ]),
      'media-records'
    ),
    element(
      'div',
      {class: 'media-pagination'},
      button(copy.previous, 'previous', {disabled: busy || !cursors.length}),
      button(copy.next, 'next', {disabled: busy || !report.next_cursor})
    )
  );
}
function renderManagement(copy) {
  const root = element('section', {
    class: 'panel media-management',
    'aria-label': copy.manageFiles
  });
  for (const [provider, snapshot] of [
    ['netflix', netflix],
    ['prime-video', prime]
  ])
    if (snapshot?.active_generation)
      root.append(
        element(
          'div',
          {class: 'media-file'},
          element(
            'span',
            {},
            element('strong', {text: providerName(provider)}),
            element('small', {
              text: snapshot.active_generation.profile_label || ''
            })
          ),
          button(copy.replaceFile, 'add-files'),
          button(copy.deleteData, `delete-${provider}`, {
            class: 'button button-danger'
          })
        )
      );
  return root;
}
function renderAbout(copy) {
  const details = element(
    'details',
    {class: 'media-about', open: aboutOpen ? true : undefined},
    element('summary', {text: copy.aboutReport}),
    element('p', {text: copy.sourceNote}),
    element('p', {text: copy.watchNote}),
    element('p', {text: copy.timeNote}),
    table(
      [copy.provider, copy.source_rows, copy.start_date, copy.end_date],
      report.sources.map((source) => [
        providerName(source.provider),
        String(source.records),
        source.start_date,
        source.end_date
      ])
    ),
    table(
      [copy.exclusions, copy.count],
      report.overview.exclusions.map((item) => [
        copy[item.label] || copy.unknown,
        String(item.count)
      ])
    )
  );
  return details;
}
async function confirmDeletion(copy, provider) {
  return new Promise((resolve) => {
    const trigger = document.activeElement;
    const dialog = element(
      'dialog',
      {'aria-label': copy.deleteData},
      element('h2', {
        text: `${copy.deleteData} · ${providerName(provider)}?`
      }),
      element('p', {text: copy.deleteBody})
    );
    const cancel = button(copy.cancel, 'unused', {disabled: false}),
      accept = button(copy.deleteData, 'unused', {
        class: 'button button-danger',
        disabled: false,
        'data-media-confirm': 'true'
      });
    dialog.append(cancel, accept);
    const complete = (accepted) => {
      dialogs.delete(dismiss);
      dialog.close();
      dialog.remove();
      if (trigger instanceof HTMLElement && trigger.isConnected)
        trigger.focus();
      resolve(accepted);
    };
    const dismiss = () => complete(false);
    dialogs.add(dismiss);
    cancel.addEventListener('click', dismiss);
    accept.addEventListener('click', () => complete(true));
    dialog.addEventListener('cancel', (event) => {
      event.preventDefault();
      dismiss();
    });
    document.body.append(dialog);
    dialog.showModal();
    cancel.focus();
  });
}
async function uploadFiles(files) {
  if (busy) return;
  const providers = files.map((file) =>
    file.name.toLowerCase().endsWith('.csv')
      ? 'netflix'
      : file.name.toLowerCase().endsWith('.zip')
        ? 'prime-video'
        : 'invalid'
  );
  if (
    providers.includes('invalid') ||
    new Set(providers).size !== providers.length
  ) {
    error = 'invalid_files';
    redraw();
    return;
  }
  busy = true;
  error = '';
  uploadOpen = false;
  controller?.abort();
  const request = new AbortController();
  controller = request;
  redraw();
  try {
    for (let index = 0; index < files.length; index++) {
      const provider = providers[index];
      suspended.delete(provider);
      if (provider === 'netflix') {
        const generation = await createLocalGeneration(request.signal);
        if (request.signal.aborted || !enabled) return;
        netflix = {...netflix, building_generation: generation};
        redraw();
        await uploadViewingActivity(
          generation.id,
          files[index],
          request.signal
        );
      } else {
        const generation = await createPrimeGeneration(request.signal);
        if (request.signal.aborted || !enabled) return;
        prime = {...prime, building_generation: generation};
        redraw();
        await uploadPrimeArchive(generation.id, files[index], request.signal);
      }
    }
  } catch (failure) {
    if (failure.name === 'AbortError') return;
    error = 'import_failed';
  } finally {
    if (controller === request) busy = false;
  }
  if (!request.signal.aborted && enabled) await refresh();
}
async function perform(action, copy) {
  if (action === 'add-files') {
    uploadOpen = true;
    redraw();
    document.getElementById('media-files')?.focus();
    return;
  }
  if (action === 'close-upload') {
    uploadOpen = false;
    redraw();
    return;
  }
  if (action === 'manage-files') {
    manageOpen = !manageOpen;
    redraw();
    return;
  }
  if (action === 'clear-filters') {
    window.clearTimeout(filterTimer);
    filter = newFilter();
    resetPagination();
    await refresh();
    return;
  }
  if (action === 'next' || action === 'previous') {
    if (action === 'next') {
      cursors.push(cursor);
      cursor = report.next_cursor;
    } else cursor = cursors.pop();
    await refresh();
    return;
  }
  if (busy && !action.startsWith('cancel-')) return;
  const provider = action.endsWith('netflix') ? 'netflix' : 'prime-video';
  if (action.startsWith('delete-') && !(await confirmDeletion(copy, provider)))
    return;
  controller?.abort();
  const request = new AbortController();
  controller = request;
  busy = true;
  error = '';
  redraw();
  try {
    if (action.startsWith('cancel-')) {
      suspended.add(provider);
      const id = (provider === 'netflix' ? netflix : prime).building_generation
        .id;
      if (provider === 'netflix') await cancelGeneration(id, request.signal);
      else await cancelPrimeGeneration(id, request.signal);
    } else if (action.startsWith('delete-')) {
      suspended.add(provider);
      if (provider === 'netflix') await deleteNetflixProvider(request.signal);
      else await deletePrimeProvider(request.signal);
    } else if (action === 'retry-analysis') {
      suspended.clear();
      attempted.clear();
      resumed.clear();
      if (prime?.building_generation?.state === 'failed')
        await cancelPrimeGeneration(
          prime.building_generation.id,
          request.signal
        );
      for (const snapshot of [netflix, prime])
        if (snapshot?.active_generation)
          attempted.delete(snapshot.active_generation.id);
      if (
        netflix?.active_generation?.analysis_level === 'local' &&
        !netflix?.building_generation
      )
        netflix = {...netflix, latest_failed_generation: null};
      updateProviders(
        await Promise.all([
          getPrimeProvider(request.signal),
          getNetflixProvider(request.signal)
        ])
      );
      if (
        netflix?.active_generation?.analysis_level === 'local' &&
        !netflix?.building_generation &&
        netflix.capabilities.tmdb_configured
      ) {
        attempted.add(netflix.active_generation.id);
        await createTMDBGeneration(
          netflix.active_generation.id,
          LOCALE_TO_TITLE_LANGUAGE[locale],
          request.signal
        );
      }
      await advanceAnalysis(request.signal);
    } else if (action === 'export') {
      const blob = await exportMediaCSV(filter, request.signal);
      const url = URL.createObjectURL(blob);
      objectURLs.add(url);
      const link = element('a', {href: url, download: 'viewing-history.csv'});
      document.body.append(link);
      link.click();
      link.remove();
      window.setTimeout(() => {
        URL.revokeObjectURL(url);
        objectURLs.delete(url);
      }, 1000);
    }
  } catch (failure) {
    if (failure.name === 'AbortError') return;
    error = 'analysis_failed';
  } finally {
    if (controller === request) busy = false;
  }
  if (!request.signal.aborted && enabled) {
    resetPagination();
    await refresh();
  }
}
