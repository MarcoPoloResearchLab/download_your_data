async page => {
  const baseURL = '__BASE_URL__';
  const validCSV = '__VALID_CSV__';
  const invalidCSV = '__INVALID_CSV__';
  const sharedShellURLs = new Set([
    'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui.css',
    'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui.js'
  ]);
  const browserErrors = [];
  const requestURLs = [];
  page.on('console', (message) => {
    if (message.type() === 'error' || message.type() === 'warning') {
      browserErrors.push(`${message.type()}: ${message.text()}`);
    }
  });
  page.on('pageerror', (error) => browserErrors.push(`pageerror: ${error.message}`));
  page.on('request', (request) => requestURLs.push(request.url()));

  const assert = (condition, message) => {
    if (!condition) {
      throw new Error(message);
    }
  };
  const route = async (hash, readySelector) => {
    await page.evaluate((nextHash) => {
      window.location.hash = nextHash;
    }, hash);
    await page.locator(readySelector).waitFor();
  };
  const selectLanguage = async (locale) => {
    await page.locator(`[data-language="${locale}"]`).click();
    await page.waitForFunction(
      (expectedLocale) => document.documentElement.lang === expectedLocale,
      locale
    );
  };
  const snapshot = async () =>
    page.evaluate(async () => {
      const response = await fetch('/api/providers/netflix', {cache: 'no-store'});
      if (!response.ok) {
        throw new Error(`snapshot HTTP ${response.status}`);
      }
      return response.json();
    });
  const kpiValue = async (label) => {
    const card = page.locator('.kpi').filter({has: page.locator('.kpi-label', {hasText: label})});
    await card.first().waitFor();
    return (await card.first().locator('.kpi-value').textContent()).trim();
  };
  const waitForState = async (predicate, label) => {
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      if (predicate(await snapshot())) {
        return;
      }
      await page.waitForTimeout(50);
    }
    throw new Error(`timed out waiting for ${label}`);
  };

  await page.setViewportSize({width: 1440, height: 1000});
  await page.goto(baseURL, {waitUntil: 'networkidle'});
  await page.waitForFunction(
    () => customElements.get('mpr-header') && customElements.get('mpr-footer')
  );
  await page.locator('mpr-header header[role="banner"]').waitFor();
  await page.locator('mpr-footer footer[role="contentinfo"]').waitFor();
  assert(
    await page.locator('mpr-header header[role="banner"]').count() === 1 &&
      await page.locator('mpr-footer footer[role="contentinfo"]').count() === 1,
    'mpr-ui must own exactly one rendered header and footer'
  );
  assert(
    await page.locator('.app-bar, footer.app-footer').count() === 0,
    'the retired app-owned header or footer is still rendered'
  );
  assert(
    await page.locator('#brand[slot="brand"]').isVisible() &&
      await page.locator('#header-context[slot="nav-left"]').isVisible() &&
      await page.locator('#language-switcher').isVisible() &&
      await page.locator('#theme-toggle').isVisible(),
    'mpr-header did not preserve the application controls in supported slots'
  );
  assert(
    await page.locator('mpr-header [data-mpr-header="google-signin"]:visible').count() === 0,
    'the local-only shell must not fabricate an authentication control'
  );
  assert(
    await page.locator('mpr-footer a[href="https://mprlab.com/"]').count() === 1 &&
      await page.locator(
        'mpr-footer a[href="https://github.com/MarcoPoloResearchLab/download_your_data"]'
      ).count() === 1 &&
      await page.locator('#footer-local[slot="legal"], [slot="legal"] #footer-local').count() === 1,
    'mpr-footer is missing its shared links or local-data disclosure'
  );
  await page.locator('#theme-toggle').click();
  await page.waitForFunction(
    () =>
      document.documentElement.dataset.theme === 'light' &&
      document.documentElement.dataset.mprTheme === 'light'
  );
  await page.locator('#theme-toggle').click();
  await page.waitForFunction(
    () =>
      document.documentElement.dataset.theme === 'dark' &&
      document.documentElement.dataset.mprTheme === 'dark'
  );
  await page.locator('[data-provider-id="netflix"]').waitFor();
  assert(
    await page.locator('.provider-card[data-provider-id]').count() === 11,
    'provider catalog must contain eleven canonical providers'
  );
  const wideCatalogLayout = await page.locator('.catalog-grid').evaluate((grid) => ({
    columns: getComputedStyle(grid).gridTemplateColumns.split(' ').filter(Boolean).length,
    width: grid.getBoundingClientRect().width
  }));
  assert(
    wideCatalogLayout.columns === 3 && wideCatalogLayout.width <= 960,
    'wide provider catalog must use a compact three-column tile grid'
  );
  const providerIconPaths = [];
  for (const card of await page.locator('.provider-card[data-provider-id]').all()) {
    const providerID = await card.getAttribute('data-provider-id');
    const mark = card.locator('.provider-mark');
    const icon = mark.locator('img.provider-icon');
    assert(await icon.count() === 1, `${providerID} must render exactly one provider icon`);
    await icon.evaluate((element) => element.decode());
    const iconRecord = await icon.evaluate((element) => ({
      alt: element.alt,
      cardWidth: element.closest('.provider-card').getBoundingClientRect().width,
      complete: element.complete,
      id: element.dataset.providerIcon,
      markHeight: element.parentElement.getBoundingClientRect().height,
      markText: element.parentElement.textContent.trim(),
      markWidth: element.parentElement.getBoundingClientRect().width,
      naturalHeight: element.naturalHeight,
      naturalWidth: element.naturalWidth,
      path: new URL(element.src).pathname,
      width: element.getBoundingClientRect().width
    }));
    assert(
      iconRecord.complete &&
        iconRecord.id === providerID &&
        iconRecord.alt === '' &&
        iconRecord.markText === '' &&
        iconRecord.naturalWidth >= 32 &&
        iconRecord.naturalHeight >= 32 &&
        iconRecord.path === `/images/providers/${providerID}.png` &&
        iconRecord.cardWidth > 0 &&
        iconRecord.markWidth >= 78 &&
        iconRecord.markHeight >= 78 &&
        iconRecord.width >= 48 &&
        iconRecord.width <= 64,
      `${providerID} provider card does not render its reviewed product logo at the required size`
    );
    assert(
      await card.locator('.provider-name').count() === 1 &&
        await card.locator('.provider-summary').count() === 1 &&
        await card.locator('.provider-actions').count() === 1,
      `${providerID} provider card is missing its name, summary, or action area`
    );
    providerIconPaths.push(iconRecord.path);
  }
  assert(
    new Set(providerIconPaths).size === 11,
    'every provider must own one distinct local product icon'
  );
  assert(
    await page.locator('.provider-card:not([data-provider-id="netflix"])').count() === 10 &&
      await page.locator(
        '.provider-card:not([data-provider-id="netflix"]) .provider-actions button'
      ).count() === 10 &&
      await page.locator(
        '.provider-card:not([data-provider-id="netflix"]) .provider-actions [data-route="guide"]'
      ).count() === 10 &&
      await page.locator(
        '.provider-card:not([data-provider-id="netflix"]) .state-chip'
      ).count() === 0 &&
      await page.locator(
        '.provider-card[data-provider-id="netflix"] .provider-card-meta .state-chip'
      ).count() === 1,
    'each guide-only catalog entry must expose one View guide action without a duplicate badge'
  );
  assert(
    await page.locator('[data-provider-id="netflix"]').count() === 1,
    'provider catalog must contain exactly one Netflix identity'
  );
  assert(
    await page.locator(
      '[data-provider-id="netflix"] [data-route="guide"][data-provider="netflix"]'
    ).count() === 1 &&
      await page.locator('[data-provider-id="netflix"] [data-route="netflix"]').count() === 1,
    'Netflix catalog entry must expose both its permanent guide and workspace'
  );
  assert(
    await page.locator('[data-provider-id="openai"]').count() === 1,
    'provider catalog must contain exactly one OpenAI identity'
  );
  assert(
    await page.locator('[data-provider-id="facebook"]').count() === 1 &&
      await page.locator('[data-provider-id="instagram"]').count() === 1 &&
      await page.locator('[data-provider-id="whatsapp"]').count() === 1 &&
      await page.locator('[data-provider-id="threads"]').count() === 1,
    'provider catalog must contain separate Facebook, Instagram, WhatsApp, and Threads identities'
  );

  const localeFacebookAlt = {
    en: 'Facebook Accounts Center: Your information and permissions',
    es: 'Centro de cuentas de Facebook: Tu información y permisos',
    fr: 'Espace Comptes Facebook : Vos informations et autorisations',
    ru: 'Центр аккаунтов Facebook: раздел «Ваша информация и разрешения»'
  };
  const localeOpenAIGuideHeading = {
    en: 'How to download your data',
    es: 'Cómo descargar tus datos',
    fr: 'Comment télécharger vos données',
    ru: 'Как скачать ваши данные'
  };
  const localeOpenAction = {
    en: 'Open',
    es: 'Abrir',
    fr: 'Ouvrir',
    ru: 'Открыть'
  };
  for (const [locale, expectedAlt] of Object.entries(localeFacebookAlt)) {
    await selectLanguage(locale);
    assert(
      (await page.locator('[data-provider-id="netflix"] .provider-name').textContent()).trim() ===
        'Netflix',
      `${locale} did not preserve the canonical Netflix identity`
    );
    await route('#guide/netflix', '#netflix');
    assert(
      (await page.locator('.guide h1').textContent()).trim() === 'Netflix',
      `${locale} Netflix guide identity changed`
    );
    assert(
      await page.locator('#netflix .instruction-step').count() === 6,
      `${locale} Netflix guide instructions are incomplete`
    );
    assert(
      await page.locator(
        '#netflix .guide-refs a[href="https://help.netflix.com/en/node/101917"]'
      ).count() === 1,
      `${locale} Netflix guide official help route is missing`
    );
    assert(
      await page.locator('.guide [data-route="netflix"]').count() === 1,
      `${locale} Netflix guide cannot open the workspace`
    );
    await route('#guide/openai', '#openai');
    assert(
      (await page.locator('.guide h1').textContent()).trim() === 'OpenAI (ChatGPT)',
      `${locale} did not preserve the canonical OpenAI identity`
    );
    assert(
      await page.locator('.guide .state-chip').count() === 0,
      `${locale} guide header must not render a generic guide badge`
    );
    assert(
      (await page.locator('#openai h2').textContent()).trim() ===
        localeOpenAIGuideHeading[locale],
      `${locale} OpenAI guide heading is incorrect`
    );
    assert(
      await page.locator('#openai .instruction-step').count() === 7,
      `${locale} OpenAI export instructions are incomplete`
    );
    assert(
      await page.locator(
        '#openai a[href="https://help.openai.com/en/articles/7260999-how-do-i-export-my-chatgpt-history-and-data"]'
      ).count() === 1,
      `${locale} OpenAI official export route is missing`
    );
    await route('#guide/whatsapp', '#whatsapp');
    assert(
      (await page.locator('.guide h1').textContent()).trim() === 'WhatsApp',
      `${locale} did not preserve the canonical WhatsApp identity`
    );
    assert(
      await page.locator('#whatsapp .instruction-step').count() === 7,
      `${locale} WhatsApp export instructions are incomplete`
    );
    assert(
      await page.locator(
        '#whatsapp .guide-refs a[href="https://faq.whatsapp.com/526463418847093/"]'
      ).count() === 1 &&
        await page.locator(
          '#whatsapp .guide-refs a[href="https://faq.whatsapp.com/1180414079177245/"]'
        ).count() === 1,
      `${locale} WhatsApp official export routes are missing`
    );
    await route('#guide/threads', '#threads');
    assert(
      (await page.locator('.guide h1').textContent()).trim() === 'Threads',
      `${locale} did not preserve the canonical Threads identity`
    );
    assert(
      await page.locator('#threads .instruction-step').count() === 7,
      `${locale} Threads export instructions are incomplete`
    );
    assert(
      await page.locator(
        '#threads a[href="https://www.facebook.com/help/instagram/259803026523198"]'
      ).count() === 1,
      `${locale} Threads official export route is missing`
    );
    await route('#guide/facebook', '#facebook');
    assert(
      await page.locator(
        `#facebook .instruction-step[data-step-index="1"] img[alt="${expectedAlt}"]`
      ).count() === 1,
      `${locale} Facebook screenshot alternative is missing`
    );
    await route('#guide/google', '#google');
    const googleFirstStepLink = page.locator(
      '#google .instruction-step[data-step-index="1"] .instruction-step-link'
    );
    assert(
      await googleFirstStepLink.getAttribute('href') === 'https://takeout.google.com/?hl=en' &&
        (await googleFirstStepLink.textContent()).trim() ===
          `${localeOpenAction[locale]} takeout.google.com ↗`,
      `${locale} Google Takeout instruction is not directly actionable`
    );
    await route('#provider/netflix', '.workspace');
    assert(
      (await page.locator('.workspace h1').textContent()).trim() === 'Netflix',
      `${locale} Netflix workspace identity changed`
    );
    assert(
      await page.locator(
        '.workspace-header [data-route="guide"][data-provider="netflix"]'
      ).count() === 1,
      `${locale} Netflix workspace cannot open its guide`
    );
    assert(
      await page.locator('.import-panel .instruction-step').count() === 6,
      `${locale} Netflix instructions are incomplete`
    );
    assert(
      await page.locator(
        '.import-panel .guide-refs a[href="https://help.netflix.com/en/node/101917"]'
      ).count() === 1,
      `${locale} Netflix official help route is missing`
    );
    await route('#catalog', '.catalog');
  }
  await selectLanguage('en');

  const stepCounts = {
    netflix: 6,
    openai: 7,
    facebook: 7,
    instagram: 7,
    whatsapp: 7,
    threads: 7,
    linkedin: 7,
    tiktok: 6,
    x: 7,
    youtube: 7,
    google: 7
  };
  const instructionLinkHosts = {
    netflix: 'help.netflix.com',
    openai: 'chatgpt.com',
    facebook: 'accountscenter.facebook.com',
    instagram: 'accountscenter.instagram.com',
    whatsapp: 'faq.whatsapp.com',
    threads: 'accountscenter.instagram.com',
    linkedin: 'www.linkedin.com',
    tiktok: 'support.tiktok.com',
    x: 'x.com',
    youtube: 'takeout.google.com',
    google: 'takeout.google.com'
  };
  const screenshotIDs = [];
  const instructionHrefs = [];
  for (const [provider, expectedStepCount] of Object.entries(stepCounts)) {
    await route(`#guide/${provider}`, `#${provider}`);
    const providerRoot = `#${provider}`;
    const steps = page.locator(`${providerRoot} .instruction-step`);
    assert(
      await steps.count() === expectedStepCount,
      `${provider} instruction step count must be ${expectedStepCount}`
    );
    for (const step of await steps.all()) {
      assert(
        await step.locator('.instruction-step-copy').count() === 1,
        `${provider} step requires one instruction`
      );
      const link = step.locator('a.instruction-step-link');
      assert(await link.count() === 1, `${provider} step requires exactly one action link`);
      const linkRecord = await link.evaluate((element) => ({
        href: element.href,
        hostname: new URL(element.href).hostname,
        protocol: new URL(element.href).protocol,
        rel: element.rel,
        target: element.target,
        text: element.textContent.trim()
      }));
      assert(
        linkRecord.protocol === 'https:' &&
          linkRecord.hostname === instructionLinkHosts[provider] &&
          linkRecord.target === '_blank' &&
          linkRecord.rel.split(/\s+/).includes('noopener') &&
          linkRecord.rel.split(/\s+/).includes('noreferrer') &&
          linkRecord.text === `Open ${linkRecord.hostname.replace(/^www\./, '')} ↗`,
        `${provider} step action link is not the approved first-party target`
      );
      instructionHrefs.push(linkRecord.href);
      const stepNumber = await step.getAttribute('data-step-index');
      assert(
        await step.locator('.instruction-step-number').textContent() === stepNumber,
        `${provider} step requires its visible sequence number`
      );
      const image = step.locator('img.instruction-screenshot');
      assert(await image.count() === 1, `${provider} step requires exactly one screenshot`);
      await image.scrollIntoViewIfNeeded();
      await image.evaluate((element) => element.decode());
      const record = await image.evaluate((element) => ({
        alt: element.alt,
        complete: element.complete,
        id: element.dataset.screenshotId,
        naturalHeight: element.naturalHeight,
        naturalWidth: element.naturalWidth,
        path: new URL(element.src).pathname,
        width: element.getBoundingClientRect().width
      }));
      assert(record.alt, `${provider} screenshot requires alternative text`);
      assert(
        record.complete && record.naturalWidth >= 480 && record.naturalHeight >= 220,
        `${provider} screenshot failed to load at its approved resolution`
      );
      assert(
        record.path.startsWith('/images/instructions/') && record.path.endsWith('.png'),
        `${provider} screenshot path is outside the self-owned asset set`
      );
      assert(record.width <= page.viewportSize().width, `${provider} screenshot overflows`);
      screenshotIDs.push(record.id);
    }
  }
  assert(screenshotIDs.length === 75, 'every instruction step must render one screenshot');
  assert(instructionHrefs.length === 75, 'every instruction step must render one action link');
  assert(new Set(screenshotIDs).size === 21, 'approved screenshot set must contain 21 assets');
  assert(
    await page.locator('.screenshot-grid, .instruction-list, .ratio').count() === 0,
    'obsolete gallery, text-only, or placeholder instruction UI remains'
  );

  await route('#guide/youtube', '#youtube');
  assert(
    await page.locator(
      '#youtube a[href="https://takeout.google.com/settings/takeout/custom/youtube"]'
    ).count() === 1,
    'YouTube direct export route is missing'
  );
  assert(
    await page.locator(
      '#youtube .instruction-step-link[href="https://takeout.google.com/settings/takeout/custom/youtube?hl=en"]'
    ).count() === 7,
    'every YouTube instruction must open its approved Google Takeout route'
  );
  await route('#guide/google', '#google');
  assert(
    await page.locator('#google a[href="https://takeout.google.com"]').count() === 1,
    'Google direct export route is missing'
  );
  assert(
    await page.locator(
      '#google .instruction-step-link[href="https://takeout.google.com/?hl=en"]'
    ).count() === 7,
    'every Google instruction must open Google Takeout'
  );

  await route('#credits', '.credits');
  const tmdbLogo = page.locator('img.tmdb-logo');
  await tmdbLogo.evaluate((element) => element.decode());
  assert(
    await tmdbLogo.getAttribute('src') === 'images/tmdb-blue-square.svg',
    'Credits must use the approved local TMDB asset'
  );
  assert(
    (await page.locator('.tmdb-credit').textContent()).includes(
      'This product uses the TMDB API but is not endorsed or certified by TMDB.'
    ),
    'Credits must display the TMDB non-endorsement notice'
  );

  await route('#catalog', '.catalog');
  const openNetflix = page.locator('[data-provider-id="netflix"] [data-route="netflix"]');
  await openNetflix.focus();
  await page.keyboard.press('Enter');
  await page.locator('.workspace').waitFor();
  assert(
    await page.locator('#app-announcer[role="status"][aria-live="polite"]').count() === 1,
    'workspace requires one polite live announcer'
  );

  const invalidInput = page.locator('#netflix-file');
  await invalidInput.setInputFiles(invalidCSV);
  await page.getByText('invalid_header', {exact: false}).first().waitFor({timeout: 15000});
  await waitForState(
    (value) =>
      value.active_generation == null &&
      value.latest_failed_generation?.failure?.code === 'invalid_header',
    'invalid CSV failure'
  );

  const observedStates = await page.evaluate(() => {
    window.__downloadYourDataObservedStates = [];
    const collect = () => {
      document.querySelectorAll('.state-chip, .progress-meta span:first-child').forEach((node) => {
        const value = node.textContent.trim();
        if (value && !window.__downloadYourDataObservedStates.includes(value)) {
          window.__downloadYourDataObservedStates.push(value);
        }
      });
    };
    new MutationObserver(collect).observe(document.querySelector('#app'), {
      childList: true,
      subtree: true
    });
    collect();
    return window.__downloadYourDataObservedStates;
  });
  assert(Array.isArray(observedStates), 'state observer did not initialize');
  await page.locator('#netflix-file').setInputFiles(validCSV);
  await waitForState(
    (value) => value.active_generation?.analysis_level === 'local',
    'ready local generation'
  );
  await page.getByRole('tab', {name: 'Overview'}).waitFor();
  await page.waitForFunction(() => document.querySelectorAll('.kpi').length === 4);
  assert(await kpiValue('Activities') === '9', 'raw activity KPI must equal nine');
  assert(
    await page.getByRole('button', {name: 'Enrich with TMDB'}).isDisabled(),
    'TMDB enrichment must be disabled when server configuration is absent'
  );
  assert(
    await page.getByText('TMDB not configured', {exact: true}).count() >= 1,
    'ready-local state must disclose missing TMDB configuration'
  );
  assert(
    await page.locator(
      '.workspace-header [data-route="guide"][data-provider="netflix"]'
    ).count() === 1,
    'ready Netflix workspace must retain its permanent guide action'
  );

  const localSnapshot = await snapshot();
  const localEvents = await page.evaluate(async (generationID) => {
    const response = await fetch(
      `/api/providers/netflix/generations/${generationID}/events?after=0`,
      {cache: 'no-store'}
    );
    return response.json();
  }, localSnapshot.active_generation.id);
  assert(
    ['receiving', 'validating', 'importing', 'ready'].every((stateName) =>
      localEvents.events.some((event) => event.state === stateName)
    ),
    'local generation event journal is missing a declared lifecycle state'
  );
  const renderedStates = await page.evaluate(() => window.__downloadYourDataObservedStates);
  assert(
    renderedStates.includes('Receiving file') && renderedStates.includes('Validating'),
    `UI did not render receiving and validating states: ${renderedStates.join(', ')}`
  );
  assert(
    await page.locator('.chart-panel[role="region"], .chart-panel').count() >= 4,
    'overview charts are missing'
  );
  assert(
    await page.locator('.chart-data table').count() >= 2,
    'populated overview charts are missing data-table alternatives'
  );
  assert(
    await page.locator('.chart-panel').evaluateAll((panels) =>
      panels.every(
        (panel) =>
          panel.querySelector('.chart-data table') !== null ||
          panel.querySelector('.empty-copy') !== null
      )
    ),
    'an overview chart lacks either a data table or an explicit empty state'
  );

  await page.locator('[data-filter="startDate"]').fill('2026-02-01');
  await page.getByText('Choose both a start date', {exact: false}).waitFor();
  await page.locator('[data-filter="endDate"]').fill('2026-02-03');
  await page.waitForFunction(
    () =>
      [...document.querySelectorAll('.kpi')].some(
        (card) =>
          card.querySelector('.kpi-label')?.textContent === 'Activities' &&
          card.querySelector('.kpi-value')?.textContent === '3'
      )
  );
  await page.getByRole('button', {name: 'Clear', exact: true}).click();
  await page.waitForFunction(
    () =>
      [...document.querySelectorAll('.kpi')].some(
        (card) =>
          card.querySelector('.kpi-label')?.textContent === 'Activities' &&
          card.querySelector('.kpi-value')?.textContent === '9'
      )
  );

  const overviewTab = page.getByRole('tab', {name: 'Overview'});
  await overviewTab.focus();
  await page.keyboard.press('ArrowRight');
  const catalogTab = page.getByRole('tab', {name: 'Catalog'});
  assert(await catalogTab.getAttribute('aria-selected') === 'true', 'tab arrow navigation failed');
  assert(
    await page.locator('#netflix-view-panel[aria-labelledby="netflix-view-catalog"]').count() === 1,
    'Catalog tabpanel is not labeled by its selected tab'
  );
  await page.waitForFunction(() => document.querySelectorAll('.dimension-grid .chart-panel').length === 9);
  assert(
    await page.getByRole('heading', {name: 'Genres by viewing year'}).count() === 1,
    'Catalog is missing genres by viewing year'
  );
  assert(await page.locator('tbody .record-title').count() === 9, 'Catalog must list all raw rows');

  await page.setViewportSize({width: 393, height: 852});
  const mobileLayout = await page.evaluate(() => {
    const rail = document.querySelector('.workspace-rail').getBoundingClientRect();
    const main = document.querySelector('.workspace-main').getBoundingClientRect();
    return {
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
      railBottom: rail.bottom,
      mainTop: main.top,
      railColumns: getComputedStyle(document.querySelector('.workspace-rail')).gridTemplateColumns
    };
  });
  assert(
    mobileLayout.documentWidth <= mobileLayout.viewportWidth,
    `mobile workspace overflows by ${mobileLayout.documentWidth - mobileLayout.viewportWidth}px`
  );
  assert(mobileLayout.railBottom <= mobileLayout.mainTop + 1, 'mobile rail must collapse above content');
  assert(!mobileLayout.railColumns.includes(' '), 'mobile rail must use one column');

  await route('#catalog', '.catalog');
  const mobileNetflixCatalog = await page.evaluate(() => {
    const grid = document.querySelector('.catalog-grid');
    const card = document.querySelector('[data-provider-id="netflix"]');
    const actions = card.querySelector('.provider-actions');
    const guideOnlyCards = [
      ...document.querySelectorAll(
        '.provider-card:not([data-provider-id="netflix"])'
      )
    ];
    const cardBox = card.getBoundingClientRect();
    const actionsBox = actions.getBoundingClientRect();
    const providerIcons = [...document.querySelectorAll('.provider-icon')];
    return {
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
      gridColumns: getComputedStyle(grid).gridTemplateColumns.split(' ').filter(Boolean).length,
      actionCount: actions.querySelectorAll('button').length,
      actionsLeft: actionsBox.left,
      actionsRight: actionsBox.right,
      cardLeft: cardBox.left,
      cardRight: cardBox.right,
      providerIconCount: providerIcons.length,
      providerIconsLarge: providerIcons.every(
        (providerIcon) => providerIcon.getBoundingClientRect().width >= 48
      ),
      providerIconsContained: providerIcons.every((providerIcon) => {
        const iconBox = providerIcon.getBoundingClientRect();
        const markBox = providerIcon.closest('.provider-mark').getBoundingClientRect();
        return (
          iconBox.width > 0 &&
          iconBox.height > 0 &&
          iconBox.left >= markBox.left &&
          iconBox.right <= markBox.right &&
          iconBox.top >= markBox.top &&
          iconBox.bottom <= markBox.bottom
        );
      }),
      summariesVisible: [...document.querySelectorAll('.provider-summary')].every(
        (summary) => summary.getBoundingClientRect().height > 0
      ),
      guideOnlyCardsHaveOneAction: guideOnlyCards.every((guideOnlyCard) => {
        const guideOnlyActions = guideOnlyCard.querySelector('.provider-actions');
        return (
          guideOnlyActions.querySelectorAll('button').length === 1 &&
          guideOnlyActions.querySelectorAll('[data-route="guide"]').length === 1 &&
          guideOnlyActions.querySelector('.state-chip') === null
        );
      })
    };
  });
  assert(
    mobileNetflixCatalog.documentWidth <= mobileNetflixCatalog.viewportWidth &&
      mobileNetflixCatalog.gridColumns === 1 &&
      mobileNetflixCatalog.actionCount === 2 &&
      mobileNetflixCatalog.actionsLeft >= mobileNetflixCatalog.cardLeft &&
      mobileNetflixCatalog.actionsRight <= mobileNetflixCatalog.cardRight,
    'mobile Netflix provider card must contain guide and workspace actions without overflow'
  );
  assert(
    mobileNetflixCatalog.guideOnlyCardsHaveOneAction &&
      mobileNetflixCatalog.summariesVisible,
    'mobile guide-only provider cards must keep their summary and one View guide action'
  );
  assert(
    mobileNetflixCatalog.providerIconCount === 11 &&
      mobileNetflixCatalog.providerIconsLarge &&
      mobileNetflixCatalog.providerIconsContained,
    'mobile provider cards must retain large contained product logos'
  );

  await route('#guide/netflix', '#netflix');
  const mobileGuideLayout = await page.evaluate(() => {
    const step = document.querySelector('#netflix .instruction-step');
    const content = step.querySelector('.instruction-step-content').getBoundingClientRect();
    const link = step.querySelector('.instruction-step-link').getBoundingClientRect();
    const image = step.querySelector('.instruction-screenshot').getBoundingClientRect();
    const stepBox = step.getBoundingClientRect();
    return {
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
      gridColumns: getComputedStyle(step).gridTemplateColumns,
      contentBottom: content.bottom,
      linkLeft: link.left,
      linkRight: link.right,
      imageTop: image.top,
      imageLeft: image.left,
      imageRight: image.right,
      stepLeft: stepBox.left,
      stepRight: stepBox.right
    };
  });
  assert(
    mobileGuideLayout.documentWidth <= mobileGuideLayout.viewportWidth,
    `mobile visual guide overflows by ${
      mobileGuideLayout.documentWidth - mobileGuideLayout.viewportWidth
    }px`
  );
  assert(
    mobileGuideLayout.gridColumns.split(' ').length === 2,
    'mobile visual step must collapse to number and instruction columns'
  );
  assert(
    mobileGuideLayout.imageTop >= mobileGuideLayout.contentBottom &&
      mobileGuideLayout.linkLeft >= mobileGuideLayout.stepLeft &&
      mobileGuideLayout.linkRight <= mobileGuideLayout.stepRight &&
      mobileGuideLayout.imageLeft >= mobileGuideLayout.stepLeft &&
      mobileGuideLayout.imageRight <= mobileGuideLayout.stepRight,
    'mobile action link and screenshot must remain inside the numbered step'
  );
  assert(
    await page.locator('#netflix .instruction-step img.instruction-screenshot').count() === 6,
    'mobile Netflix guide must retain one screenshot per step'
  );
  await route('#provider/netflix', '.workspace');

  await page.emulateMedia({reducedMotion: 'reduce'});
  const reducedMotion = await page.locator('.button').first().evaluate((element) => ({
    animation: getComputedStyle(element).animationDuration,
    transition: getComputedStyle(element).transitionDuration
  }));
  assert(
    Number.parseFloat(reducedMotion.animation) <= 0.00001 &&
      Number.parseFloat(reducedMotion.transition) <= 0.00001,
    `reduced-motion contract is not active: ${JSON.stringify(reducedMotion)}`
  );
  await page.emulateMedia({reducedMotion: 'no-preference'});
  await page.setViewportSize({width: 1440, height: 1000});

  const contrast = await page.evaluate(() => {
    const rgb = (value) => value.match(/\d+(?:\.\d+)?/g).slice(0, 3).map(Number);
    const luminance = (value) => {
      const channels = rgb(value).map((channel) => {
        const normalized = channel / 255;
        return normalized <= 0.03928
          ? normalized / 12.92
          : ((normalized + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
    };
    const ratio = (foreground, background) => {
      const values = [luminance(foreground), luminance(background)].sort((a, b) => b - a);
      return (values[0] + 0.05) / (values[1] + 0.05);
    };
    const body = getComputedStyle(document.body);
    const primary = getComputedStyle(document.querySelector('.button-primary'));
    return {
      body: ratio(body.color, body.backgroundColor),
      primary: ratio(primary.color, primary.backgroundColor)
    };
  });
  assert(contrast.body >= 4.5, `body contrast ${contrast.body} is below 4.5`);
  assert(contrast.primary >= 4.5, `primary button contrast ${contrast.primary} is below 4.5`);

  const beforeReplacement = (await snapshot()).active_generation.id;
  await page.locator('#netflix-file').setInputFiles(validCSV);
  const replacementDialog = page.getByRole('dialog', {
    name: 'Replace the active Netflix library?'
  });
  await replacementDialog.waitFor();
  assert(
    (await replacementDialog.textContent()).includes(
      'The current library stays active while the new CSV validates and imports.'
    ),
    'replacement disclosure is incomplete'
  );
  await replacementDialog.getByRole('button', {name: 'Cancel'}).click();
  await replacementDialog.waitFor({state: 'detached'});
  assert(
    (await snapshot()).active_generation.id === beforeReplacement,
    'canceling replacement changed the active generation'
  );

  await page.getByRole('button', {name: 'Delete Netflix data'}).click();
  let deleteDialog = page.getByRole('dialog', {name: 'Delete all Netflix data?'});
  await deleteDialog.waitFor();
  await deleteDialog.getByRole('button', {name: 'Cancel'}).click();
  assert((await snapshot()).active_generation.id === beforeReplacement, 'delete cancel changed data');
  await page.getByRole('button', {name: 'Delete Netflix data'}).click();
  deleteDialog = page.getByRole('dialog', {name: 'Delete all Netflix data?'});
  await deleteDialog.getByRole('button', {name: 'Delete all Netflix data'}).click();
  await waitForState(
    (value) =>
      value.state === 'empty' &&
      value.active_generation == null &&
      value.building_generation == null,
    'empty provider after deletion'
  );
  await page.getByRole('heading', {name: 'Import Netflix Viewing activity'}).waitFor();

  const externalRequests = requestURLs.filter(
    (rawURL) =>
      !rawURL.startsWith('about:') &&
      rawURL !== baseURL &&
      !rawURL.startsWith(`${baseURL}/`) &&
      !sharedShellURLs.has(rawURL)
  );
  assert(
    externalRequests.length === 0,
    `browser made external requests: ${externalRequests.join(', ')}`
  );
  assert(
    [...sharedShellURLs].every((url) => requestURLs.includes(url)),
    `browser did not load the complete mpr-ui shell: ${[...sharedShellURLs]
      .filter((url) => !requestURLs.includes(url))
      .join(', ')}`
  );
  assert(browserErrors.length === 0, `browser errors: ${browserErrors.join(' | ')}`);
}
