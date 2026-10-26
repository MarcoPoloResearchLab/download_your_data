// @ts-check

import {APIError,getOpenAIProvider,initializeAPI,resetAPI,searchOpenAI} from './api.js';
import {element} from './dom.js';
import {instructionLinkURL} from './provider-links.js';
import {clearMediaWorkspace,hydrateMediaWorkspace,renderMediaWorkspace} from './media-workspace.js';
import {MEDIA_COPY} from './media-copy.js';
import {
  GUIDE_ONLY_PROVIDER_IDS,
  isWorkspaceRoute,
  navigate,
  parseRoute,
  WORKSPACE_PROVIDER_IDS,
  VIEWING_HISTORY_ROUTE
} from './routing.js';

const STORAGE_KEYS = Object.freeze({
  language: 'download-your-data.language',
  theme: 'download-your-data.theme'
});

const LOCALES = Object.freeze(['en', 'es', 'fr', 'ru']);
const REQUIRED_UI_KEYS = Object.freeze([
  'provider_catalog',
  'workspace',
  'guide',
  'data_analysis',
  'open',
  'view_guide',
  'private_workspace',
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
  'state_ready_private',
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
  'private_import_privacy',
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
  'privacy',
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
  'auth_checking_title',
  'auth_checking_body',
  'auth_required_title',
  'auth_required_body',
  'workspace_loading_title',
  'workspace_loading_body',
  'workspace_error_title',
  'workspace_error_body',
  'retry_workspace',
  'credits_title',
  'credits_intro',
  'tmdb_credit',
  'official_website',
  'shared_shell_dependency',
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
  'progress',
  'openai_prepare_title',
  'openai_prepare_body',
  'openai_index_body',
  'openai_search_title',
  'openai_search_body',
  'openai_search_label',
  'openai_search_placeholder',
  'openai_search_action',
  'openai_mode',
  'openai_mode_hybrid',
  'openai_mode_semantic',
  'openai_mode_lexical',
  'openai_include_archived',
  'openai_search_results',
  'openai_no_results',
  'openai_search_hint',
  'openai_conversations',
  'openai_messages',
  'openai_indexed_documents',
  'openai_index_model',
  'openai_query_privacy',
  'openai_search_completed'
]);

const state = {
  data: null,
  capabilities: null,
  locale: 'en',
  theme: 'dark',
  route: {name: 'catalog'},
  sharedAuthStatus: 'pending',
  workspaceStatus: 'idle',
  workspaceProvider: '',
  workspaceError: null,
  openai: null,
  openAIQuery: '',
  openAIMode: 'hybrid',
  openAIIncludeArchived: true,
  openAIResults: [],
  openAISearchBusy: false,
  openAISearchError: null,
  openAISearchController: null,
  actionBusy: false,
  actionError: null,
  workspaceController: null
};

const app = document.querySelector('#app');
const announcer = document.querySelector('#app-announcer');

boot().catch((error) => {
  state.actionError = normalizeError(error);
  renderFatal();
});

async function boot() {
  attachGlobalHandlers();
  initializeTheme();
  state.route = parseRoute();
  const initialController = new AbortController();
  const data = await fetchJSON('content/application.json', initialController.signal);
  state.data = validateAppData(data);
  state.locale = initialLocale();
  document.documentElement.lang = state.locale;
  render();
  if (state.sharedAuthStatus === 'authenticated') {
    await openAuthenticatedSurface();
  }
}

function attachGlobalHandlers() {
  const lifecycleBuffer = Reflect.get(
    window,
    'DownloadYourDataAuthLifecycle'
  );
  if (!lifecycleBuffer || typeof lifecycleBuffer.take !== 'function') {
    throw new Error('authentication lifecycle buffer is unavailable');
  }
  const initialStatuses = lifecycleBuffer.take();
  document.addEventListener(
    'mpr-ui:auth:authenticated',
    handleSharedAuthenticated
  );
  document.addEventListener(
    'mpr-ui:auth:unauthenticated',
    handleSharedUnauthenticated
  );
  initialStatuses.forEach((status) => {
    if (status === 'authenticated') {
      handleSharedAuthenticated();
    } else if (status === 'unauthenticated') {
      handleSharedUnauthenticated();
    } else {
      throw new Error('authentication lifecycle buffer contains an invalid status');
    }
  });
  window.addEventListener('hashchange', () => {
    state.route = parseRoute();
    clearProtectedWorkspace();
    render();
    if (state.sharedAuthStatus === 'authenticated') {
      void openAuthenticatedSurface().catch(renderLifecycleFailure);
    }
  });
  window.addEventListener('beforeunload', cleanupAll, {once: true});
  document.addEventListener('click', handleClick);
  document.addEventListener('submit', handleSubmit);
  document.addEventListener('keydown', handleKeyDown);
}

function handleSharedAuthenticated() {
  state.sharedAuthStatus = 'authenticated';
  void openAuthenticatedSurface().catch(renderLifecycleFailure);
}

function handleSharedUnauthenticated() {
  state.sharedAuthStatus = 'unauthenticated';
  clearProtectedWorkspace();
  if (state.data) {
    render();
  }
}

async function openAuthenticatedSurface() {
  if (!state.data || state.sharedAuthStatus !== 'authenticated') {
    return;
  }
  if (isWorkspaceRoute(state.route)) {
    await hydrateWorkspace(state.route.name);
    return;
  }
  render();
  await completeAuthTransition();
}

async function hydrateWorkspace(providerID) {
  if (
    state.sharedAuthStatus !== 'authenticated' ||
    !isWorkspaceRoute({name:providerID})
  ) {
    return;
  }
  if (
    state.workspaceStatus === 'loading' &&
    state.workspaceProvider === providerID
  ) {
    return;
  }

  clearProtectedWorkspace();
  const controller = new AbortController();
  state.workspaceController = controller;
  state.workspaceProvider = providerID;
  state.workspaceStatus = 'loading';
  render();
  try {
    state.capabilities = await initializeAPI(controller.signal);
    if (providerID === 'openai') {
      state.openai = await getOpenAIProvider(controller.signal);
    } else {
      await hydrateMediaWorkspace(controller.signal,state.locale);
    }
    if (
      controller.signal.aborted ||
      state.sharedAuthStatus !== 'authenticated' ||
      state.route.name !== providerID
    ) {
      return;
    }
    state.workspaceStatus = 'ready';
    render();
  } catch (error) {
    if (error.name === 'AbortError') {
      return;
    }
    state.workspaceStatus = 'error';
    state.workspaceError = normalizeError(error);
    render();
  } finally {
    if (state.workspaceController === controller) {
      state.workspaceController = null;
    }
  }
  await completeAuthTransition();
}

async function completeAuthTransition() {
  const mprUI = Reflect.get(window, 'MPRUI');
  if (!mprUI || typeof mprUI.whenAutoOrchestrationReady !== 'function') {
    throw new Error('mpr-ui orchestration contract is unavailable');
  }
  await mprUI.whenAutoOrchestrationReady();
  document.dispatchEvent(new CustomEvent('download-your-data:app-ready'));
}

function renderLifecycleFailure(error) {
  state.actionError = normalizeError(error);
  renderFatal();
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

  const actionButton = event.target.closest('[data-action]');
  if (!actionButton || state.actionBusy) {
    return;
  }
  const action = actionButton.dataset.action;
  if (
    action === 'retry-workspace' &&
    state.sharedAuthStatus === 'authenticated' &&
    isWorkspaceRoute(state.route)
  ) {
    state.workspaceStatus = 'idle';
    void hydrateWorkspace(state.route.name).catch(renderLifecycleFailure);
  }
}

function handleSubmit(event) {
  if (!event.target.matches('#openai-search-form')) {
    return;
  }
  event.preventDefault();
  const form = new FormData(event.target);
  void beginOpenAISearch({
    query: String(form.get('query') || ''),
    mode: String(form.get('mode') || ''),
    includeArchived: form.get('include_archived') === 'on'
  });
}

function render() {
  if (!state.data) {
    return;
  }
  updateChrome();
  if (isWorkspaceRoute(state.route) && state.workspaceStatus !== 'ready') {
    renderWorkspaceGate(state.route.name);

  } else if (state.route.name === 'openai') {
    renderOpenAIWorkspace();
  } else if (state.route.name === VIEWING_HISTORY_ROUTE) {
    setHeaderContext(MEDIA_COPY[state.locale].heading);
    replaceApp(renderMediaWorkspace(ui(),state.locale,render));
  } else if (state.route.name === 'guide') {
    renderGuide(state.route.provider);
  } else if (state.route.name === 'credits') {
    renderCredits();
  } else {
    renderCatalog();
  }
}

function renderWorkspaceGate(providerID) {
  const provider = providerID === VIEWING_HISTORY_ROUTE ? {title:MEDIA_COPY[state.locale].heading} : localizedProvider(providerID);
  setHeaderContext(provider.title);
  const root = element('div', {class: 'workspace-gate'});
  const heading = element('div', {class: 'page-heading'});
  const copy = element('div');
  copy.append(
    element('a', {
      class: 'back-link',
      href: '#catalog',
      text: `← ${ui().back_catalog}`
    }),
    element('p', {class: 'page-kicker', text: ui().private_workspace}),
    element('h1', {text: provider.title})
  );
  heading.append(copy);

  let title = ui().workspace_loading_title;
  let body = ui().workspace_loading_body;
  let tone = 'info';
  if (state.sharedAuthStatus === 'pending') {
    title = ui().auth_checking_title;
    body = ui().auth_checking_body;
  } else if (state.sharedAuthStatus === 'unauthenticated') {
    title = ui().auth_required_title;
    body = ui().auth_required_body;
    tone = 'warning';
  } else if (state.workspaceStatus === 'error') {
    title = ui().workspace_error_title;
    body = `${ui().workspace_error_body} · ${state.workspaceError?.code || 'integration_error'}`;
    tone = 'danger';
  }

  const panel = element('section', {
    class: 'panel panel-raised workspace-gate-panel',
    'data-auth-state': state.sharedAuthStatus,
    'data-workspace-state': state.workspaceStatus,
    'data-tone': tone
  });
  panel.append(
    element('p', {class: 'eyebrow', text: ui().private_workspace}),
    element('h2', {text: title}),
    element('p', {class: 'panel-copy', text: body})
  );
  if (
    state.sharedAuthStatus === 'authenticated' &&
    state.workspaceStatus === 'error'
  ) {
    panel.append(
      actionButton(ui().retry_workspace, {
        'data-action': 'retry-workspace',
        class: 'button button-primary'
      })
    );
  }
  root.append(heading, panel);
  replaceApp(root);
}

function renderCatalog() {
  setHeaderContext(ui().provider_catalog);
  const root = element('div', {class: 'catalog'});
  const heading = element('div', {class: 'page-heading'});
  const copy = element('div');
  copy.append(element('h1', {text: ui().private_workspace}));
  heading.append(copy);
  heading.append(element('a',{class:'button button-primary',href:`#app/${VIEWING_HISTORY_ROUTE}`,text:MEDIA_COPY[state.locale].heading}));

  const grid = element('section', {
    class: 'catalog-grid',
    'aria-label': ui().provider_catalog
  });
  for (const providerDefinition of state.data.provider_registry) {
    const localized = localizedProvider(providerDefinition.id);
    const card = element('article', {
      class: 'provider-card',
      'data-provider-id': providerDefinition.id
    });
    const providerLink = element('a', {
      class: 'provider-card-guide',
      href: providerDefinition.catalog_path,
      'aria-label': localized.nav,
    });
    const cardCopy = element('div', {class: 'provider-card-copy'});
    const providerName = element('h2', {
      class: 'provider-name',
      text: localized.nav
    });
    if (providerDefinition.surface === 'workspace') {
      cardCopy.append(
        element(
          'div',
          {class: 'provider-card-meta'},
          providerName,
          actionButton(ui().data_analysis, {
            'data-route': providerDefinition.id==='netflix'?VIEWING_HISTORY_ROUTE:providerDefinition.id,
            class: 'button button-primary provider-analysis-action'
          })
        )
      );
    } else if (providerDefinition.id === 'amazon') {
      cardCopy.append(element('div',{class:'provider-card-meta'},providerName,actionButton(ui().data_analysis,{'data-route':VIEWING_HISTORY_ROUTE,class:'button button-primary provider-analysis-action'})));
    } else {
      cardCopy.append(providerName);
    }
    cardCopy.append(
      element('p', {class: 'provider-summary', text: localized.intro})
    );
    card.append(
      providerLink,
      providerMark(providerDefinition.id, localized.nav, providerDefinition.icon_src),
      cardCopy
    );
    grid.append(card);
  }
  root.append(heading, grid);
  replaceApp(root);
}

function renderGuide(providerID) {
  const provider = localizedProvider(providerID);
  if (!provider) {
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
  heading.append(copy);
  if (WORKSPACE_PROVIDER_IDS.includes(providerID)) {
    const actions = element('div', {class: 'page-heading-actions'});
    actions.append(
      actionButton(ui().open, {
        'data-route': providerID==='netflix'?VIEWING_HISTORY_ROUTE:providerID,
        class: 'button button-primary'
      })
    );
    heading.append(actions);
  }

  const section = element('section', {
    id: provider.id,
    class: 'panel panel-raised guide-card'
  });
  section.append(
    element('h2', {text: ui().guide_steps_title}),
    renderInstructionSteps(provider)
  );

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

  if (provider.note) {
    section.append(element('p', {class: 'panel-copy', text: provider.note}));
  }
  root.append(heading, section);
  replaceApp(root);
}

function renderInstructionSteps(provider) {
  const assets = new Map(
    state.data.instruction_screenshots[provider.id].map((asset) => [asset.id, asset])
  );
  const list = element('ol', {class: 'instruction-steps'});
  provider.steps.forEach((step, index) => {
    const asset = assets.get(step.screenshot_id);
    const target = instructionLinkURL(provider.id, asset.href);
    const visual = element(
      'figure',
      {class: 'instruction-visual'},
      element('img', {
        class: 'instruction-screenshot',
        src: asset.src,
        alt: step.alt,
        loading: 'lazy',
        decoding: 'async',
        'data-screenshot-id': asset.id
      })
    );
    list.append(
      element(
        'li',
        {
          class: 'instruction-step',
          'data-step-index': String(index + 1)
        },
        element('span', {
          class: 'instruction-step-number',
          text: String(index + 1),
          'aria-hidden': 'true'
        }),
        element(
          'div',
          {class: 'instruction-step-content'},
          element('p', {class: 'instruction-step-copy', text: step.text}),
          element('a', {
            class: 'instruction-step-link',
            href: target.href,
            target: '_blank',
            rel: 'noopener noreferrer',
            text: `${ui().open} ${target.hostname.replace(/^www\./, '')} ↗`
          })
        ),
        visual
      )
    );
  });
  return list;
}

function renderOpenAIWorkspace() {
  const provider = localizedProvider('openai');
  const definition = providerDefinition('openai');
  setHeaderContext(provider.title);
  const root = element('div', {class: 'workspace openai-workspace'});
  const presentation = openAIStatePresentation();
  const heading = element('div', {class: 'workspace-header'});
  const title = element('div', {class: 'workspace-title'});
  title.append(
    element('a', {
      class: 'back-link',
      href: '#catalog',
      'aria-label': ui().back_catalog,
      text: '←'
    }),
    providerMark('openai', provider.title, definition.icon_src),
    element(
      'div',
      {},
      element('p', {class: 'eyebrow', text: ui().workspace}),
      element('h1', {text: provider.title})
    )
  );
  const actions = element('div', {class: 'workspace-header-actions'});
  actions.append(
    actionButton(ui().view_guide, {
      'data-route': 'guide',
      'data-provider': 'openai'
    }),
    stateChip(presentation.label, presentation.tone)
  );
  heading.append(title, actions);

  const grid = element('div', {class: 'workspace-grid'});
  const main = element('div', {class: 'workspace-main'});
  const rail = element('aside', {
    class: 'workspace-rail',
    'aria-label': ui().details
  });
  if (state.openai.state === 'ready') {
    main.append(renderOpenAISearchPanel());
  } else {
    main.append(renderOpenAIPreparationPanel());
  }
  rail.append(...renderOpenAIRail());
  grid.append(main, rail);
  root.append(heading, grid);
  replaceApp(root);
}

function renderOpenAIPreparationPanel() {
  const indexRequired = state.openai.state === 'index_required';
  const panel = element('section', {class: 'panel panel-raised openai-prepare'});
  panel.append(
    element('p', {class: 'eyebrow', text: ui().private_workspace}),
    element('h2', {text: ui().openai_prepare_title}),
    element('p', {
      class: 'panel-copy',
      text: indexRequired ? ui().openai_index_body : ui().openai_prepare_body
    })
  );
  return panel;
}

function renderOpenAISearchPanel() {
  const panel = element('section', {class: 'panel panel-raised openai-search-panel'});
  const header = element('div', {class: 'panel-header'});
  header.append(
    element(
      'div',
      {},
      element('h2', {text: ui().openai_search_title}),
      element('p', {class: 'panel-copy', text: ui().openai_search_body})
    )
  );
  const form = element('form', {
    id: 'openai-search-form',
    class: 'openai-search-form'
  });
  const queryField = element('label', {class: 'field openai-query-field'});
  queryField.append(
    element('span', {text: ui().openai_search_label}),
    element('input', {
      type: 'search',
      name: 'query',
      value: state.openAIQuery,
      placeholder: ui().openai_search_placeholder,
      maxlength: String(state.openai.capabilities.max_query_bytes),
      autocomplete: 'off',
      required: true,
      disabled: state.openAISearchBusy
    })
  );
  const modeField = element('label', {class: 'field openai-mode-field'});
  const mode = element('select', {
    name: 'mode',
    disabled: state.openAISearchBusy
  });
  for (const [value, label] of [
    ['hybrid', ui().openai_mode_hybrid],
    ['semantic', ui().openai_mode_semantic],
    ['lexical', ui().openai_mode_lexical]
  ]) {
    mode.append(
      element('option', {
        value,
        selected: state.openAIMode === value,
        text: label
      })
    );
  }
  modeField.append(element('span', {text: ui().openai_mode}), mode);
  const archived = element(
    'label',
    {class: 'openai-archived-field'},
    element('input', {
      type: 'checkbox',
      name: 'include_archived',
      checked: state.openAIIncludeArchived,
      disabled: state.openAISearchBusy
    }),
    document.createTextNode(ui().openai_include_archived)
  );
  form.append(
    queryField,
    modeField,
    archived,
    element('button', {
      class: 'button button-primary',
      type: 'submit',
      disabled: state.openAISearchBusy,
      text: state.openAISearchBusy ? ui().loading : ui().openai_search_action
    })
  );
  panel.append(
    header,
    form,
    element('p', {class: 'privacy-note', text: ui().openai_query_privacy}),
    renderOpenAISearchResults()
  );
  return panel;
}

function renderOpenAISearchResults() {
  const region = element('div', {
    class: 'openai-results',
    'aria-live': 'polite'
  });
  if (state.openAISearchError) {
    region.append(alertNode('danger', formatError(state.openAISearchError)));
    return region;
  }
  if (state.openAISearchBusy) {
    region.append(element('p', {class: 'empty-copy', text: ui().loading}));
    return region;
  }
  if (!state.openAIQuery) {
    region.append(element('p', {class: 'empty-copy', text: ui().openai_search_hint}));
    return region;
  }
  region.append(element('h3', {text: ui().openai_search_results}));
  if (!state.openAIResults.length) {
    region.append(element('p', {class: 'empty-copy', text: ui().openai_no_results}));
    return region;
  }
  const list = element('ol', {class: 'openai-result-list'});
  state.openAIResults.forEach((result) => {
    const item = element('li', {class: 'openai-result'});
    const resultHeader = element('div', {class: 'openai-result-header'});
    resultHeader.append(
      element('h4', {text: result.conversation_title || ui().unknown}),
      element('span', {
        class: 'openai-result-score',
        text: `${ui().score}: ${result.score.toFixed(4)}`
      })
    );
    const excerpts = element('div', {class: 'openai-excerpts'});
    result.excerpts.forEach((excerpt) => {
      excerpts.append(
        element(
          'blockquote',
          {class: 'openai-excerpt'},
          element('span', {class: 'openai-excerpt-role', text: excerpt.role}),
          element('p', {text: excerpt.text})
        )
      );
    });
    item.append(resultHeader, excerpts);
    list.append(item);
  });
  region.append(list);
  return region;
}

function renderOpenAIRail() {
  const statistics = state.openai.statistics;
  const summary = state.openai.search_index;
  const archive = element('section', {class: 'panel rail-section'});
  archive.append(
    element('h2', {text: ui().details}),
    element(
      'dl',
      {class: 'rail-list'},
      definition(ui().openai_conversations, formatNumber(statistics.conversations)),
      definition(ui().openai_messages, formatNumber(statistics.messages)),
      definition(
        ui().openai_indexed_documents,
        formatNumber(summary?.document_count || 0)
      )
    )
  );
  const inference = element('section', {class: 'panel rail-section'});
  inference.append(
    element('h2', {text: ui().analysis}),
    element(
      'dl',
      {class: 'rail-list'},
      definition(ui().openai_index_model, summary?.model || '—'),
      definition(
        ui().type,
        String(state.openai.capabilities.inference_boundary)
      )
    )
  );
  return [archive, inference];
}

function renderCredits() {
  setHeaderContext(ui().credits);
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
  const dependency = element('section', {class: 'panel'});
  dependency.append(
    element('h2', {text: ui().private_workspace}),
    element('p', {class: 'panel-copy', text: ui().shared_shell_dependency})
  );
  root.append(heading, dependency);
  replaceApp(root);
}

async function beginOpenAISearch(searchRequest) {
  const query = searchRequest.query.trim();
  state.openAIQuery = query;
  state.openAIMode = searchRequest.mode;
  state.openAIIncludeArchived = searchRequest.includeArchived;
  state.openAIResults = [];
  state.openAISearchError = null;
  const queryBytes = new TextEncoder().encode(query).byteLength;
  if (
    !query ||
    queryBytes > state.openai.capabilities.max_query_bytes ||
    !state.openai.capabilities.search_modes.includes(searchRequest.mode)
  ) {
    state.openAISearchError = {code: 'invalid_openai_query', row: 0};
    render();
    announce(formatError(state.openAISearchError));
    return;
  }

  cleanupOpenAISearch();
  const controller = new AbortController();
  state.openAISearchController = controller;
  state.openAISearchBusy = true;
  render();
  try {
    const response = await searchOpenAI(
      {
        query,
        mode: searchRequest.mode,
        includeArchived: searchRequest.includeArchived,
        limit: Math.min(20, state.openai.capabilities.max_results),
        excerpts: Math.min(3, state.openai.capabilities.max_excerpts)
      },
      controller.signal
    );
    state.openAIResults = response.results;
    announce(ui().openai_search_completed);
  } catch (error) {
    if (error.name !== 'AbortError') {
      state.openAISearchError = normalizeError(error);
      announce(formatError(state.openAISearchError));
    }
  } finally {
    if (state.openAISearchController === controller) {
      state.openAISearchController = null;
    }
    state.openAISearchBusy = false;
    if (state.route.name === 'openai') {
      render();
    }
  }
}

function resetOpenAISearchData() {
  cleanupOpenAISearch();
  state.openAIQuery = '';
  state.openAIMode = 'hybrid';
  state.openAIIncludeArchived = true;
  state.openAIResults = [];
  state.openAISearchBusy = false;
  state.openAISearchError = null;
}

function cleanupAll() {
  cleanupOpenAISearch();
  cleanupWorkspaceRequest();
}

function cleanupOpenAISearch() {
  state.openAISearchController?.abort();
  state.openAISearchController = null;
}

function cleanupWorkspaceRequest() {
  state.workspaceController?.abort();
  state.workspaceController = null;
}

function clearProtectedWorkspace() {
  clearMediaWorkspace();
  cleanupOpenAISearch();
  cleanupWorkspaceRequest();
  resetOpenAISearchData();
  resetAPI();
  state.capabilities = null;
  state.openai = null;
  state.workspaceStatus = 'idle';
  state.workspaceProvider = '';
  state.workspaceError = null;
  state.actionBusy = false;
  state.actionError = null;
}

function openAIStatePresentation() {
  if (state.openai?.state === 'ready') {
    return {label: ui().state_ready_private, tone: 'success'};
  }
  if (state.openai?.state === 'index_required') {
    return {label: ui().state_action_needed, tone: 'warning'};
  }
  return {label: ui().state_empty, tone: 'neutral'};
}

function updateChrome() {
  const strings = localized();
  document.title = strings.site_title;
  document.documentElement.lang = state.locale;
  const brand = document.querySelector('#brand');
  brand.setAttribute('aria-label', strings.site_title);
  document.querySelector('#brand-label').textContent = strings.site_title;
  document.querySelector('#credits-button').textContent = ui().credits;
  document.querySelector('#footer-local').textContent = ui().private_workspace;
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
  render();
}

function initializeTheme() {
  const saved = localStorage.getItem(STORAGE_KEYS.theme);
  setTheme(saved === 'light' ? 'light' : 'dark');
}

function setTheme(theme) {
  state.theme = theme;
  document.documentElement.dataset.theme = theme;
  document.documentElement.dataset.mprTheme = theme;
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

function providerDefinition(providerID) {
  return state.data.provider_registry.find((provider) => provider.id === providerID);
}

function validateAppData(data) {
  assertObject(data, 'content/application.json');
  assertObject(data.title_links, 'title_links');
  assertString(data.title_links.imdb, 'title_links.imdb');
  const imdbTitleURL = new URL(data.title_links.imdb);
  if (imdbTitleURL.protocol !== 'https:' || imdbTitleURL.hostname !== 'www.imdb.com' ||
      imdbTitleURL.pathname !== '/title/' || imdbTitleURL.port || imdbTitleURL.search ||
      imdbTitleURL.hash || imdbTitleURL.username || imdbTitleURL.password) {
    throw new Error('title_links.imdb must use the official IMDb title URL');
  }
  assertObject(data.credits, 'credits');
  assertObject(data.credits.tmdb, 'credits.tmdb');
  assertString(data.credits.tmdb.notice, 'credits.tmdb.notice');
  assertString(data.credits.tmdb.website, 'credits.tmdb.website');
  const tmdbWebsite = new URL(data.credits.tmdb.website);
  if (
    tmdbWebsite.protocol !== 'https:' ||
    tmdbWebsite.hostname !== 'www.themoviedb.org' ||
    tmdbWebsite.username ||
    tmdbWebsite.password
  ) {
    throw new Error('credits.tmdb.website must use the official TMDB HTTPS host');
  }
  assertArray(data.provider_registry, 'provider_registry');
  assertObject(data.instruction_screenshots, 'instruction_screenshots');
  assertObject(data.strings, 'strings');
  const providerIDs = data.provider_registry.map((provider) => {
    assertObject(provider, 'provider_registry[]');
    assertString(provider.id, 'provider_registry[].id');
    assertString(provider.icon_src, `provider ${provider.id} icon_src`);
    if (provider.icon_src !== `images/providers/${provider.id}.png`) {
      throw new Error(`provider ${provider.id} icon_src must use its canonical local asset`);
    }
    if (
      provider.surface !== 'workspace' &&
      provider.surface !== 'guide' &&
      provider.surface !== 'tool'
    ) {
      throw new Error(`provider ${provider.id} has invalid surface`);
    }
    assertString(provider.catalog_path, `provider ${provider.id} catalog_path`);
    if (![
      ...(WORKSPACE_PROVIDER_IDS.includes(provider.id) || GUIDE_ONLY_PROVIDER_IDS.includes(provider.id) ? [`#guide/${provider.id}`] : []),
      '/tools/google-authenticator/',
      '/tools/password-merger/',
    ].includes(provider.catalog_path)) {
      throw new Error(`provider ${provider.id} catalog_path must use its guide or a canonical local tool route`);
    }
    return provider.id;
  });
  const definitionsByID = new Map(
    data.provider_registry.map((provider) => [provider.id, provider])
  );
  if (new Set(providerIDs).size !== providerIDs.length || providerIDs[0] !== 'netflix') {
    throw new Error('provider_registry must start with one unique netflix provider');
  }
  WORKSPACE_PROVIDER_IDS.forEach((providerID) => {
    const definition = data.provider_registry.find((provider) => provider.id === providerID);
    if (!definition || definition.surface !== 'workspace') {
      throw new Error(`${providerID} must be workspace-capable`);
    }
  });
  GUIDE_ONLY_PROVIDER_IDS.forEach((providerID) => {
    const definition = data.provider_registry.find((provider) => provider.id === providerID);
    if (!definition || definition.surface !== 'guide') {
      throw new Error(`${providerID} must be guide-only`);
    }
  });
  const instructionProviderIDs = data.provider_registry
    .filter((provider) => provider.surface !== 'tool')
    .map((provider) => provider.id);
  if (Object.keys(data.instruction_screenshots).length !== instructionProviderIDs.length) {
    throw new Error('instruction_screenshots must cover every provider');
  }
  const screenshotIDsByProvider = new Map();
  instructionProviderIDs.forEach((providerID) => {
    const assets = data.instruction_screenshots[providerID];
    assertArray(assets, `${providerID} screenshots`);
    if (!assets.length) {
      throw new Error(`${providerID} must have at least one screenshot`);
    }
    const screenshotIDs = new Set();
    assets.forEach((asset) => {
      assertObject(asset, `${providerID} screenshots[]`);
      assertString(asset.id, `${providerID} screenshot id`);
      assertString(asset.src, `${providerID} screenshot src`);
      instructionLinkURL(providerID, asset.href);
      if (screenshotIDs.has(asset.id)) {
        throw new Error(`${providerID} has duplicate screenshot ${asset.id}`);
      }
      screenshotIDs.add(asset.id);
    });
    screenshotIDsByProvider.set(providerID, screenshotIDs);
  });

  LOCALES.forEach((locale) => {
    const mediaCopy = MEDIA_COPY[locale];
    assertObject(mediaCopy, `media copy.${locale}`);
    const mediaKeys = Object.keys(MEDIA_COPY.en);
    if (Object.keys(mediaCopy).length!==mediaKeys.length) throw new Error('media copy keys differ');
    for (const key of mediaKeys) {
      assertString(mediaCopy[key], `media copy.${locale}.${key}`);
      if (!mediaCopy[key].trim()) throw new Error('media copy is empty');
    }
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
      for (const key of ['id', 'nav', 'title', 'intro']) {
        assertString(provider[key], `strings.${locale}.${provider.id}.${key}`);
      }
      assertArray(provider.steps, `${provider.id}.steps`);
      assertArray(provider.refs, `${provider.id}.refs`);
      if (definitionsByID.get(provider.id)?.surface !== 'tool' && !provider.steps.length) {
        throw new Error(`${locale} ${provider.id} must have at least one instruction step`);
      }
      if (Object.hasOwn(provider, 'images')) {
        throw new Error(`${locale} ${provider.id} uses the obsolete provider image gallery`);
      }
      if (
        Object.hasOwn(provider, 'state') ||
        Object.hasOwn(provider, 'status') ||
        Object.hasOwn(provider, 'generation')
      ) {
        throw new Error(`localized provider ${provider.id} contains backend workflow state`);
      }
      if (definitionsByID.get(provider.id)?.surface === 'tool') {
        if (provider.steps.length || provider.refs.length) {
          throw new Error(`${locale} ${provider.id} tool providers cannot contain guide steps or references`);
        }
        return;
      }
      const availableScreenshotIDs = screenshotIDsByProvider.get(provider.id);
      const usedScreenshotIDs = new Set();
      provider.steps.forEach((step, index) => {
        assertObject(step, `${locale} ${provider.id} step ${index + 1}`);
        for (const key of ['text', 'screenshot_id', 'alt']) {
          assertString(step[key], `${locale} ${provider.id} step ${index + 1} ${key}`);
          if (!step[key].trim()) {
            throw new Error(`${locale} ${provider.id} step ${index + 1} ${key} is empty`);
          }
        }
        if (!availableScreenshotIDs.has(step.screenshot_id)) {
          throw new Error(
            `${locale} ${provider.id} step ${index + 1} references unknown screenshot ${step.screenshot_id}`
          );
        }
        usedScreenshotIDs.add(step.screenshot_id);
      });
      if (usedScreenshotIDs.size !== availableScreenshotIDs.size) {
        throw new Error(`${locale} ${provider.id} has an unused screenshot`);
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

function providerMark(providerID, title, iconSource) {
  return element(
    'span',
    {
      class: 'provider-mark',
      title,
      'aria-hidden': 'true'
    },
    element('img', {
      class: 'provider-icon',
      src: iconSource,
      alt: '',
      width: '24',
      height: '24',
      decoding: 'async',
      'data-provider-icon': providerID
    })
  );
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
