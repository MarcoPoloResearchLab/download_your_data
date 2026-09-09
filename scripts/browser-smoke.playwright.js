async page => {
  const baseURL = '__BASE_URL__';
  const validCSV = '__VALID_CSV__';
  const sessionCookie = '__SESSION_COOKIE__';
  const sessionToken = '__SESSION_TOKEN__';
  const loopAwareSiteID = '5a6e13d5-7584-451e-b058-36b9ecef8e8d';
  const loopAwarePixelURL =
    `https://loopaware.mprlab.com/pixel.js?site_id=${loopAwareSiteID}`;
  const loopAwareVisitURLPrefix =
    `https://loopaware-api.mprlab.com/public/visits?site_id=${loopAwareSiteID}&`;
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
  const requestedAsset = (assetURL) =>
    requestURLs.some(
      (rawURL) => rawURL === assetURL || rawURL.startsWith(`${assetURL}?`)
    );
  const isCurrentLoopAwareRequest = (rawURL) =>
    rawURL === loopAwarePixelURL || rawURL.startsWith(loopAwareVisitURLPrefix);
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
      document: document.documentElement.scrollWidth,
      overflow: Array.from(document.querySelectorAll('body *')).filter(node => node.getBoundingClientRect().right > innerWidth && getComputedStyle(node).visibility !== 'hidden').slice(0,8).map(node=>({tag:node.tagName,classes:node.className,right:node.getBoundingClientRect().right}))
    }));
    assert(
      dimensions.document <= dimensions.viewport,
      `${label} overflows horizontally: ${JSON.stringify(dimensions)}`
    );
  };
  const assertInstructionImages = async (providerID) => {
    const screenshotIDs = await page
      .locator(`#${providerID} .instruction-screenshot`)
      .evaluateAll((images) => [
        ...new Set(images.map((image) => image.getAttribute('data-screenshot-id')))
      ]);
    for (const screenshotID of screenshotIDs) {
      const screenshot = page
        .locator(`#${providerID} .instruction-screenshot[data-screenshot-id="${screenshotID}"]`)
        .first();
      await screenshot.scrollIntoViewIfNeeded();
      const pixels = await screenshot.evaluate(async (image) => {
        await image.decode();
        const canvas = document.createElement('canvas');
        canvas.width = 64;
        canvas.height = 64;
        const context = canvas.getContext('2d', {willReadFrequently: true});
        if (!context) {
          throw new Error('instruction screenshot canvas context is unavailable');
        }
        context.drawImage(image, 0, 0, canvas.width, canvas.height);
        const rgba = context.getImageData(0, 0, canvas.width, canvas.height).data;
        const colors = new Map();
        let dominantPixels = 0;
        for (let offset = 0; offset < rgba.length; offset += 4) {
          const color =
            ((rgba[offset] >> 4) << 8) |
            ((rgba[offset + 1] >> 4) << 4) |
            (rgba[offset + 2] >> 4);
          const count = (colors.get(color) || 0) + 1;
          colors.set(color, count);
          dominantPixels = Math.max(dominantPixels, count);
        }
        return {
          naturalWidth: image.naturalWidth,
          naturalHeight: image.naturalHeight,
          distinctColors: colors.size,
          dominantPixels,
          totalPixels: canvas.width * canvas.height
        };
      });
      assert(
        pixels.naturalWidth > 0 &&
          pixels.naturalHeight > 0 &&
          pixels.distinctColors >= 8 &&
          pixels.dominantPixels * 1000 <= pixels.totalPixels * 995,
        `${providerID} screenshot ${screenshotID} lacks decoded visible content: ${JSON.stringify(pixels)}`
      );
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
  await page.goto(baseURL, {waitUntil: 'domcontentloaded'});
  await page.waitForFunction(
    () =>
      customElements.get('mpr-header') &&
      customElements.get('mpr-footer') &&
      window.MPRUI?.testing
  );
  await page.locator('.catalog').waitFor();
  await setSharedAuth(false);

  assert(
    await page.locator('.provider-card[data-provider-id]').count() === 12,
    'anonymous provider catalog must contain twelve canonical providers'
  );
  assert(
    await page.locator('.catalog .page-heading h1').textContent() ===
      'Private workspace' &&
      await page.locator('.catalog .page-heading .lede').count() === 0,
    'provider catalog must use Private workspace as its sole page heading'
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
    'google',
    'amazon'
  ]) {
    await route(`#guide/${providerID}`, `#${providerID}`);
    assert(
      await page.locator(`#${providerID} .instruction-step`).count() > 0,
      `${providerID} guide must render instructions anonymously`
    );
    await assertInstructionImages(providerID);
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
  await route('#guide/amazon', '#amazon');
  assert(
    await page.locator('#amazon .instruction-step').count() === 6 &&
      await page.locator(
        '#amazon .guide-refs a[href="https://www.amazon.com/hz/privacy-central/data-requests/preview.html"]'
      ).count() === 1,
    'Amazon guide must remain complete and public'
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

  const protectedRequestCountBeforeResources = protectedRequests().length;
  await page.setViewportSize({width: 1440, height: 1000});
  await page.goto(`${baseURL}/resources/`, {waitUntil: 'domcontentloaded'});
  assert(
    await page.locator('.resource-card').count() === 13,
    'resource hub must expose thirteen current crawlable resources'
  );
  assert(
    await page.locator('link[rel="canonical"]').getAttribute('href') ===
      `${baseURL}/resources/`,
    'resource hub canonical does not use the final trailing-slash URL'
  );
  await page.evaluate(() => {
    const structuredData = document.querySelector('#structured-data')?.textContent;
    if (!structuredData) {
      throw new Error('resource hub structured data is missing');
    }
    JSON.parse(structuredData);
  });
  for (const resourcePath of [
    '/resources/netflix-viewing-history-csv/',
    '/resources/netflix-viewing-history-analyzer/',
    '/resources/chatgpt-data-export/',
    '/resources/whatsapp-chat-export/'
  ]) {
    await page.setViewportSize({width: 390, height: 844});
    await page.goto(`${baseURL}${resourcePath}`, {waitUntil: 'domcontentloaded'});
    assert(
      await page.locator('h1').count() === 1 &&
        await page.locator('#quick-verdict-title').count() === 1 &&
        await page.locator('pre code').count() === 1 &&
        await page.locator('details').count() >= 3 &&
        await page.locator('a[rel~="author"]').count() === 1,
      `${resourcePath} is missing required public resource depth`
    );
    assert(
      await page.locator('link[rel="canonical"]').getAttribute('href') ===
        `${baseURL}${resourcePath}`,
      `${resourcePath} canonical does not match the final served URL`
    );
    assert(
      await page.locator('img[loading="lazy"]').evaluateAll((images) =>
        images.every(
          (image) =>
            Number(image.getAttribute('width')) > 0 &&
            Number(image.getAttribute('height')) > 0
        )
      ),
      `${resourcePath} has a lazy image without explicit dimensions`
    );
    await assertNoHorizontalOverflow(resourcePath);
  }
  assert(
    protectedRequests().length === protectedRequestCountBeforeResources,
    `public resources made protected requests: ${protectedRequests().join(', ')}`
  );

  await page.setViewportSize({width: 1440, height: 1000});
  await page.goto(baseURL, {waitUntil: 'domcontentloaded'});
  await page.waitForFunction(
    () =>
      customElements.get('mpr-header') &&
      customElements.get('mpr-footer') &&
      window.MPRUI?.testing
  );
  await page.locator('.catalog').waitFor();
  await setSharedAuth(false);

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
      !isCurrentLoopAwareRequest(rawURL) &&
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
    requestedAsset(loopAwarePixelURL) &&
      requestURLs.some((rawURL) => rawURL.startsWith(loopAwareVisitURLPrefix)),
    'browser did not send the current LoopAware telemetry'
  );
  assert(
    requestedAsset(
      'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui.css'
    ) &&
      requestedAsset(
        'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui-config.js'
      ) &&
      requestedAsset(
        'https://cdn.jsdelivr.net/gh/MarcoPoloResearchLab/mpr-ui@latest/mpr-ui.js'
      ),
    'browser did not load the complete mpr-ui@latest bootstrap'
  );
  assert(browserErrors.length === 0, `browser errors: ${browserErrors.join(' | ')}`);
}
