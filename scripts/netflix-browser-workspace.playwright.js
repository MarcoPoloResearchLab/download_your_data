async page => {
  const baseURL = '__BASE_URL__';
  const viewingCSV = '__VIEWING_CSV__';
  const sessionCookie = '__SESSION_COOKIE__';
  const sessionToken = '__SESSION_TOKEN__';
  const sharedShellURLs = new Set([
    'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui.css',
    'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui-config.js',
    'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui.js'
  ]);
  const browserErrors = [];
  const requests = [];
  page.on('console', (message) => {
    if (message.type() === 'error') {
      browserErrors.push(`console: ${message.text()}`);
    }
  });
  page.on('pageerror', (error) => browserErrors.push(`pageerror: ${error.message}`));
  page.on('request', (request) => {
    requests.push({
      method: request.method(),
      url: request.url(),
      postData: request.postData() || ''
    });
  });

  const assert = (condition, message) => {
    if (!condition) {
      throw new Error(message);
    }
  };
  const snapshot = async () =>
    page.evaluate(async () => {
      const response = await fetch('/api/providers/netflix', {
        cache: 'no-store',
        credentials: 'include'
      });
      if (!response.ok) {
        throw new Error(`snapshot HTTP ${response.status}`);
      }
      return response.json();
    });
  const waitForState = async (predicate, label) => {
    const deadline = Date.now() + 20000;
    while (Date.now() < deadline) {
      const current = await snapshot();
      if (predicate(current)) {
        return current;
      }
      await page.waitForTimeout(50);
    }
    throw new Error(`timed out waiting for ${label}`);
  };
  const route = async (hash, selector) => {
    await page.evaluate((nextHash) => {
      window.location.hash = nextHash;
    }, hash);
    await page.locator(selector).waitFor();
  };
  const selectLanguage = async (locale) => {
    await page.locator(`[data-language="${locale}"]`).click();
    await page.waitForFunction(
      (expectedLocale) => document.documentElement.lang === expectedLocale,
      locale
    );
  };
  const openConsentDialog = async () => {
    const retry = page.locator('[data-action="retry-enrichment"]');
    if (await retry.count()) {
      await retry.click();
    } else {
      await page.locator('[data-action="enrich"]').click();
    }
    const dialog = page.locator('dialog');
    await dialog.waitFor();
    assert(
      await dialog.getAttribute('aria-labelledby') === 'confirmation-title',
      'consent dialog is missing its accessible title relationship'
    );
    return dialog;
  };
  const tmdbCreateRequests = () =>
    requests.filter(
      (request) =>
        request.method === 'POST' &&
        request.url.endsWith('/api/providers/netflix/generations') &&
        request.postData.includes('"analysis_level":"tmdb"')
    );
  const localCreateRequests = () =>
    requests.filter(
      (request) =>
        request.method === 'POST' &&
        request.url.endsWith('/api/providers/netflix/generations') &&
        request.postData.includes('"analysis_level":"local"')
    );
  const syntheticViewingCSV = `Title,Date
Synthetic Film,1/1/26
Synthetic Series: Season 1: First,1/2/26
Synthetic Series: Season 1: Second,2/2/26
Another Film,2/3/26
`;
  const dropViewingActivity = async () => {
    await page.evaluate((contents) => {
      const dropZone = document.querySelector('.drop-zone');
      if (!dropZone) {
        throw new Error('Netflix drop zone is missing');
      }
      const transfer = new DataTransfer();
      transfer.items.add(
        new File([contents], 'ViewingActivity.csv', {type: 'text/csv'})
      );
      dropZone.dispatchEvent(
        new DragEvent('drop', {
          bubbles: true,
          cancelable: true,
          dataTransfer: transfer
        })
      );
    }, syntheticViewingCSV);
  };

  await page.setViewportSize({width: 1440, height: 1000});
  await page.goto(baseURL, {waitUntil: 'networkidle'});
  await page.waitForFunction(
    () =>
      customElements.get('mpr-header') &&
      customElements.get('mpr-footer') &&
      window.MPRUI?.testing
  );
  assert(
    await page.locator('mpr-header header[role="banner"]').count() === 1 &&
      await page.locator('mpr-footer footer[role="contentinfo"]').count() === 1,
    'configured workflow must retain the mpr-ui header and footer'
  );
  await page.locator('[data-provider-id="netflix"]').waitFor();
  assert(
    requests.every(
      (request) =>
        !request.url.startsWith(`${baseURL}/api/`) ||
        request.url === `${baseURL}/api/health`
    ),
    'anonymous catalog made a protected application request'
  );
  await page.context().addCookies([
    {
      name: sessionCookie,
      value: sessionToken,
      url: baseURL,
      httpOnly: true,
      sameSite: 'Lax'
    }
  ]);
  await page.evaluate(() => {
    window.MPRUI.testing.authenticate(document.querySelector('#app-header'), {
      user_id: 'browser-netflix-user',
      user_email: 'browser-contract@example.invalid',
      user_display_name: 'Browser Contract',
      user_avatar_url: 'https://lh3.googleusercontent.com/a/browser-contract',
      display: 'Browser Contract',
      avatar_url: 'https://lh3.googleusercontent.com/a/browser-contract'
    });
  });

  for (const locale of ['en', 'es', 'fr', 'ru']) {
    await selectLanguage(locale);
    const netflixRows = page.locator('[data-provider-id="netflix"]');
    assert(await netflixRows.count() === 1, `${locale} has a duplicate Netflix provider`);
    assert(
      (await netflixRows.locator('.provider-name').textContent()).trim() === 'Netflix',
      `${locale} changed the Netflix provider identity`
    );
    assert(
      !(await netflixRows.textContent()).includes('undefined'),
      `${locale} Netflix catalog copy contains an unresolved value`
    );
  }
  await selectLanguage('en');

  const openButton = page.locator('[data-provider-id="netflix"] [data-route="netflix"]');
  await openButton.focus();
  await page.keyboard.press('Enter');
  await page.locator('.workspace').waitFor();
  assert(
    await page.getByText('Configured', {exact: true}).count() === 1,
    'deterministic server must expose configured TMDB state'
  );
  assert(
    await page.locator('#language-switcher[aria-label="Language"]').count() === 1,
    'language selector is not localized or named'
  );

  await page.evaluate(() => {
    window.__downloadYourDataObservedStates = [];
    const collect = () => {
      document
        .querySelectorAll('.state-chip, .progress-meta span:first-child, .alert p')
        .forEach((node) => {
          const text = node.textContent.trim();
          if (text && !window.__downloadYourDataObservedStates.includes(text)) {
            window.__downloadYourDataObservedStates.push(text);
          }
        });
    };
    new MutationObserver(collect).observe(document.querySelector('#app'), {
      childList: true,
      subtree: true
    });
    collect();
  });

  let signalInitialLocalCreate = () => {};
  const initialLocalCreateSeen = new Promise((resolve) => {
    signalInitialLocalCreate = resolve;
  });
  let releaseInitialLocalCreate = () => {};
  const initialLocalCreateGate = new Promise((resolve) => {
    releaseInitialLocalCreate = resolve;
  });
  let holdingInitialLocalCreate = false;
  const generationCreationPattern = '**/api/providers/netflix/generations';
  const holdInitialLocalCreate = async (requestRoute) => {
    const request = requestRoute.request();
    if (
      !holdingInitialLocalCreate &&
      request.method() === 'POST' &&
      request.postData()?.includes('"analysis_level":"local"')
    ) {
      holdingInitialLocalCreate = true;
      signalInitialLocalCreate();
      await initialLocalCreateGate;
    }
    await requestRoute.continue();
  };
  await page.route(generationCreationPattern, holdInitialLocalCreate);
  await dropViewingActivity();
  await Promise.race([
    initialLocalCreateSeen,
    page.waitForTimeout(5000).then(() => {
      throw new Error('timed out waiting for initial local creation request');
    })
  ]);
  await dropViewingActivity();
  await page.waitForTimeout(100);
  assert(
    localCreateRequests().length === 1,
    `busy drop zone created ${localCreateRequests().length} local generations; want 1`
  );
  releaseInitialLocalCreate();
  const local = await waitForState(
    (value) => value.active_generation?.analysis_level === 'local',
    'initial ready-local generation'
  );
  const initialLocalID = local.active_generation.id;
  await page.getByRole('tab', {name: 'Overview'}).waitFor();
  await page.waitForFunction(
    () =>
      [...document.querySelectorAll('.kpi')].some(
        (card) =>
          card.querySelector('.kpi-label')?.textContent === 'Activities' &&
          card.querySelector('.kpi-value')?.textContent === '4'
      )
  );
  assert(
    await page.getByText('Ready', {exact: true}).count() >= 1,
    'configured raw generation did not render ready private state'
  );

  const localEvents = await page.evaluate(async (generationID) => {
    const response = await fetch(
      `/api/providers/netflix/generations/${generationID}/events?after=0`,
      {cache: 'no-store'}
    );
    return response.json();
  }, initialLocalID);
  assert(
    ['receiving', 'validating', 'importing', 'ready'].every((stateName) =>
      localEvents.events.some((event) => event.state === stateName)
    ),
    'browser-imported generation did not record its complete local lifecycle'
  );

  let dialog = await openConsentDialog();
  assert(
    await dialog.getByRole('heading', {name: 'Send title queries to TMDB?'}).count() === 1,
    'consent dialog title is incorrect'
  );
  const consentText = await dialog.textContent();
  assert(
    consentText.includes(
      'Only unique derived title queries and the selected locale are sent.'
    ) &&
      consentText.includes(
        'Dates, profile data, raw CSV bytes, and complete activity rows are never sent.'
      ),
    'consent dialog does not disclose the exact TMDB boundary'
  );
  await page.waitForTimeout(250);
  assert(
    tmdbCreateRequests().length === 0,
    'browser created a TMDB generation before explicit consent'
  );
  await dialog.getByRole('button', {name: 'Authorize title queries'}).click();
  let enriching = await waitForState(
    (value) =>
      value.active_generation?.id === initialLocalID &&
      value.building_generation?.analysis_level === 'tmdb' &&
      value.building_generation?.state === 'enriching',
    'cancelable enrichment'
  );
  const canceledGenerationID = enriching.building_generation.id;
  await page.waitForTimeout(250);
  assert(
    await page.locator('progress[aria-label="Generation progress"]').count() >= 1,
    'enrichment progress is missing an accessible name'
  );
  assert(
    await page.getByText('Enriching', {exact: true}).count() >= 1,
    'backend enriching state is not rendered'
  );
  await page
    .locator('.workspace-content [data-action="cancel-generation"]')
    .click();
  const canceled = await waitForState(
    (value) =>
      value.active_generation?.id === initialLocalID &&
      value.building_generation == null &&
      value.latest_failed_generation?.id === canceledGenerationID &&
      value.latest_failed_generation?.failure?.code === 'canceled',
    'canceled enrichment'
  );
  assert(canceled.active_generation.analysis_level === 'local', 'cancel changed active data');
  await page.getByText('The requested generation was canceled.', {exact: false}).waitFor();
  assert(
    await page.getByRole('button', {name: 'Retry'}).count() === 1,
    'canceled enrichment must expose one retry action'
  );

  dialog = await openConsentDialog();
  await dialog.getByRole('button', {name: 'Authorize title queries'}).click();
  const rateLimited = await waitForState(
    (value) =>
      value.active_generation?.id === initialLocalID &&
      value.building_generation == null &&
      value.latest_failed_generation?.failure?.code === 'rate_limited',
    'rate-limited enrichment'
  );
  assert(rateLimited.active_generation.analysis_level === 'local', 'failure changed active data');
  await page.getByText('rate_limited', {exact: false}).first().waitFor();
  assert(
    await page.locator('.alert[role="alert"]').count() >= 1,
    'remote failure is missing assertive status semantics'
  );

  dialog = await openConsentDialog();
  await dialog.getByRole('button', {name: 'Authorize title queries'}).click();
  const readyTMDB = await waitForState(
    (value) =>
      value.active_generation?.analysis_level === 'tmdb' &&
      value.active_generation?.review_title_count === 0 &&
      value.building_generation == null,
    'ready enriched generation'
  );
  await page.getByText('Ready + TMDB', {exact: true}).first().waitFor();
  assert(
    await page.getByRole('link', {name: 'Export enriched CSV'}).count() === 1,
    'ready-enriched generation is missing export'
  );
  assert(
    await page.getByRole('button', {name: 'Retry'}).count() === 0,
    'successful replacement retained a stale Retry action'
  );

  const readyTMDBEvents = await page.evaluate(async (generationID) => {
    const response = await fetch(
      `/api/providers/netflix/generations/${generationID}/events?after=0`,
      {cache: 'no-store'}
    );
    return response.json();
  }, readyTMDB.active_generation.id);
  assert(
    ['enriching', 'ready'].every((stateName) =>
      readyTMDBEvents.events.some((event) => event.state === stateName)
    ),
    'ready TMDB generation event journal is incomplete'
  );

  const readyTMDBID = readyTMDB.active_generation.id;
  await page.locator('#netflix-file').setInputFiles(viewingCSV);
  dialog = page.getByRole('dialog', {name: 'Replace the active Netflix library?'});
  await dialog.waitFor();
  await dialog.getByRole('button', {name: 'Build replacement'}).click();
  const replacementLocal = await waitForState(
    (value) =>
      value.active_generation?.analysis_level === 'local' &&
      value.active_generation?.id !== readyTMDBID &&
      value.building_generation == null,
    'successful local replacement'
  );
  assert(
    replacementLocal.active_generation.activity_count === 4,
    'local replacement changed source activity coverage'
  );

  await selectLanguage('es');
  dialog = await openConsentDialog();
  await dialog.locator('button[value="confirm"]').click();
  await dialog.waitFor({state: 'detached'});
  await selectLanguage('en');
  const reviewReady = await waitForState(
    (value) =>
      value.active_generation?.analysis_level === 'tmdb' &&
      value.active_generation?.review_title_count === 1 &&
      value.building_generation == null,
    'review-bearing enriched generation'
  );
  assert(
    reviewReady.active_generation.matched_title_count === 1 &&
      reviewReady.active_generation.unmatched_title_count === 1,
    'final exact match coverage is incorrect'
  );
  await page.getByText('Action needed', {exact: true}).first().waitFor();
  await page.getByText('Some titles need review.', {exact: false}).first().waitFor();

  await page.getByRole('tab', {name: 'Match quality'}).click();
  await page.locator('#netflix-view-panel[aria-labelledby="netflix-view-match_quality"]').waitFor();
  await page.waitForFunction(() => document.querySelectorAll('tbody .record-title').length === 4);
  for (const [label, expected] of [
    ['Matched', '1'],
    ['Review', '1'],
    ['Unmatched', '1']
  ]) {
    const card = page.locator('.kpi').filter({hasText: label}).first();
    assert(
      (await card.locator('.kpi-value').textContent()).trim() === expected,
      `${label} title coverage must equal ${expected}`
    );
  }
  assert(
    await page.locator('.chart-data table').count() >= 1,
    'match-coverage chart is missing its table alternative'
  );

  await page.locator('[data-filter="matchStatus"]').selectOption('review');
  await page.waitForFunction(() => document.querySelectorAll('tbody .record-title').length === 2);
  await page.locator('[data-filter="startDate"]').fill('2026-01-01');
  await page.locator('[data-filter="endDate"]').fill('2026-01-31');
  await page.waitForFunction(
    () =>
      document.querySelectorAll('tbody .record-title').length === 1 &&
      document.querySelector('tbody .record-title')?.textContent.includes('Synthetic Series')
  );
  assert(
    requests.some(
      (request) =>
        request.url.includes('/analytics?') &&
        request.url.includes('start_date=2026-01-01') &&
        request.url.includes('end_date=2026-01-31') &&
        request.url.includes('match_status=review')
    ) &&
      requests.some(
        (request) =>
          request.url.includes('/records?') &&
          request.url.includes('start_date=2026-01-01') &&
          request.url.includes('end_date=2026-01-31') &&
          request.url.includes('match_status=review')
      ),
    'shared date and match filters did not reach both canonical APIs'
  );

  await page.getByRole('button', {name: 'Clear', exact: true}).click();
  await page.getByRole('tab', {name: 'Catalog'}).click();
  await page.waitForFunction(() => document.querySelectorAll('.dimension-grid .chart-panel').length === 9);
  const genresByYear = page.locator('.chart-panel').filter({hasText: 'Genres by viewing year'});
  assert(
    await genresByYear.count() === 1 && await genresByYear.locator('.chart-data table').count() === 1,
    'enriched Catalog is missing the genres-by-viewing-year table'
  );
  await page.waitForFunction(() => document.querySelectorAll('tbody .record-title').length === 4);
  assert(
    (await page.locator('#netflix-view-panel').textContent()).includes('98 min'),
    'accepted metadata is missing from the enriched catalog'
  );

  const downloadPromise = page.waitForEvent('download');
  await page.getByRole('link', {name: 'Export enriched CSV'}).click();
  const download = await downloadPromise;
  assert(
    /^netflix-enriched-ng_[a-f0-9]{32}\.csv$/.test(download.suggestedFilename()),
    `unexpected export filename ${download.suggestedFilename()}`
  );
  const exportStream = await download.createReadStream();
  let exportCSV = '';
  for await (const chunk of exportStream) {
    exportCSV += chunk.toString('utf8');
  }
  assert(
    exportCSV.includes('MatchStatus') &&
      exportCSV.includes('matched') &&
      exportCSV.includes('review') &&
      exportCSV.includes('unmatched'),
    'browser export is missing canonical match outcomes'
  );

  await route('#credits', '.credits');
  const tmdbLogo = page.locator('img.tmdb-logo');
  await tmdbLogo.evaluate((element) => element.decode());
  assert(
    await tmdbLogo.getAttribute('src') === 'images/tmdb-blue-square.svg',
    'TMDB credit asset is not self-owned'
  );
  assert(
    (await page.locator('.tmdb-credit').textContent()).includes(
      'This product uses the TMDB API but is not endorsed or certified by TMDB.'
    ),
    'TMDB credit notice is missing'
  );

  await route('#app/netflix', '.workspace');
  await page.getByRole('button', {name: 'Delete Netflix data'}).click();
  dialog = page.getByRole('dialog', {name: 'Delete all Netflix data?'});
  await dialog.waitFor();
  assert(
    (await dialog.textContent()).includes('TMDB cache entry'),
    'full deletion disclosure omits the private cache'
  );
  await dialog.getByRole('button', {name: 'Delete all Netflix data'}).click();
  await waitForState(
    (value) =>
      value.state === 'empty' &&
      value.active_generation == null &&
      value.building_generation == null &&
      value.latest_failed_generation == null,
    'complete provider deletion'
  );
  await page.getByRole('heading', {name: 'Import Netflix Viewing activity'}).waitFor();

  const observedStates = await page.evaluate(() => window.__downloadYourDataObservedStates);
  for (const expected of [
    'Receiving file',
    'Validating',
    'Ready',
    'Enriching',
    'Building replacement',
    'Ready + TMDB',
    'Action needed'
  ]) {
    assert(
      observedStates.some((value) => value.includes(expected)),
      `UI never rendered declared state ${expected}: ${observedStates.join(' | ')}`
    );
  }
  assert(
    observedStates.some((value) => value.includes('canceled')) &&
      observedStates.some((value) => value.includes('rate_limited')),
    'UI did not render canceled and failed outcomes'
  );

  const creations = tmdbCreateRequests();
  assert(creations.length === 4, `browser TMDB creation count ${creations.length}; want 4`);
  for (const request of creations) {
    const payload = JSON.parse(request.postData);
    assert(
      payload.tmdb_title_query_consent === 'authorize-tmdb-title-queries',
      'TMDB generation omitted the exact consent contract'
    );
    assert(
      payload.locale === 'en-US' || payload.locale === 'es-ES',
      `unexpected enrichment locale ${payload.locale}`
    );
  }

  const externalRequests = requests.filter((request) => {
    if (request.url.startsWith('about:') || request.url.startsWith('data:')) {
      return false;
    }
    return (
      request.url !== baseURL &&
      !request.url.startsWith(`${baseURL}/`) &&
      !request.url.startsWith('https://accounts.google.com/') &&
      !request.url.startsWith('https://cdn.jsdelivr.net/') &&
      !request.url.startsWith('https://lh3.googleusercontent.com/')
    );
  });
  assert(
    externalRequests.length === 0,
    `browser made external requests: ${externalRequests.map((request) => request.url).join(', ')}`
  );
  assert(
    [...sharedShellURLs].every((url) =>
      requests.some((request) => request.url === url)
    ),
    'configured workflow did not load the complete mpr-ui shell'
  );
  assert(browserErrors.length === 0, `browser errors: ${browserErrors.join(' | ')}`);
}
