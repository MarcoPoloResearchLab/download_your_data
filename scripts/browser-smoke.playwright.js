async page => {
  const baseURL = '__BASE_URL__';
  const validCSV = '__VALID_CSV__';
  const sessionCookie = '__SESSION_COOKIE__';
  const sessionToken = '__SESSION_TOKEN__';
  const browserErrors = [];
  const requestURLs = [];

  page.on('console', (message) => {
    if (message.type() === 'error') {
      browserErrors.push(`console: ${message.text()}`);
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
  const protectedRequests = () =>
    requestURLs.filter(
      (rawURL) =>
        rawURL.startsWith(`${baseURL}/api/`) &&
        rawURL !== `${baseURL}/api/health`
    );
  const assertNoHorizontalOverflow = async (label) => {
    const dimensions = await page.evaluate(() => ({
      viewport: window.innerWidth,
      document: document.documentElement.scrollWidth
    }));
    assert(
      dimensions.document <= dimensions.viewport,
      `${label} overflows horizontally: ${JSON.stringify(dimensions)}`
    );
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
  const waitForSnapshot = async (predicate, label) => {
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      const value = await snapshot();
      if (predicate(value)) {
        return value;
      }
      await page.waitForTimeout(50);
    }
    throw new Error(`timed out waiting for ${label}`);
  };
  const setSharedAuth = async (authenticated) => {
    await page.evaluate((nextAuthenticated) => {
      const header = document.querySelector('#app-header');
      if (
        !window.MPRUI ||
        !window.MPRUI.testing ||
        typeof window.MPRUI.testing.authenticate !== 'function' ||
        typeof window.MPRUI.testing.unauthenticate !== 'function'
      ) {
        throw new Error('mpr-ui browser test lifecycle is unavailable');
      }
      if (nextAuthenticated) {
        window.MPRUI.testing.authenticate(header, {
          user_id: 'browser-smoke-user',
          user_email: 'browser-contract@example.invalid',
          user_display_name: 'Browser Contract',
          user_avatar_url: 'https://lh3.googleusercontent.com/a/browser-contract',
          display: 'Browser Contract',
          avatar_url: 'https://lh3.googleusercontent.com/a/browser-contract'
        });
      } else {
        window.MPRUI.testing.unauthenticate(header);
      }
    }, authenticated);
  };

  await page.setViewportSize({width: 1440, height: 1000});
  await page.goto(baseURL, {waitUntil: 'networkidle'});
  await page.waitForFunction(
    () =>
      customElements.get('mpr-header') &&
      customElements.get('mpr-footer') &&
      window.MPRUI?.testing
  );
  await page.locator('.catalog').waitFor();
  await setSharedAuth(false);

  assert(
    await page.locator('.provider-card[data-provider-id]').count() === 11,
    'anonymous provider catalog must contain eleven canonical providers'
  );
  assert(
    await page.locator('mpr-header header[role="banner"]').count() === 1 &&
      await page.locator('mpr-footer footer[role="contentinfo"]').count() === 1 &&
      await page.locator('mpr-user').count() === 1,
    'mpr-ui must own one header, footer, and account surface'
  );
  assert(
    protectedRequests().length === 0,
    `public catalog made protected requests: ${protectedRequests().join(', ')}`
  );

  await page.setViewportSize({width: 390, height: 844});
  for (const providerID of [
    'netflix',
    'openai',
    'facebook',
    'instagram',
    'whatsapp',
    'threads',
    'linkedin',
    'tiktok',
    'x',
    'youtube',
    'google'
  ]) {
    await route(`#guide/${providerID}`, `#${providerID}`);
    assert(
      await page.locator(`#${providerID} .instruction-step`).count() > 0,
      `${providerID} guide must render instructions anonymously`
    );
    await assertNoHorizontalOverflow(`${providerID} anonymous guide`);
  }

  await route('#guide/netflix', '#netflix');
  assert(
    await page.locator('#netflix .instruction-step').count() === 6 &&
      await page.locator(
        '#netflix .guide-refs a[href="https://help.netflix.com/en/node/101917"]'
      ).count() === 1,
    'Netflix guide must remain complete and public'
  );
  await route('#guide/openai', '#openai');
  assert(
    await page.locator('#openai .instruction-step').count() === 7 &&
      await page.locator(
        '#openai .guide-refs a[href="https://help.openai.com/en/articles/7260999-how-do-i-export-my-chatgpt-history-and-data"]'
      ).count() === 1,
    'OpenAI guide must remain complete and public'
  );
  await route('#credits', '.credits');
  assert(
    (await page.locator('.tmdb-credit').textContent()).includes(
      'This product uses the TMDB API but is not endorsed or certified by TMDB.'
    ),
    'public Credits must render its static TMDB attribution'
  );
  assert(
    protectedRequests().length === 0,
    `public guides or Credits made protected requests: ${protectedRequests().join(', ')}`
  );

  await page.setViewportSize({width: 1440, height: 1000});
  await route('#provider/netflix', '.catalog');
  assert(
    await page.evaluate(() => window.location.hash) === '#provider/netflix',
    'obsolete provider route must not be rewritten into a compatibility alias'
  );
  await route('#app/netflix', '.workspace-gate');
  assert(
    ['pending', 'unauthenticated'].includes(
      await page.locator('.workspace-gate-panel').getAttribute('data-auth-state')
    ) &&
      protectedRequests().length === 0,
    'unsettled or signed-out application route made a protected request'
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
    window.__downloadYourDataReadyEvents = 0;
    document.addEventListener('download-your-data:app-ready', () => {
      window.__downloadYourDataReadyEvents += 1;
    });
  });
  await setSharedAuth(true);
  await page.locator('.workspace').waitFor();
  await page.waitForFunction(() => window.__downloadYourDataReadyEvents === 1);
  assert(
    await page.locator('.workspace h1').textContent() === 'Netflix',
    'authenticated lifecycle did not hydrate the Netflix workspace'
  );
  assert(
    protectedRequests().some((rawURL) => rawURL === `${baseURL}/api/capabilities`) &&
      protectedRequests().some(
        (rawURL) => rawURL === `${baseURL}/api/providers/netflix`
      ),
    'authenticated lifecycle did not make the required protected requests'
  );

  await page.locator('#netflix-file').setInputFiles(validCSV);
  const readySnapshot = await waitForSnapshot(
    (value) => value.active_generation?.state === 'ready',
    'ready Netflix generation'
  );
  assert(
    readySnapshot.active_generation.analysis_level === 'local',
    'Netflix import did not produce the private base analysis generation'
  );
  await page.getByRole('tab', {name: 'Overview'}).waitFor();
  await page.waitForFunction(() => document.querySelectorAll('.kpi').length === 4);
  await page.setViewportSize({width: 390, height: 844});
  await assertNoHorizontalOverflow('authenticated Netflix workspace');
  await page.setViewportSize({width: 1440, height: 1000});

  await setSharedAuth(false);
  await page.locator(
    '.workspace-gate-panel[data-auth-state="unauthenticated"]'
  ).waitFor();
  assert(
    await page.locator('.workspace, .kpi, #netflix-file').count() === 0,
    'shared unauthenticated lifecycle did not clear protected workspace UI'
  );
  const requestCountAfterLogout = protectedRequests().length;
  await page.waitForTimeout(500);
  assert(
    protectedRequests().length === requestCountAfterLogout,
    'protected polling continued after shared logout'
  );

  await route('#catalog', '.catalog');
  await route('#guide/google', '#google');
  assert(
    await page.locator('#google .instruction-step').count() === 7,
    'public guides must remain usable after logout'
  );

  await route(
    '#app/openai',
    '.workspace-gate-panel[data-auth-state="unauthenticated"]'
  );
  await setSharedAuth(true);
  await page.locator('.openai-workspace').waitFor();
  assert(
    await page.locator('.openai-prepare').count() === 1 &&
      await page.locator('.openai-command').count() === 0,
    'OpenAI workspace must not expose the retired unscoped operator commands'
  );

  const unexpectedExternalRequests = requestURLs.filter((rawURL) => {
    if (rawURL.startsWith('about:') || rawURL.startsWith('data:')) {
      return false;
    }
    return (
      !rawURL.startsWith(`${baseURL}/`) &&
      rawURL !== baseURL &&
      !rawURL.startsWith('https://accounts.google.com/') &&
      !rawURL.startsWith('https://cdn.jsdelivr.net/') &&
      !rawURL.startsWith('https://lh3.googleusercontent.com/')
    );
  });
  assert(
    unexpectedExternalRequests.length === 0,
    `browser made unexpected external requests: ${unexpectedExternalRequests.join(', ')}`
  );
  assert(
    requestURLs.includes(
      'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui.css'
    ) &&
      requestURLs.includes(
        'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui-config.js'
      ) &&
      requestURLs.includes(
        'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui.js'
      ),
    'browser did not load the complete mpr-ui@latest bootstrap'
  );
  assert(browserErrors.length === 0, `browser errors: ${browserErrors.join(' | ')}`);
}
