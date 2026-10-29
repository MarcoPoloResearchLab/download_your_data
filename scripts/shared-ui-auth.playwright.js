async page => {
  const baseURL = '__BASE_URL__';
  const apiURL = '__API_URL__';
  const sessionCookie = '__SESSION_COOKIE__';
  const sessionToken = '__SESSION_TOKEN__';
  const profile = {user_id: 'browser-netflix-user', user_email: 'browser@example.invalid', display: 'Browser Contract'};
  const errors = [];
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', message => { if(message.type() === 'error' && !message.text().startsWith('Failed to load resource:')) errors.push(message.text()); });
  await page.route('https://loopaware.mprlab.com/**', route => route.fulfill({status:200,body:''}));
  let authenticated = false;
  let recoveryRequests = 0;
  let replayPending = false;
  let protectedAttempts = 0;
  let googleExchanges = 0;
  await page.route(`${apiURL}/auth/**`, async route => {
    const request = route.request();
    const path = request.url().slice(apiURL.length).split('?')[0];
    assert(request.headers()['x-tauth-tenant'] === 'download-your-data-test', 'auth tenant is preserved');
    if (path === '/auth/nonce') return route.fulfill({json:{nonce:'fixture-nonce'}});
    if (path === '/auth/google') {
      const payload = request.postDataJSON();
      assert(payload.google_id_token === 'fixture-credential' && payload.nonce_token === 'fixture-nonce', 'Google exchange contract');
      googleExchanges += 1;
      authenticated = true;
      await page.context().addCookies([{name:sessionCookie,value:sessionToken,url:apiURL,httpOnly:true,sameSite:'Lax'}]);
      return route.fulfill({json:profile});
    }
    if (path === '/auth/session') {
      recoveryRequests += 1;
      return route.fulfill({status:authenticated?200:401,json:authenticated?profile:{error:'session_required'}});
    }
    if (path === '/auth/logout') {
      authenticated = false;
      await page.context().clearCookies();
      return route.fulfill({status:204,body:''});
    }
    throw new Error(`unexpected auth route ${path}`);
  });
  await page.route(`${apiURL}/api/capabilities`, route => {
    protectedAttempts += 1;
    if (replayPending) {
      replayPending = false;
      return route.fulfill({status:401,json:{error:{code:'session_required'}}});
    }
    return route.continue();
  });
  let mutationPending = false;
  let mutationAttempts = 0;
  await page.route(`${apiURL}/api/providers/netflix/generations`, route => {
    if (route.request().method() === 'POST') {
      mutationAttempts += 1;
      if (mutationPending) {
        mutationPending = false;
        return route.fulfill({status:401,json:{error:{code:'session_required'}}});
      }
    }
    return route.continue();
  });
  const assertHeaderAccountOrder = async (accountSelector) => {
    const languages = page.locator('#app-header #language-switcher');
    const account = page.locator(accountSelector);
    assert(await languages.isVisible() && await account.isVisible(), 'header keeps language and account controls visible');
    const languageBounds = await languages.boundingBox();
    const accountBounds = await account.boundingBox();
    assert(accountBounds.x >= languageBounds.x + languageBounds.width, `login/account must sit to the right of language controls: ${JSON.stringify({viewport:page.viewportSize(),languageBounds,accountBounds})}`);
    assert(Math.abs(accountBounds.y + accountBounds.height / 2 - languageBounds.y - languageBounds.height / 2) < 2, 'language and login/account share one header row');
    const bounds = await page.evaluate(() => ({width:innerWidth,document:document.documentElement.scrollWidth}));
    assert(bounds.document <= bounds.width, `header viewport overflow: ${JSON.stringify(bounds)}`);
    assert(await languages.evaluate((node, selector) => Boolean(node.compareDocumentPosition(document.querySelector(selector)) & Node.DOCUMENT_POSITION_FOLLOWING), accountSelector), 'keyboard order follows language then login/account');
    assert(await page.locator('#app-header #theme-toggle').count() === 0, 'header has no theme switcher');
    assert(await page.locator('#app-footer #theme-toggle').count() === 1, 'footer owns the theme switcher');
  };
  const viewportWidths = [320, 390, 768, 769, 1280];
  for (const width of viewportWidths) {
    await page.setViewportSize({width,height:900});
    await page.goto(`${baseURL}/#app/viewing-history`, {waitUntil:'domcontentloaded'});
    await page.locator('[data-fixture-google]').waitFor().catch(async error => { throw new Error(error.message + JSON.stringify({errors, state:await page.evaluate(() => ({auth:document.querySelector('#app-header')?.getAttribute('auth-config'), google:!!window.google, mpr:!!window.MPRUI}))})); });
    assert(await page.locator('.workspace-gate').count() === 1, 'anonymous workspace stays gated');
    assert(!(await page.locator('#credits-button').isVisible()), 'signed-out header hides Credits');
    await assertHeaderAccountOrder('[data-fixture-google]');
    const themeBefore = await page.locator('html').getAttribute('data-theme');
    await page.locator('#app-footer #theme-toggle').click();
    const themeAfter = await page.locator('html').getAttribute('data-theme');
    assert(themeAfter !== themeBefore && await page.locator('html').getAttribute('data-mpr-theme') === themeAfter, 'footer toggles both theme attributes');
    assert(await page.evaluate(() => localStorage.getItem('download-your-data.theme')) === themeAfter, 'footer theme persists');
    await page.locator('[data-language="fr"]').click();
    assert(await page.locator('html').getAttribute('lang') === 'fr', 'header language control still changes locale');
    await page.locator('[data-language="en"]').click();
    const auth = JSON.parse(await page.locator('#app-header').getAttribute('auth-config'));
    assert(auth.providers.google.clientId === 'test.apps.googleusercontent.com' && auth.sessionPath === '/auth/session', 'generated provider map');
    replayPending = true;
    const attemptsBefore = protectedAttempts;
    const sessionsBefore = recoveryRequests;
    await page.locator('[data-fixture-google]').click();
    await page.locator('.media-workspace').waitFor();
    await assertHeaderAccountOrder('mpr-user [data-mpr-user="trigger"]');
    assert(await page.locator('#credits-button').getAttribute('hidden') === null, 'authenticated state reveals Credits');
    assert(protectedAttempts - attemptsBefore === 2, 'protected read retries once after recovery');
    assert(recoveryRequests > sessionsBefore, 'shared session endpoint performs recovery');
    mutationPending = true;
    const mutationsBefore = mutationAttempts;
    const mutationRecoveryBefore = recoveryRequests;
    const generationID = await page.evaluate(async () => {
      const api = await import('/application/api.js');
      const generation = await api.createLocalGeneration(new AbortController().signal);
      await api.cancelGeneration(generation.id, new AbortController().signal);
      return generation.id;
    });
    assert(generationID.startsWith('ng_'), 'real API creates the generation');
    assert(mutationAttempts - mutationsBefore === 2, 'mutation retries once after authorization recovery');
    assert(recoveryRequests > mutationRecoveryBefore, 'mutation uses shared session recovery');
    await page.reload({waitUntil:'domcontentloaded'});
    await page.locator('.media-workspace').waitFor();
    assert(await page.locator('html').getAttribute('data-theme') === themeAfter, 'footer theme survives reload');
    const bounds = await page.evaluate(() => ({width:innerWidth,document:document.documentElement.scrollWidth}));
    assert(bounds.document <= bounds.width, `viewport overflow ${width}`);
    assert(await page.locator('mpr-footer a[href="/resources/"]').count() === 1, 'footer keeps resource link');
    if (width === 1280) {
      assert(await page.locator('#credits-button').isVisible(), 'authenticated desktop header shows Credits');
      await page.locator('#credits-button').click();
      await page.locator('.credits').waitFor();
    }
    await page.locator('mpr-user [data-mpr-user="trigger"]').click();
    await page.locator('mpr-user [data-mpr-user="logout"]').click();
    await page.locator('[data-fixture-google]').waitFor({state:'visible'});
    assert(!authenticated, 'logout clears the external session');
    assert(!(await page.locator('#credits-button').isVisible()), 'logout hides Credits');
    await assertHeaderAccountOrder('[data-fixture-google]');
    assert(await page.locator('.media-workspace').count() === 0, 'logout clears private workspace');
  }
  assert(googleExchanges === viewportWidths.length, 'all viewport flows exchange a Google credential');
  assert(errors.length === 0, `browser errors: ${errors.join('; ')}`);
}
