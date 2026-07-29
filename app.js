// @ts-check

import {
  APIError,
  cancelGeneration,
  createLocalGeneration,
  createTMDBGeneration,
  deleteNetflixProvider,
  exportGenerationURL,
  getAnalytics,
  getGenerationEvents,
  getNetflixProvider,
  getRecords,
  initializeAPI,
  uploadViewingActivity
} from './api.js';
import {countChart, seriesChart} from './charts.js';

const STORAGE_KEYS = Object.freeze({
  language: 'download-your-data.language',
  theme: 'download-your-data.theme'
});

const LOCALES = Object.freeze(['en', 'es', 'fr', 'ru']);
const GUIDE_SCREENSHOT_PROVIDERS = Object.freeze([
  'openai',
  'facebook',
  'instagram',
  'linkedin',
  'tiktok',
  'x',
  'youtube',
  'google'
]);
const VIEWS = Object.freeze(['overview', 'catalog', 'match_quality']);
const MATCH_STATUSES = Object.freeze(['matched', 'review', 'unmatched']);
const LOCALE_TO_TMDB = Object.freeze({
  en: 'en-US',
  es: 'es-ES',
  fr: 'fr-FR',
  ru: 'ru-RU'
});
const REQUIRED_UI_KEYS = Object.freeze([
  'catalog_title',
  'catalog_intro',
  'provider_catalog',
  'workspace',
  'guide',
  'open',
  'view_guide',
  'local_only',
  'privacy_footer',
  'skip_to_content',
  'language',
  'credits',
  'theme_light',
  'theme_dark',
  'back_catalog',
  'state_empty',
  'state_receiving',
  'state_validating',
  'state_importing',
  'state_enriching',
  'state_ready_local',
  'state_ready_tmdb',
  'state_action_needed',
  'state_failed',
  'state_deleting',
  'state_canceled',
  'state_replacement',
  'state_not_configured',
  'overview',
  'catalog',
  'match_quality',
  'start_date',
  'end_date',
  'match_status',
  'all_statuses',
  'matched',
  'review',
  'unmatched',
  'clear_filters',
  'empty_title',
  'empty_body',
  'choose_csv',
  'drop_csv',
  'max_upload',
  'local_import_privacy',
  'instructions_title',
  'import',
  'enrich',
  'retry',
  'cancel',
  'replace',
  'export_csv',
  'delete_all',
  'load_more',
  'activities',
  'unique_titles',
  'date_range',
  'metadata_coverage',
  'match_coverage',
  'monthly_activity',
  'weekday_genres',
  'top_titles',
  'media_types',
  'genres',
  'genres_by_viewing_year',
  'languages',
  'origin_countries',
  'release_years',
  'rating_bands',
  'runtime_bands',
  'seasons',
  'episodes',
  'active_generation',
  'details',
  'actions',
  'imported',
  'source_rows',
  'analysis',
  'tmdb',
  'configured',
  'not_configured',
  'local_privacy',
  'tmdb_boundary',
  'enrich_title',
  'enrich_disclosure',
  'enrich_query_only',
  'confirm_enrich',
  'delete_title',
  'delete_disclosure',
  'confirm_delete',
  'replace_title',
  'replace_disclosure',
  'confirm_replace',
  'dismiss',
  'not_configured_body',
  'review_notice',
  'replacement_notice',
  'canceled_notice',
  'failure_notice',
  'error_notice',
  'filter_pair_required',
  'invalid_csv',
  'empty_results',
  'data_table',
  'count',
  'date',
  'title',
  'type',
  'release',
  'rating',
  'runtime',
  'series',
  'outcome',
  'candidate',
  'score',
  'reason',
  'unknown',
  'movie',
  'loading',
  'credits_title',
  'credits_intro',
  'tmdb_credit',
  'official_website',
  'no_external_assets',
  'guide_title',
  'guide_steps_title',
  'official_help',
  'file_selected',
  'import_started',
  'enrichment_started',
  'generation_canceled',
  'provider_deleted',
  'filters_applied',
  'page_loaded',
  'cached',
  'progress'
]);

const state = {
  data: null,
  capabilities: null,
  locale: 'en',
  theme: 'dark',
  route: {name: 'catalog'},
  view: 'overview',
  filter: {
    startDate: '',
    endDate: '',
    matchStatus: ''
  },
  netflix: null,
  analytics: null,
  records: [],
  nextCursor: '',
  dataKey: '',
  dataLoading: false,
  dataError: '',
  actionBusy: false,
  actionError: null,
  notice: null,
  eventSequences: new Map(),
  lastEvent: null,
  pollTimer: 0,
  pollController: null,
  dataController: null,
  actionController: null
};

const app = document.querySelector('#app');
const announcer = document.querySelector('#app-announcer');

boot().catch((error) => {
  state.actionError = normalizeError(error);
  renderFatal();
});

async function boot() {
  initializeTheme();
  attachGlobalHandlers();
  const initialController = new AbortController();
  const [data, capabilities, netflix] = await Promise.all([
    fetchJSON('data.json', initialController.signal),
    initializeAPI(initialController.signal),
    getNetflixProvider(initialController.signal)
  ]);
  state.data = validateAppData(data);
  state.capabilities = capabilities;
  state.netflix = netflix;
  state.locale = initialLocale();
  document.documentElement.lang = state.locale;
  state.route = parseRoute();
  render();
  schedulePoll();
}

function attachGlobalHandlers() {
  window.addEventListener('hashchange', () => {
    state.route = parseRoute();
    resetWorkspaceData();
    render();
  });
  window.addEventListener('beforeunload', cleanupAll, {once: true});
  document.addEventListener('click', handleClick);
  document.addEventListener('change', handleChange);
  document.addEventListener('keydown', handleKeyDown);
  document.addEventListener('dragover', handleDragOver);
  document.addEventListener('dragleave', handleDragLeave);
  document.addEventListener('drop', handleDrop);
}

function handleKeyDown(event) {
  const selectedTab = event.target.closest('[role="tab"]');
  const tabList = event.target.closest('[role="tablist"]');
  if (!selectedTab || !tabList) {
    return;
  }
  const tabs = [...tabList.querySelectorAll('[role="tab"]')];
  const currentIndex = tabs.indexOf(selectedTab);
  let nextIndex = currentIndex;
  if (event.key === 'ArrowRight') {
    nextIndex = (currentIndex + 1) % tabs.length;
  } else if (event.key === 'ArrowLeft') {
    nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
  } else if (event.key === 'Home') {
    nextIndex = 0;
  } else if (event.key === 'End') {
    nextIndex = tabs.length - 1;
  } else {
    return;
  }
  event.preventDefault();
  tabs[nextIndex].focus();
  tabs[nextIndex].click();
}

async function handleClick(event) {
  const languageButton = event.target.closest('[data-language]');
  if (languageButton) {
    setLocale(languageButton.dataset.language);
    return;
  }

  if (event.target.closest('#theme-toggle')) {
    setTheme(state.theme === 'dark' ? 'light' : 'dark');
    return;
  }

  const routeButton = event.target.closest('[data-route]');
  if (routeButton) {
    navigate(routeButton.dataset.route, routeButton.dataset.provider || '');
    return;
  }

  const viewButton = event.target.closest('[data-view]');
  if (viewButton) {
    state.view = viewButton.dataset.view;
    resetWorkspaceData();
    render();
    return;
  }

  const actionButton = event.target.closest('[data-action]');
  if (!actionButton || state.actionBusy) {
    return;
  }
  const action = actionButton.dataset.action;
  if (action === 'choose-file' || action === 'replace') {
    /** @type {HTMLInputElement | null} */
    const fileInput = document.querySelector('#netflix-file');
    fileInput?.click();
  } else if (action === 'clear-filters') {
    state.filter = {startDate: '', endDate: '', matchStatus: ''};
    resetWorkspaceData();
    announce(ui().filters_applied);
    render();
  } else if (action === 'enrich' || action === 'retry-enrichment') {
    await beginEnrichment();
  } else if (action === 'cancel-generation') {
    await cancelBuildingGeneration();
  } else if (action === 'delete-provider') {
    await confirmProviderDeletion();
  } else if (action === 'load-more') {
    await loadMoreRecords();
  } else if (action === 'dismiss-notice') {
    state.notice = null;
    render();
  }
}

async function handleChange(event) {
  if (event.target.matches('#netflix-file')) {
    const [file] = event.target.files;
    if (file) {
      await beginLocalImport(file, Boolean(state.netflix?.active_generation));
    }
    event.target.value = '';
    return;
  }
  if (event.target.matches('[data-filter]')) {
    state.filter[event.target.dataset.filter] = event.target.value;
    resetWorkspaceData();
    if (validFilterPair()) {
      announce(ui().filters_applied);
    }
    render();
  }
}

function handleDragOver(event) {
  const dropZone = event.target.closest('.drop-zone');
  if (!dropZone) {
    return;
  }
  event.preventDefault();
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'copy';
  }
  dropZone.dataset.dragging = 'true';
}

function handleDragLeave(event) {
  const dropZone = event.target.closest('.drop-zone');
  if (dropZone && !dropZone.contains(event.relatedTarget)) {
    dropZone.dataset.dragging = 'false';
  }
}

async function handleDrop(event) {
  const dropZone = event.target.closest('.drop-zone');
  if (!dropZone || state.actionBusy) {
    return;
  }
  event.preventDefault();
  dropZone.dataset.dragging = 'false';
  const [file] = event.dataTransfer?.files || [];
  if (file) {
    await beginLocalImport(file, Boolean(state.netflix?.active_generation));
  }
}

function render() {
  if (!state.data || !state.netflix) {
    return;
  }
  updateChrome();
  if (state.route.name === 'netflix') {
    renderNetflixWorkspace();
  } else if (state.route.name === 'guide') {
    renderGuide(state.route.provider);
  } else if (state.route.name === 'credits') {
    renderCredits();
  } else {
    renderCatalog();
  }
}

function renderCatalog() {
  setHeaderContext(ui().provider_catalog);
  const root = element('div', {class: 'catalog'});
  const heading = element('div', {class: 'page-heading'});
  const copy = element('div');
  copy.append(
    element('p', {class: 'page-kicker', text: ui().local_only}),
    element('h1', {text: ui().catalog_title}),
    element('p', {class: 'lede', text: ui().catalog_intro})
  );
  heading.append(copy);

  const list = element('section', {
    class: 'catalog-list',
    'aria-label': ui().provider_catalog
  });
  for (const providerDefinition of state.data.provider_registry) {
    const localized = localizedProvider(providerDefinition.id);
    const row = element('article', {
      class: 'provider-row',
      'data-provider-id': providerDefinition.id
    });
    row.append(
      providerMark(providerDefinition.id, localized.title),
      element(
        'div',
        {},
        element('span', {class: 'provider-name', text: localized.title}),
        element('span', {
          class: 'provider-type',
          text: providerDefinition.surface === 'workspace' ? ui().workspace : ui().guide
        })
      ),
      element('p', {class: 'provider-summary', text: localized.intro})
    );
    const actions = element('div', {class: 'row-actions'});
    if (providerDefinition.id === 'netflix') {
      const presentation = netflixStatePresentation();
      actions.append(
        stateChip(presentation.label, presentation.tone),
        actionButton(ui().open, {
          'data-route': 'netflix',
          class: 'button button-primary'
        })
      );
    } else {
      actions.append(
        stateChip(ui().guide, 'neutral'),
        actionButton(ui().view_guide, {
          'data-route': 'guide',
          'data-provider': providerDefinition.id
        })
      );
    }
    row.append(actions);
    list.append(row);
  }
  root.append(heading, list);
  replaceApp(root);
}

function renderGuide(providerID) {
  const provider = localizedProvider(providerID);
  if (!provider || providerID === 'netflix') {
    navigate('catalog');
    return;
  }
  setHeaderContext(provider.title);
  const root = element('div', {class: 'guide'});
  const heading = element('div', {class: 'page-heading'});
  const copy = element('div');
  copy.append(
    element(
      'a',
      {class: 'back-link', href: '#catalog', text: `← ${ui().back_catalog}`}
    ),
    element('p', {class: 'page-kicker', text: ui().guide_title}),
    element('h1', {text: provider.title}),
    element('p', {class: 'lede', text: provider.intro})
  );
  heading.append(copy, stateChip(ui().guide, 'neutral'));

  const section = element('section', {
    id: provider.id,
    class: 'panel panel-raised guide-card'
  });
  const orderedList = element('ol', {class: 'instruction-list'});
  provider.steps.forEach((step) => orderedList.append(element('li', {text: step})));
  section.append(element('h2', {text: ui().guide_steps_title}), orderedList);

  if (provider.refs.length) {
    const refs = element('ul', {
      class: 'guide-refs',
      'aria-label': ui().official_help
    });
    provider.refs.forEach((reference) => {
      refs.append(
        element(
          'li',
          {},
          element('a', {
            href: reference.href,
            target: '_blank',
            rel: 'noopener',
            text: reference.label
          })
        )
      );
    });
    section.append(refs);
  }

  const assets = state.data.instruction_screenshots[providerID] || [];
  if (assets.length) {
    const screenshots = element('div', {class: 'screenshot-grid'});
    assets.forEach((asset, index) => {
      screenshots.append(
        element('img', {
          class: 'instruction-screenshot',
          src: asset.src,
          alt: provider.images[index].alt,
          loading: 'lazy',
          decoding: 'async',
          'data-screenshot-id': asset.id
        })
      );
    });
    section.append(screenshots);
  }
  if (provider.note) {
    section.append(element('p', {class: 'panel-copy', text: provider.note}));
  }
  root.append(heading, section);
  replaceApp(root);
}

function renderNetflixWorkspace() {
  const provider = localizedProvider('netflix');
  setHeaderContext(provider.title);
  const root = element('div', {class: 'workspace'});
  const presentation = netflixStatePresentation();
  const heading = element('div', {class: 'workspace-header'});
  const title = element('div', {class: 'workspace-title'});
  title.append(
    element('a', {
      class: 'back-link',
      href: '#catalog',
      'aria-label': ui().back_catalog,
      text: '←'
    }),
    providerMark('netflix', provider.title),
    element(
      'div',
      {},
      element('p', {class: 'eyebrow', text: ui().workspace}),
      element('h1', {text: provider.title})
    )
  );
  heading.append(title, stateChip(presentation.label, presentation.tone));

  const grid = element('div', {class: 'workspace-grid'});
  const main = element('div', {class: 'workspace-main'});
  const rail = element('aside', {
    class: 'workspace-rail',
    'aria-label': ui().details
  });

  appendWorkspaceNotices(main);
  if (!state.netflix.active_generation && !state.netflix.building_generation) {
    main.append(renderEmptyImport(provider));
  } else if (!state.netflix.active_generation && state.netflix.building_generation) {
    main.append(renderBuildingOnly());
  } else {
    main.append(renderWorkspaceToolbar(), renderActiveWorkspace());
  }
  rail.append(...renderStateRail());
  grid.append(main, rail);
  root.append(heading, grid);
  replaceApp(root);

  if (state.netflix.active_generation && validFilterPair()) {
    queueMicrotask(ensureWorkspaceData);
  }
}

function renderEmptyImport(provider) {
  const panel = element('section', {class: 'panel panel-raised import-panel'});
  const layout = element('div', {class: 'import-layout'});
  const importCopy = element('div');
  importCopy.append(
    element('p', {class: 'eyebrow', text: ui().local_only}),
    element('h2', {text: ui().empty_title}),
    element('p', {class: 'panel-copy', text: ui().empty_body}),
    element('p', {class: 'privacy-note', text: ui().local_import_privacy})
  );
  const dropZone = element('label', {class: 'drop-zone'});
  dropZone.append(
    element('input', {
      id: 'netflix-file',
      type: 'file',
      accept: '.csv,text/csv',
      disabled: state.actionBusy
    }),
    element(
      'span',
      {},
      element('span', {class: 'drop-title', text: ui().drop_csv}),
      element('span', {
        class: 'drop-help',
        text: `${ui().max_upload}: ${formatBytes(state.netflix.capabilities.max_upload_bytes)}`
      }),
      element('span', {class: 'button button-primary', text: ui().choose_csv})
    )
  );
  importCopy.append(dropZone);

  const instructions = element('div');
  instructions.append(element('h3', {text: ui().instructions_title}));
  const list = element('ol', {class: 'instruction-list'});
  provider.steps.forEach((step) => list.append(element('li', {text: step})));
  instructions.append(list);
  const official = provider.refs[0];
  if (official) {
    instructions.append(
      element(
        'p',
        {class: 'guide-refs'},
        element('a', {
          href: official.href,
          target: '_blank',
          rel: 'noopener',
          text: official.label
        })
      )
    );
  }
  layout.append(importCopy, instructions);
  panel.append(layout);
  return panel;
}

function renderBuildingOnly() {
  const building = state.netflix.building_generation;
  const panel = element('section', {class: 'panel panel-raised import-panel'});
  panel.append(
    element('p', {class: 'eyebrow', text: generationStateLabel(building)}),
    element('h2', {text: generationStateLabel(building)}),
    element('p', {class: 'panel-copy', text: ui().local_import_privacy}),
    progressBar(building),
    actionButton(ui().cancel, {
      'data-action': 'cancel-generation',
      class: 'button button-danger',
      disabled: state.actionBusy
    })
  );
  return panel;
}

function renderWorkspaceToolbar() {
  const toolbar = element('div', {class: 'toolbar'});
  const tabs = element('div', {class: 'tabs', role: 'tablist'});
  VIEWS.forEach((view) => {
    tabs.append(
      element('button', {
        id: `netflix-view-${view}`,
        class: 'tab',
        type: 'button',
        role: 'tab',
        'data-view': view,
        'aria-controls': 'netflix-view-panel',
        'aria-selected': state.view === view ? 'true' : 'false',
        tabindex: state.view === view ? '0' : '-1',
        text: ui()[view]
      })
    );
  });
  toolbar.append(tabs, filterControls());
  return toolbar;
}

function filterControls() {
  const filters = element('div', {class: 'filters'});
  filters.append(
    element('input', {
      class: 'filter-date',
      type: 'date',
      value: state.filter.startDate,
      min: String(state.netflix.capabilities.minimum_viewing_year) + '-01-01',
      'data-filter': 'startDate',
      'aria-label': ui().start_date
    }),
    element('input', {
      class: 'filter-date',
      type: 'date',
      value: state.filter.endDate,
      min: String(state.netflix.capabilities.minimum_viewing_year) + '-01-01',
      'data-filter': 'endDate',
      'aria-label': ui().end_date
    })
  );
  const select = element('select', {
    'data-filter': 'matchStatus',
    'aria-label': ui().match_status
  });
  select.append(element('option', {value: '', text: ui().all_statuses}));
  MATCH_STATUSES.forEach((status) => {
    select.append(
      element('option', {
        value: status,
        selected: state.filter.matchStatus === status,
        text: ui()[status]
      })
    );
  });
  filters.append(
    select,
    actionButton(ui().clear_filters, {
      'data-action': 'clear-filters',
      class: 'button button-quiet'
    })
  );
  return filters;
}

function renderActiveWorkspace() {
  const content = element('div', {
    id: 'netflix-view-panel',
    class: 'workspace-content',
    role: 'tabpanel',
    'aria-labelledby': `netflix-view-${state.view}`
  });
  const building = state.netflix.building_generation;
  if (building) {
    const notice = element('div', {class: 'alert', 'data-tone': 'warning'});
    notice.append(
      element('span', {class: 'alert-icon', text: '↻'}),
      element(
        'div',
        {},
        element('strong', {text: ui().replacement_notice}),
        progressBar(building)
      ),
      actionButton(ui().cancel, {
        'data-action': 'cancel-generation',
        class: 'button button-danger',
        disabled: state.actionBusy
      })
    );
    content.append(notice);
  }
  if (!validFilterPair()) {
    content.append(
      alertNode('warning', ui().filter_pair_required)
    );
    return content;
  }
  if (state.dataError) {
    content.append(alertNode('danger', state.dataError));
  }
  if (state.dataLoading && !state.analytics) {
    content.append(element('section', {class: 'panel loading-block', 'aria-label': ui().loading}));
    return content;
  }
  if (!state.analytics) {
    content.append(element('section', {class: 'panel', text: ui().loading}));
    return content;
  }
  if (state.view === 'catalog') {
    content.append(renderCatalogView());
  } else if (state.view === 'match_quality') {
    content.append(renderMatchQualityView());
  } else {
    content.append(renderOverview());
  }
  return content;
}

function renderOverview() {
  const data = state.analytics.data;
  const fragment = document.createDocumentFragment();
  fragment.append(
    renderKPIs(data),
    element(
      'div',
      {class: 'chart-grid'},
      seriesChart({
        title: ui().monthly_activity,
        labels: data.month_labels,
        series: data.monthly_media,
        emptyLabel: ui().empty_results,
        tableLabel: ui().data_table,
        valueLabel: ui().activities,
        formatLabel: formatMonth,
        formatSeries: translateDimension
      }),
      seriesChart({
        title: ui().weekday_genres,
        labels: data.weekday_labels,
        series: data.genres_by_weekday,
        emptyLabel: ui().empty_results,
        tableLabel: ui().data_table,
        valueLabel: ui().activities,
        formatLabel: translateWeekday
      }),
      countChart({
        title: ui().top_titles,
        counts: data.top_titles,
        emptyLabel: ui().empty_results,
        tableLabel: ui().title,
        valueLabel: ui().activities
      }),
      countChart({
        title: ui().genres,
        counts: data.genres,
        emptyLabel: ui().empty_results,
        tableLabel: ui().genres,
        valueLabel: ui().activities
      })
    )
  );
  return fragment;
}

function renderCatalogView() {
  const data = state.analytics.data;
  const fragment = document.createDocumentFragment();
  fragment.append(renderKPIs(data));
  const dimensions = element('div', {class: 'dimension-grid'});
  const charts = [
    {field: 'media_types', titleKey: 'media_types', formatter: translateDimension},
    {field: 'genres', titleKey: 'genres'},
    {field: 'languages', titleKey: 'languages', formatter: translateDimension},
    {field: 'origin_countries', titleKey: 'origin_countries', formatter: translateDimension},
    {field: 'release_years', titleKey: 'release_years', formatter: translateDimension},
    {field: 'rating_bands', titleKey: 'rating_bands', formatter: translateDimension},
    {field: 'runtime_bands', titleKey: 'runtime_bands', formatter: translateDimension},
    {field: 'season_counts', titleKey: 'seasons', formatter: translateDimension},
    {field: 'episode_bands', titleKey: 'episodes', formatter: translateDimension}
  ];
  charts.forEach(({field, titleKey, formatter}) => {
    dimensions.append(
      countChart({
        title: ui()[titleKey],
        counts: data[field],
        emptyLabel: ui().empty_results,
        tableLabel: ui()[titleKey],
        valueLabel: ui().activities,
        formatLabel: formatter || ((value) => value)
      })
    );
  });
  fragment.append(
    dimensions,
    seriesChart({
      title: ui().genres_by_viewing_year,
      labels: data.viewing_years.map(String),
      series: data.genres_by_viewing_year,
      emptyLabel: ui().empty_results,
      tableLabel: ui().data_table,
      valueLabel: ui().activities
    }),
    renderRecordsTable(false)
  );
  return fragment;
}

function renderMatchQualityView() {
  const data = state.analytics.data;
  const fragment = document.createDocumentFragment();
  const coverage = element('div', {class: 'kpi-grid'});
  for (const status of MATCH_STATUSES) {
    const titleCount = countFor(data.match_status_titles, status);
    const activityCount = countFor(data.match_status_activities, status);
    coverage.append(
      kpi(
        ui()[status],
        formatNumber(titleCount),
        `${formatNumber(activityCount)} ${ui().activities.toLowerCase()}`
      )
    );
  }
  coverage.append(
    kpi(
      ui().cached,
      formatNumber(state.netflix.active_generation.cache_hit_title_count),
      ui().unique_titles
    )
  );
  fragment.append(
    coverage,
    countChart({
      title: ui().match_coverage,
      counts: data.match_status_titles,
      emptyLabel: ui().empty_results,
      tableLabel: ui().outcome,
      valueLabel: ui().unique_titles,
      formatLabel: translateDimension
    })
  );
  if (state.netflix.active_generation.review_title_count > 0) {
    fragment.append(alertNode('warning', ui().review_notice));
  }
  fragment.append(renderRecordsTable(true));
  return fragment;
}

function renderKPIs(data) {
  const grid = element('section', {
    class: 'kpi-grid',
    'aria-label': ui().overview
  });
  const metadataPercent = data.activity_count
    ? Math.round((data.metadata_activity_count / data.activity_count) * 100)
    : 0;
  grid.append(
    kpi(ui().activities, formatNumber(data.activity_count)),
    kpi(ui().unique_titles, formatNumber(data.unique_title_count)),
    kpi(
      ui().date_range,
      data.start_date && data.end_date
        ? `${formatDate(data.start_date)} – ${formatDate(data.end_date)}`
        : '—'
    ),
    kpi(
      ui().metadata_coverage,
      `${metadataPercent}%`,
      `${formatNumber(data.metadata_activity_count)} / ${formatNumber(data.activity_count)}`
    )
  );
  return grid;
}

function renderRecordsTable(matchQuality) {
  const panel = element('section', {class: 'panel'});
  const header = element('div', {class: 'panel-header'});
  header.append(
    element(
      'div',
      {},
      element('h2', {text: matchQuality ? ui().match_quality : ui().catalog}),
      element('p', {
        class: 'panel-copy',
        text: `${formatNumber(state.records.length)} ${ui().activities.toLowerCase()}`
      })
    )
  );
  panel.append(header);
  if (!state.records.length) {
    panel.append(element('p', {class: 'empty-copy', text: ui().empty_results}));
    return panel;
  }
  const wrapper = element('div', {class: 'table-scroll'});
  const table = element('table');
  const head = element('thead');
  const headRow = element('tr');
  const headers = matchQuality
    ? [ui().date, ui().title, ui().outcome, ui().candidate, ui().score, ui().reason]
    : [ui().date, ui().title, ui().type, ui().genres, ui().release, ui().rating, ui().runtime, ui().series, ui().outcome];
  headers.forEach((headerLabel) => headRow.append(element('th', {scope: 'col', text: headerLabel})));
  head.append(headRow);
  const body = element('tbody');
  state.records.forEach((record) => {
    body.append(matchQuality ? matchRecordRow(record) : catalogRecordRow(record));
  });
  table.append(head, body);
  wrapper.append(table);
  panel.append(wrapper);
  if (state.nextCursor) {
    panel.append(
      actionButton(state.dataLoading ? ui().loading : ui().load_more, {
        'data-action': 'load-more',
        class: 'button',
        disabled: state.dataLoading
      })
    );
  }
  return panel;
}

function catalogRecordRow(record) {
  const row = element('tr');
  const metadata = record.metadata;
  const matchStatus = record.match?.status || '';
  row.append(
    element('td', {text: formatDate(record.date_iso)}),
    element(
      'th',
      {scope: 'row', class: 'record-title'},
      document.createTextNode(record.title),
      record.derived_title !== record.title
        ? element('span', {class: 'record-detail', text: record.derived_title})
        : null
    ),
    element('td', {text: metadata ? translateDimension(metadata.media_type) : ui().unknown}),
    element('td', {text: metadata?.genres?.join(', ') || ui().unknown}),
    element('td', {text: metadata?.release_date ? formatDate(metadata.release_date) : ui().unknown}),
    element('td', {
      text: metadata?.vote_average !== undefined
        ? `${metadata.vote_average.toFixed(1)} (${formatNumber(metadata.vote_count || 0)})`
        : ui().unknown
    }),
    element('td', {
      text: metadata?.runtime_minutes
        ? `${formatNumber(metadata.runtime_minutes)} min`
        : ui().unknown
    }),
    element('td', {
      text: metadata?.seasons || metadata?.episodes
        ? `${metadata.seasons || '—'} / ${metadata.episodes || '—'}`
        : '—'
    }),
    statusCell(matchStatus)
  );
  return row;
}

function matchRecordRow(record) {
  const match = record.match;
  const evidence = match?.evidence;
  const row = element('tr');
  row.append(
    element('td', {text: formatDate(record.date_iso)}),
    element(
      'th',
      {scope: 'row', class: 'record-title'},
      document.createTextNode(record.title),
      element('span', {class: 'record-detail', text: record.derived_title})
    ),
    statusCell(match?.status || ''),
    element('td', {text: evidence?.best_candidate_title || '—'}),
    element('td', {
      text: evidence ? `${Math.round(evidence.best_score * 100)}%` : '—'
    }),
    element('td', {text: evidence?.reason || '—'})
  );
  return row;
}

function renderStateRail() {
  const active = state.netflix.active_generation;
  const building = state.netflix.building_generation;
  const details = element('section', {class: 'panel rail-section'});
  details.append(element('h2', {text: ui().active_generation}));
  const list = element('dl', {class: 'rail-list'});
  if (active) {
    list.append(
      definition(ui().analysis, active.analysis_level === 'tmdb' ? 'TMDB' : ui().local_only),
      definition(ui().imported, formatDateTime(active.completed_at || active.updated_at)),
      definition(ui().source_rows, formatNumber(active.activity_count)),
      definition(ui().unique_titles, formatNumber(active.unique_title_count))
    );
  } else {
    list.append(definition(ui().analysis, ui().state_empty));
  }
  details.append(list);

  const tmdb = element('section', {class: 'panel rail-section'});
  tmdb.append(element('h2', {text: ui().tmdb}));
  tmdb.append(
    stateChip(
      state.netflix.capabilities.tmdb_configured ? ui().configured : ui().not_configured,
      state.netflix.capabilities.tmdb_configured ? 'success' : 'warning'
    )
  );
  if (!state.netflix.capabilities.tmdb_configured) {
    tmdb.append(
      element(
        'p',
        {class: 'privacy-note'},
        document.createTextNode(ui().not_configured_body + ' '),
        element('code', {text: 'DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN'})
      )
    );
  } else {
    tmdb.append(element('p', {class: 'privacy-note', text: ui().tmdb_boundary}));
  }

  const actions = element('section', {class: 'panel rail-section'});
  actions.append(element('h2', {text: ui().actions}));
  const actionList = element('div', {class: 'rail-actions'});
  const failed = actionableFailedGeneration();
  if (active && !building) {
    if (active.analysis_level === 'local') {
      actionList.append(
        actionButton(ui().enrich, {
          'data-action': 'enrich',
          class: 'button button-primary',
          disabled: state.actionBusy || !state.netflix.capabilities.tmdb_configured
        })
      );
    } else {
      actionList.append(
        element('a', {
          class: 'button button-primary',
          href: exportGenerationURL(active.id),
          download: '',
          text: ui().export_csv
        })
      );
    }
    actionList.append(
      actionButton(ui().replace, {
        'data-action': 'replace',
        disabled: state.actionBusy
      }),
      element('input', {
        id: 'netflix-file',
        hidden: true,
        type: 'file',
        accept: '.csv,text/csv',
        disabled: state.actionBusy
      })
    );
  }
  if (building) {
    actionList.append(
      actionButton(ui().cancel, {
        'data-action': 'cancel-generation',
        class: 'button button-danger',
        disabled: state.actionBusy
      })
    );
  }
  if (failed && !building) {
    actionList.append(
      actionButton(ui().retry, {
        'data-action': failed.analysis_level === 'tmdb' ? 'retry-enrichment' : 'replace',
        disabled: state.actionBusy
      })
    );
  }
  if (active || failed) {
    actionList.append(
      actionButton(ui().delete_all, {
        'data-action': 'delete-provider',
        class: 'button button-danger',
        disabled: state.actionBusy
      })
    );
  }
  actions.append(actionList);

  const privacy = element('section', {class: 'panel rail-section'});
  privacy.append(
    element('h2', {text: ui().local_privacy}),
    element('p', {class: 'privacy-note', text: ui().local_import_privacy})
  );
  return [details, tmdb, actions, privacy];
}

function appendWorkspaceNotices(main) {
  if (state.notice) {
    const notice = alertNode(state.notice.tone, state.notice.message);
    notice.append(
      actionButton(ui().dismiss, {
        'data-action': 'dismiss-notice',
        class: 'button button-quiet'
      })
    );
    main.append(notice);
  }
  if (state.actionError) {
    main.append(alertNode('danger', formatError(state.actionError)));
  }
  const failed = actionableFailedGeneration();
  if (failed && !state.netflix.building_generation) {
    const label = failed.failure?.code === 'canceled'
      ? ui().canceled_notice
      : ui().failure_notice;
    main.append(
      alertNode(
        failed.failure?.code === 'canceled' ? 'warning' : 'danger',
        `${label} (${failed.failure?.code || 'failed'})`
      )
    );
  }
  if (
    state.netflix.active_generation?.analysis_level === 'tmdb' &&
    state.netflix.active_generation.review_title_count > 0 &&
    state.view !== 'match_quality'
  ) {
    main.append(alertNode('warning', ui().review_notice));
  }
}

function renderCredits() {
  setHeaderContext(ui().credits);
  const attribution = state.netflix.capabilities.tmdb_attribution;
  const root = element('div', {class: 'credits'});
  const heading = element('div', {class: 'page-heading'});
  const copy = element('div');
  copy.append(
    element('a', {class: 'back-link', href: '#catalog', text: `← ${ui().back_catalog}`}),
    element('p', {class: 'page-kicker', text: ui().credits}),
    element('h1', {text: ui().credits_title}),
    element('p', {class: 'lede', text: ui().credits_intro})
  );
  heading.append(copy);
  const panel = element('section', {class: 'panel panel-raised tmdb-credit'});
  panel.append(
    element('img', {
      class: 'tmdb-logo',
      src: 'images/tmdb-blue-square.svg',
      alt: 'TMDB'
    }),
    element(
      'div',
      {},
      element('h2', {text: ui().tmdb_credit}),
      element('p', {text: attribution.notice}),
      element(
        'p',
        {},
        element('a', {
          href: attribution.website,
          target: '_blank',
          rel: 'noopener',
          text: ui().official_website
        })
      )
    )
  );
  const local = element('section', {class: 'panel'});
  local.append(
    element('h2', {text: ui().local_only}),
    element('p', {class: 'panel-copy', text: ui().no_external_assets})
  );
  root.append(heading, panel, local);
  replaceApp(root);
}

async function beginLocalImport(file, replacing) {
  if (!validViewingActivityFile(file)) {
    state.actionError = {code: 'invalid_csv', row: 0};
    render();
    announce(ui().invalid_csv);
    return;
  }
  if (replacing) {
    const confirmed = await confirmDialog({
      title: ui().replace_title,
      body: ui().replace_disclosure,
      confirmLabel: ui().confirm_replace
    });
    if (!confirmed) {
      return;
    }
  }
  runAction(async (signal) => {
    const generation = await createLocalGeneration(signal);
    mergeBuildingGeneration(generation);
    announce(`${ui().file_selected}: ${file.name}`);
    render();
    const staged = await uploadViewingActivity(generation.id, file, signal);
    mergeBuildingGeneration(staged);
    state.notice = {tone: 'success', message: ui().import_started};
    announce(ui().import_started);
    render();
    schedulePoll(true);
  });
}

async function beginEnrichment() {
  const active = state.netflix.active_generation;
  if (!active || active.state !== 'ready') {
    return;
  }
  if (!state.netflix.capabilities.tmdb_configured) {
    state.actionError = {code: 'not_configured', row: 0};
    render();
    return;
  }
  const confirmed = await confirmDialog({
    title: ui().enrich_title,
    body: `${ui().enrich_disclosure}\n\n${ui().enrich_query_only}`,
    confirmLabel: ui().confirm_enrich
  });
  if (!confirmed) {
    return;
  }
  runAction(async (signal) => {
    const generation = await createTMDBGeneration(
      active.id,
      LOCALE_TO_TMDB[state.locale],
      signal
    );
    mergeBuildingGeneration(generation);
    state.notice = {tone: 'success', message: ui().enrichment_started};
    announce(ui().enrichment_started);
    render();
    schedulePoll(true);
  });
}

async function cancelBuildingGeneration() {
  const building = state.netflix.building_generation;
  if (!building) {
    return;
  }
  runAction(async (signal) => {
    await cancelGeneration(building.id, signal);
    state.notice = {tone: 'warning', message: ui().generation_canceled};
    announce(ui().generation_canceled);
    await refreshNetflix(signal);
  });
}

async function confirmProviderDeletion() {
  const confirmed = await confirmDialog({
    title: ui().delete_title,
    body: ui().delete_disclosure,
    confirmLabel: ui().confirm_delete,
    danger: true
  });
  if (!confirmed) {
    return;
  }
  runAction(async (signal) => {
    await deleteNetflixProvider(signal);
    resetWorkspaceData();
    state.notice = {tone: 'success', message: ui().provider_deleted};
    announce(ui().provider_deleted);
    await refreshNetflix(signal);
  });
}

async function runAction(operation) {
  cleanupAction();
  const controller = new AbortController();
  state.actionController = controller;
  state.actionBusy = true;
  state.actionError = null;
  render();
  try {
    await operation(controller.signal);
  } catch (error) {
    if (error.name !== 'AbortError') {
      state.actionError = normalizeError(error);
      await refreshNetflixWithoutThrow();
      announce(formatError(state.actionError));
    }
  } finally {
    if (state.actionController === controller) {
      state.actionController = null;
    }
    state.actionBusy = false;
    render();
    schedulePoll();
  }
}

async function ensureWorkspaceData() {
  const active = state.netflix.active_generation;
  if (!active || active.state !== 'ready' || !validFilterPair()) {
    return;
  }
  const key = dataRequestKey(active.id);
  if (state.dataLoading || state.dataKey === key) {
    return;
  }
  cleanupDataRequest();
  const controller = new AbortController();
  state.dataController = controller;
  state.dataLoading = true;
  state.dataError = '';
  try {
    const [analytics, page] = await Promise.all([
      getAnalytics(active.id, state.filter, controller.signal),
      getRecords(
        active.id,
        state.filter,
        '',
        state.netflix.capabilities.max_record_page_size,
        controller.signal
      )
    ]);
    state.analytics = analytics;
    state.records = page.records;
    state.nextCursor = page.next_cursor || '';
    state.dataKey = key;
  } catch (error) {
    if (error.name !== 'AbortError') {
      state.dataError = formatError(normalizeError(error));
    }
  } finally {
    if (state.dataController === controller) {
      state.dataController = null;
    }
    state.dataLoading = false;
    if (state.route.name === 'netflix') {
      render();
    }
  }
}

async function loadMoreRecords() {
  const active = state.netflix.active_generation;
  if (!active || !state.nextCursor || state.dataLoading) {
    return;
  }
  cleanupDataRequest();
  const controller = new AbortController();
  state.dataController = controller;
  state.dataLoading = true;
  render();
  try {
    const page = await getRecords(
      active.id,
      state.filter,
      state.nextCursor,
      state.netflix.capabilities.max_record_page_size,
      controller.signal
    );
    state.records = [...state.records, ...page.records];
    state.nextCursor = page.next_cursor || '';
    announce(ui().page_loaded);
  } catch (error) {
    if (error.name !== 'AbortError') {
      state.dataError = formatError(normalizeError(error));
    }
  } finally {
    if (state.dataController === controller) {
      state.dataController = null;
    }
    state.dataLoading = false;
    render();
  }
}

async function refreshNetflix(signal) {
  const previousActiveID = state.netflix?.active_generation?.id || '';
  const snapshot = await getNetflixProvider(signal);
  state.netflix = snapshot;
  const currentActiveID = snapshot.active_generation?.id || '';
  if (previousActiveID !== currentActiveID) {
    resetWorkspaceData();
  }
  state.lastEvent = null;
  render();
  schedulePoll();
}

async function refreshNetflixWithoutThrow() {
  const controller = new AbortController();
  try {
    await refreshNetflix(controller.signal);
  } catch {
    // The original typed action failure remains the authoritative notice.
  }
}

function schedulePoll(immediate = false) {
  window.clearTimeout(state.pollTimer);
  state.pollTimer = 0;
  if (!state.netflix?.building_generation) {
    cleanupPoll();
    return;
  }
  state.pollTimer = window.setTimeout(pollBuildingGeneration, immediate ? 0 : 350);
}

async function pollBuildingGeneration() {
  cleanupPoll();
  const building = state.netflix?.building_generation;
  if (!building) {
    return;
  }
  const controller = new AbortController();
  state.pollController = controller;
  const after = state.eventSequences.get(building.id) || 0;
  try {
    const [events, snapshot] = await Promise.all([
      getGenerationEvents(building.id, after, controller.signal),
      getNetflixProvider(controller.signal)
    ]);
    state.eventSequences.set(building.id, events.last_sequence);
    if (events.events.length) {
      state.lastEvent = events.events.at(-1);
    }
    const priorActive = state.netflix.active_generation?.id || '';
    state.netflix = snapshot;
    if ((snapshot.active_generation?.id || '') !== priorActive) {
      resetWorkspaceData();
    }
    render();
  } catch (error) {
    if (error.name !== 'AbortError') {
      state.actionError = normalizeError(error);
      render();
    }
  } finally {
    if (state.pollController === controller) {
      state.pollController = null;
    }
    schedulePoll();
  }
}

function mergeBuildingGeneration(generation) {
  state.netflix = {
    ...state.netflix,
    state: 'building',
    building_generation: generation
  };
  state.eventSequences.set(generation.id, 0);
  state.lastEvent = null;
}

function resetWorkspaceData() {
  cleanupDataRequest();
  state.analytics = null;
  state.records = [];
  state.nextCursor = '';
  state.dataKey = '';
  state.dataLoading = false;
  state.dataError = '';
}

function cleanupAll() {
  window.clearTimeout(state.pollTimer);
  cleanupPoll();
  cleanupDataRequest();
  cleanupAction();
}

function cleanupPoll() {
  state.pollController?.abort();
  state.pollController = null;
}

function cleanupDataRequest() {
  state.dataController?.abort();
  state.dataController = null;
}

function cleanupAction() {
  state.actionController?.abort();
  state.actionController = null;
}

function netflixStatePresentation() {
  if (!state.netflix) {
    return {label: ui().state_action_needed, tone: 'danger'};
  }
  const building = state.netflix.building_generation;
  const active = state.netflix.active_generation;
  if (state.netflix.state === 'deleting') {
    return {label: ui().state_deleting, tone: 'warning'};
  }
  if (building) {
    if (active) {
      return {label: ui().state_replacement, tone: 'info'};
    }
    return {
      label: generationStateLabel(building),
      tone: building.state === 'failed' ? 'danger' : 'info'
    };
  }
  if (active?.analysis_level === 'tmdb') {
    if (active.review_title_count > 0) {
      return {label: ui().state_action_needed, tone: 'warning'};
    }
    return {label: ui().state_ready_tmdb, tone: 'success'};
  }
  if (active?.analysis_level === 'local') {
    return {
      label: state.netflix.capabilities.tmdb_configured
        ? ui().state_ready_local
        : ui().state_not_configured,
      tone: state.netflix.capabilities.tmdb_configured ? 'success' : 'warning'
    };
  }
  const failed = actionableFailedGeneration();
  if (failed) {
    const canceled = failed.failure?.code === 'canceled';
    return {
      label: canceled ? ui().state_canceled : ui().state_failed,
      tone: canceled ? 'warning' : 'danger'
    };
  }
  return {label: ui().state_empty, tone: 'neutral'};
}

function actionableFailedGeneration() {
  const failed = state.netflix?.latest_failed_generation;
  const active = state.netflix?.active_generation;
  if (!failed || !active) {
    return failed || null;
  }
  const failedTime = Date.parse(failed.completed_at || failed.updated_at || failed.created_at);
  const activeTime = Date.parse(active.completed_at || active.updated_at || active.created_at);
  if (Number.isNaN(failedTime) || Number.isNaN(activeTime)) {
    return failed;
  }
  return failedTime > activeTime ? failed : null;
}

function generationStateLabel(generation) {
  const key = `state_${generation?.state || 'empty'}`;
  return ui()[key] || ui().state_action_needed;
}

function progressBar(generation) {
  const percent = generation.progress_percent || state.lastEvent?.progress_percent || 0;
  const complete = generation.completed_title_count || state.lastEvent?.completed_title_count || 0;
  const total = generation.unique_title_count || state.lastEvent?.total_title_count || 0;
  const wrapper = element('div');
  wrapper.append(
    element('progress', {
      class: 'progress',
      max: '100',
      value: String(percent),
      'aria-label': ui().progress
    }),
    element(
      'div',
      {class: 'progress-meta'},
      element('span', {text: generationStateLabel(generation)}),
      element('span', {
        text: total ? `${formatNumber(complete)} / ${formatNumber(total)}` : `${percent}%`
      })
    )
  );
  return wrapper;
}

function parseRoute() {
  const raw = window.location.hash.replace(/^#/, '');
  if (raw === 'provider/netflix' || raw === 'netflix') {
    return {name: 'netflix'};
  }
  if (raw === 'credits') {
    return {name: 'credits'};
  }
  if (raw.startsWith('guide/')) {
    const provider = raw.slice('guide/'.length);
    if (GUIDE_SCREENSHOT_PROVIDERS.includes(provider)) {
      return {name: 'guide', provider};
    }
  }
  return {name: 'catalog'};
}

function navigate(route, provider = '') {
  if (route === 'netflix') {
    window.location.hash = 'provider/netflix';
  } else if (route === 'guide') {
    window.location.hash = `guide/${provider}`;
  } else if (route === 'credits') {
    window.location.hash = 'credits';
  } else {
    window.location.hash = 'catalog';
  }
}

function updateChrome() {
  const strings = localized();
  document.title = strings.site_title;
  document.documentElement.lang = state.locale;
  document.querySelector('#brand-label').textContent = strings.site_title;
  document.querySelector('#credits-button').textContent = ui().credits;
  document.querySelector('#footer-local').textContent = ui().local_only;
  document.querySelector('#footer-privacy').textContent = ui().privacy_footer;
  document.querySelector('.skip-link').textContent = ui().skip_to_content;
  document.querySelector('#language-switcher').setAttribute('aria-label', ui().language);
  document.querySelectorAll('[data-language]').forEach((button) => {
    button.setAttribute(
      'aria-pressed',
      button.getAttribute('data-language') === state.locale ? 'true' : 'false'
    );
  });
  const themeToggle = document.querySelector('#theme-toggle');
  themeToggle.setAttribute(
    'aria-label',
    state.theme === 'dark' ? ui().theme_light : ui().theme_dark
  );
}

function setHeaderContext(value) {
  document.querySelector('#header-context').textContent = value;
}

function initialLocale() {
  const saved = localStorage.getItem(STORAGE_KEYS.language);
  if (LOCALES.includes(saved)) {
    return saved;
  }
  const browserLocale = (navigator.language || 'en').slice(0, 2);
  return LOCALES.includes(browserLocale) ? browserLocale : 'en';
}

function setLocale(locale) {
  if (!LOCALES.includes(locale)) {
    return;
  }
  state.locale = locale;
  localStorage.setItem(STORAGE_KEYS.language, locale);
  resetWorkspaceData();
  render();
}

function initializeTheme() {
  const saved = localStorage.getItem(STORAGE_KEYS.theme);
  setTheme(saved === 'light' ? 'light' : 'dark');
}

function setTheme(theme) {
  state.theme = theme;
  document.documentElement.dataset.theme = theme;
  localStorage.setItem(STORAGE_KEYS.theme, theme);
  if (state.data) {
    updateChrome();
  }
}

function localized() {
  return state.data.strings[state.locale];
}

function ui() {
  return localized().ui;
}

function localizedProvider(providerID) {
  return localized().platforms.find((provider) => provider.id === providerID);
}

function validateAppData(data) {
  assertObject(data, 'data.json');
  assertArray(data.provider_registry, 'provider_registry');
  assertObject(data.instruction_screenshots, 'instruction_screenshots');
  assertObject(data.strings, 'strings');
  const providerIDs = data.provider_registry.map((provider) => {
    assertObject(provider, 'provider_registry[]');
    assertString(provider.id, 'provider_registry[].id');
    if (provider.surface !== 'workspace' && provider.surface !== 'guide') {
      throw new Error(`provider ${provider.id} has invalid surface`);
    }
    return provider.id;
  });
  if (new Set(providerIDs).size !== providerIDs.length || providerIDs[0] !== 'netflix') {
    throw new Error('provider_registry must start with one unique netflix provider');
  }
  const netflixDefinition = data.provider_registry.find((provider) => provider.id === 'netflix');
  if (netflixDefinition.surface !== 'workspace') {
    throw new Error('netflix must be workspace-capable');
  }
  GUIDE_SCREENSHOT_PROVIDERS.forEach((providerID) => {
    const definition = data.provider_registry.find((provider) => provider.id === providerID);
    if (!definition || definition.surface !== 'guide') {
      throw new Error(`${providerID} must be guide-only`);
    }
    assertArray(data.instruction_screenshots[providerID], `${providerID} screenshots`);
  });

  LOCALES.forEach((locale) => {
    const strings = data.strings[locale];
    assertObject(strings, `strings.${locale}`);
    assertString(strings.site_title, `strings.${locale}.site_title`);
    assertObject(strings.ui, `strings.${locale}.ui`);
    REQUIRED_UI_KEYS.forEach((key) => {
      assertString(strings.ui[key], `strings.${locale}.ui.${key}`);
      if (!strings.ui[key].trim()) {
        throw new Error(`strings.${locale}.ui.${key} must not be empty`);
      }
    });
    assertArray(strings.ui.weekdays, `strings.${locale}.ui.weekdays`);
    if (strings.ui.weekdays.length !== 7) {
      throw new Error(`strings.${locale}.ui.weekdays must contain seven labels`);
    }
    assertArray(strings.platforms, `strings.${locale}.platforms`);
    if (
      strings.platforms.length !== providerIDs.length ||
      strings.platforms.some((provider, index) => provider.id !== providerIDs[index])
    ) {
      throw new Error(`strings.${locale}.platforms must match provider_registry`);
    }
    strings.platforms.forEach((provider) => {
      for (const key of ['id', 'title', 'intro']) {
        assertString(provider[key], `strings.${locale}.${provider.id}.${key}`);
      }
      assertArray(provider.steps, `${provider.id}.steps`);
      assertArray(provider.refs, `${provider.id}.refs`);
      assertArray(provider.images, `${provider.id}.images`);
      if (
        Object.hasOwn(provider, 'state') ||
        Object.hasOwn(provider, 'status') ||
        Object.hasOwn(provider, 'generation')
      ) {
        throw new Error(`localized provider ${provider.id} contains backend workflow state`);
      }
      const sharedAssets = data.instruction_screenshots[provider.id] || [];
      if (provider.images.length !== sharedAssets.length) {
        throw new Error(`${locale} ${provider.id} image alternatives do not match shared assets`);
      }
    });
  });
  return data;
}

async function fetchJSON(path, signal) {
  const response = await fetch(path, {
    cache: 'no-store',
    credentials: 'same-origin',
    signal
  });
  if (!response.ok) {
    throw new Error(`${path} HTTP ${response.status}`);
  }
  return response.json();
}

function validViewingActivityFile(file) {
  return (
    file instanceof File &&
    file.size > 0 &&
    file.size <= state.netflix.capabilities.max_upload_bytes &&
    file.name.toLowerCase().endsWith('.csv')
  );
}

function validFilterPair() {
  return Boolean(state.filter.startDate) === Boolean(state.filter.endDate);
}

function dataRequestKey(generationID) {
  return [
    generationID,
    state.filter.startDate,
    state.filter.endDate,
    state.filter.matchStatus
  ].join('|');
}

function normalizeError(error) {
  if (error instanceof APIError) {
    return {
      code: error.code,
      row: error.row,
      generationID: error.generationID
    };
  }
  return {
    code: error?.message || 'unexpected_error',
    row: 0,
    generationID: ''
  };
}

function formatError(error) {
  const base = error.code === 'invalid_csv' ? ui().invalid_csv : ui().error_notice;
  const row = error.row ? ` · ${ui().source_rows} ${error.row}` : '';
  return `${base} · ${error.code}${row}`;
}

function translateDimension(value) {
  if (value === 'unknown') {
    return ui().unknown;
  }
  if (value === 'movie') {
    return ui().movie;
  }
  if (value === 'series') {
    return ui().series;
  }
  if (MATCH_STATUSES.includes(value)) {
    return ui()[value];
  }
  return value;
}

function translateWeekday(value) {
  const index = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'].indexOf(value);
  return index >= 0 ? ui().weekdays[index] : value;
}

function formatMonth(value) {
  const [year, month] = value.split('-').map(Number);
  if (!year || !month) {
    return value;
  }
  return new Intl.DateTimeFormat(state.locale, {
    month: 'short',
    year: '2-digit',
    timeZone: 'UTC'
  }).format(new Date(Date.UTC(year, month - 1, 1)));
}

function formatDate(value) {
  if (!value) {
    return '—';
  }
  const parsed = new Date(`${value}T00:00:00Z`);
  if (Number.isNaN(parsed.valueOf())) {
    return value;
  }
  return new Intl.DateTimeFormat(state.locale, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    timeZone: 'UTC'
  }).format(parsed);
}

function formatDateTime(value) {
  if (!value) {
    return '—';
  }
  const parsed = new Date(value);
  if (Number.isNaN(parsed.valueOf())) {
    return value;
  }
  return new Intl.DateTimeFormat(state.locale, {
    dateStyle: 'medium',
    timeStyle: 'short'
  }).format(parsed);
}

function formatNumber(value) {
  return new Intl.NumberFormat(state.locale).format(value);
}

function formatBytes(value) {
  return new Intl.NumberFormat(state.locale, {
    style: 'unit',
    unit: 'megabyte',
    maximumFractionDigits: 0
  }).format(value / (1024 * 1024));
}

function countFor(counts, label) {
  return counts.find((count) => count.label === label)?.value || 0;
}

function statusCell(status) {
  const label = status ? translateDimension(status) : ui().local_only;
  return element(
    'td',
    {},
    element('span', {class: 'status-dot', 'data-status': status, 'aria-hidden': 'true'}),
    document.createTextNode(label)
  );
}

function providerMark(providerID, title) {
  const labels = {
    netflix: 'N',
    openai: 'AI',
    facebook: 'f',
    instagram: '◎',
    linkedin: 'in',
    tiktok: '♪',
    x: 'X',
    youtube: '▶',
    google: 'G'
  };
  return element('span', {
    class: 'provider-mark',
    title,
    'aria-hidden': 'true',
    text: labels[providerID] || title.slice(0, 1)
  });
}

function stateChip(label, tone) {
  return element('span', {
    class: 'state-chip',
    'data-tone': tone,
    text: label
  });
}

function kpi(label, value, detail = '') {
  const node = element('article', {class: 'kpi'});
  node.append(
    element('span', {class: 'kpi-label', text: label}),
    element('strong', {class: 'kpi-value', text: value})
  );
  if (detail) {
    node.append(element('span', {class: 'kpi-detail', text: detail}));
  }
  return node;
}

function definition(term, description) {
  const wrapper = element('div');
  wrapper.append(
    element('dt', {text: term}),
    element('dd', {text: description})
  );
  return wrapper;
}

function alertNode(tone, message) {
  const alert = element('div', {
    class: 'alert',
    'data-tone': tone,
    role: tone === 'danger' ? 'alert' : 'status'
  });
  alert.append(
    element('span', {
      class: 'alert-icon',
      'aria-hidden': 'true',
      text: tone === 'danger' ? '!' : tone === 'success' ? '✓' : 'i'
    }),
    element('p', {text: message})
  );
  return alert;
}

function actionButton(label, attributes = {}) {
  return element('button', {
    type: 'button',
    class: 'button',
    ...attributes,
    text: label
  });
}

function replaceApp(node) {
  app.replaceChildren(node);
}

function renderFatal() {
  const error = state.actionError || {code: 'startup_failed'};
  app.replaceChildren(
    element(
      'section',
      {class: 'panel', role: 'alert'},
      element('h1', {text: 'Download Your Data'}),
      element('p', {text: `Application startup failed: ${error.code}`})
    )
  );
}

function announce(message) {
  announcer.textContent = '';
  window.requestAnimationFrame(() => {
    announcer.textContent = message;
  });
}

function confirmDialog({title, body, confirmLabel, danger = false}) {
  return new Promise((resolve) => {
    const dialog = element('dialog', {
      'aria-labelledby': 'confirmation-title',
      'aria-describedby': 'confirmation-body'
    });
    const form = element('form', {method: 'dialog'});
    const bodyNode = element('div', {class: 'dialog-body', id: 'confirmation-body'});
    bodyNode.append(element('h2', {id: 'confirmation-title', text: title}));
    body.split('\n\n').forEach((paragraph) => {
      bodyNode.append(element('p', {text: paragraph}));
    });
    const actions = element('div', {class: 'dialog-actions'});
    actions.append(
      element('button', {
        class: 'button',
        type: 'submit',
        value: 'cancel',
        text: ui().cancel
      }),
      element('button', {
        class: `button ${danger ? 'button-danger' : 'button-primary'}`,
        type: 'submit',
        value: 'confirm',
        text: confirmLabel
      })
    );
    form.append(bodyNode, actions);
    dialog.append(form);
    document.body.append(dialog);
    dialog.addEventListener(
      'close',
      () => {
        const confirmed = dialog.returnValue === 'confirm';
        dialog.remove();
        resolve(confirmed);
      },
      {once: true}
    );
    dialog.showModal();
    dialog.querySelector('button[value="cancel"]').focus();
  });
}

function element(tagName, attributes = {}, ...children) {
  const node = document.createElement(tagName);
  for (const [name, value] of Object.entries(attributes)) {
    if (name === 'class') {
      node.className = value;
    } else if (name === 'text') {
      node.textContent = value;
    } else if (['disabled', 'selected', 'checked'].includes(name)) {
      node[name] = Boolean(value);
    } else if (value !== undefined && value !== null) {
      node.setAttribute(name, String(value));
    }
  }
  node.append(...children.flat().filter((child) => child !== undefined && child !== null));
  return node;
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
