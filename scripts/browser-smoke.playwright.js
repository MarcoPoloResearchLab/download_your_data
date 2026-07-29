async page => {
  const baseURL = '__BASE_URL__';
  const validCSV = '__VALID_CSV__';
  const invalidCSV = '__INVALID_CSV__';
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
  await page.locator('[data-provider-id="netflix"]').waitFor();
  assert(
    await page.locator('.provider-row[data-provider-id]').count() === 9,
    'provider catalog must contain nine canonical providers'
  );
  assert(
    await page.locator('[data-provider-id="netflix"]').count() === 1,
    'provider catalog must contain exactly one Netflix identity'
  );
  assert(
    await page.locator('[data-provider-id="openai"]').count() === 1,
    'provider catalog must contain exactly one OpenAI identity'
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
  for (const [locale, expectedAlt] of Object.entries(localeFacebookAlt)) {
    await selectLanguage(locale);
    assert(
      (await page.locator('[data-provider-id="netflix"] .provider-name').textContent()).trim() ===
        'Netflix',
      `${locale} did not preserve the canonical Netflix identity`
    );
    await route('#guide/openai', '#openai');
    assert(
      (await page.locator('.guide h1').textContent()).trim() === 'OpenAI (ChatGPT)',
      `${locale} did not preserve the canonical OpenAI identity`
    );
    assert(
      (await page.locator('#openai h2').textContent()).trim() ===
        localeOpenAIGuideHeading[locale],
      `${locale} OpenAI guide heading is incorrect`
    );
    assert(
      await page.locator('#openai .instruction-list li').count() === 7,
      `${locale} OpenAI export instructions are incomplete`
    );
    assert(
      await page.locator(
        '#openai a[href="https://help.openai.com/en/articles/7260999-how-do-i-export-my-chatgpt-history-and-data"]'
      ).count() === 1,
      `${locale} OpenAI official export route is missing`
    );
    await route('#guide/facebook', '#facebook');
    assert(
      await page.locator(`#facebook img[alt="${expectedAlt}"]`).count() === 1,
      `${locale} Facebook screenshot alternative is missing`
    );
    await route('#provider/netflix', '.workspace');
    assert(
      (await page.locator('.workspace h1').textContent()).trim() === 'Netflix',
      `${locale} Netflix workspace identity changed`
    );
    assert(
      await page.locator('.import-panel .instruction-list li').count() === 6,
      `${locale} Netflix instructions are incomplete`
    );
    assert(
      await page.locator(
        '.import-panel a[href="https://help.netflix.com/en/node/101917"]'
      ).count() === 1,
      `${locale} Netflix official help route is missing`
    );
    await route('#catalog', '.catalog');
  }
  await selectLanguage('en');

  const screenshotCounts = {
    openai: 0,
    facebook: 2,
    instagram: 2,
    linkedin: 2,
    tiktok: 0,
    x: 2,
    youtube: 2,
    google: 2
  };
  const screenshotIDs = [];
  for (const [provider, expectedCount] of Object.entries(screenshotCounts)) {
    await route(`#guide/${provider}`, `#${provider}`);
    const images = page.locator(`#${provider} img.instruction-screenshot`);
    assert(
      await images.count() === expectedCount,
      `${provider} screenshot count must be ${expectedCount}`
    );
    for (const image of await images.all()) {
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
  assert(screenshotIDs.length === 12, 'authenticated guide set must contain 12 screenshots');
  assert(new Set(screenshotIDs).size === 12, 'authenticated screenshot IDs must be unique');
  assert(await page.locator('.ratio').count() === 0, 'placeholder screenshot tiles remain');

  await route('#guide/youtube', '#youtube');
  assert(
    await page.locator(
      '#youtube a[href="https://takeout.google.com/settings/takeout/custom/youtube"]'
    ).count() === 1,
    'YouTube direct export route is missing'
  );
  await route('#guide/google', '#google');
  assert(
    await page.locator('#google a[href="https://takeout.google.com"]').count() === 1,
    'Google direct export route is missing'
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
      !rawURL.startsWith(`${baseURL}/`)
  );
  assert(
    externalRequests.length === 0,
    `browser made external requests: ${externalRequests.join(', ')}`
  );
  assert(browserErrors.length === 0, `browser errors: ${browserErrors.join(' | ')}`);
}
