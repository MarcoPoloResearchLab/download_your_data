async page => {
  const baseURL = '__BASE_URL__';
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const waitActivities = async expected => {
    try { await page.waitForFunction(expected => document.querySelector('[data-media-kpi="activities"]')?.textContent === String(expected), expected); }
    catch (failure) { throw new Error(`activity total ${expected}: ${await page.locator('body').innerText()}`); }
  };
  const errors = [];
  const apiStatuses = [];
  let phase = 'bootstrap';
  try {
  page.on('response',response => { if (response.url().startsWith(`${baseURL}/api/`)) apiStatuses.push(`${response.status()} ${response.url()}`); });
  page.on('pageerror', error => errors.push(error.message));
  await page.setViewportSize({width: 1440, height: 1000});
  await page.goto(`${baseURL}/#app/viewing-history`, {waitUntil: 'domcontentloaded'});
  await page.waitForFunction(() => window.MPRUI?.testing);
  try { await page.locator('[data-auth-state]').waitFor(); }
  catch (failure) { throw new Error(`authentication gate: ${errors.join('; ')}; ${await page.locator('body').innerText()}`); }
  assert(await page.locator('[data-auth-state]').getAttribute('data-auth-state') !== 'authenticated', 'shared workspace needs its authentication gate');
  assert(apiStatuses.length===0,'private data requested before authentication');
  await page.context().addCookies([{name: '__SESSION_COOKIE__', value: '__SESSION_TOKEN__', url: baseURL, httpOnly: true, sameSite: 'Lax'}]);
  await page.evaluate(() => window.MPRUI.testing.authenticate(document.querySelector('#app-header'), {user_id:'browser-media-user', user_email:'browser@example.invalid', user_display_name:'Media contract', user_avatar_url:'https://lh3.googleusercontent.com/a/browser-contract', display:'Media contract', avatar_url:'https://lh3.googleusercontent.com/a/browser-contract'}));
  try { await page.locator('.media-workspace').waitFor(); }
  catch (failure) { throw new Error(`workspace hydration: ${errors.join('; ')}; ${apiStatuses.join('; ')}; ${await page.locator('body').innerText()}`); }
  await page.locator('#media-netflix-file').setInputFiles('__VIEWING_CSV__');
  await page.locator('#media-netflix-label').fill('Synthetic profile');
  await page.locator('[data-media-action="import-netflix"]').click();
  await waitActivities(1);
  assert(await page.locator('[data-media-panel="overview"] .panel-copy').evaluateAll(nodes=>nodes.some(node=>node.textContent.startsWith('Prime Video:'))),'duration exclusions are not scoped to Prime Video');
  await page.locator('#media-prime-file').setInputFiles('__PRIME_ZIP__');
  await page.locator('[data-media-action="preview-prime"]').click();
  await page.locator('.media-preview').waitFor();
  assert(await page.locator('.media-dataset').count() === 6, 'dataset preview missing');
  for (const dataset of ['watch_events','searches','purchases','trailers']) await page.locator(`input[name="dataset"][value="${dataset}"]`).check();
  await page.locator('[data-media-action="confirm-prime"]').click();
  await waitActivities(2);
  const chooseView = async view => { await page.locator(`[data-media-view="${view}"]`).click(); await page.locator(`[data-media-panel="${view}"]`).waitFor(); };
  await chooseView('titles');
  phase = 'enrichment consent';
  const enrichmentRequests = [];
  page.on('request',request => { if (request.method()==='POST' && request.url().endsWith('/generations') && request.postData()?.includes('"tmdb"')) enrichmentRequests.push(request.postData()); });
  await page.locator('[data-media-action="enrich-prime"]').click();
  await page.locator('dialog[open]').waitFor();
  await page.keyboard.press('Escape');
  assert(enrichmentRequests.length===0, 'dismissed consent sent a title query request');
  assert(await page.locator('[data-media-action="enrich-prime"]').evaluate(node => node===document.activeElement), 'consent did not restore keyboard focus');
  for (const provider of ['netflix','prime']) {
    await page.locator(`[data-media-action="enrich-${provider}"]`).click();
    await page.locator('[data-media-confirm="true"]').click();
    await page.waitForFunction(provider => document.querySelector('[data-media-title="tmdb:movie:2001"]')?.closest('tr').textContent.includes(provider), provider==='netflix'?'Netflix':'Prime Video');
  }
  assert(enrichmentRequests.length===2 && enrichmentRequests.every(body=>body.includes('authorize-tmdb-title-queries')), 'enrichment consent contract');
  await chooseView('titles');
  await page.locator('[data-media-title="tmdb:movie:2001"]').waitFor();
  assert((await page.locator('.media-titles > tbody > tr').first().textContent()).includes('Netflix, Prime Video'), 'accepted title does not join both providers');
  await page.locator('[data-media-title="tmdb:movie:2001"]').click();
  await page.waitForFunction(()=>document.querySelectorAll('.media-records > tbody > tr').length===5);
  assert(!(await page.locator('.media-records').textContent()).includes('Private search words'), 'title identity filter included a search');
  await page.locator('[data-media-action="clear-filters"]').click();
  await chooseView('activity');
  assert(await page.locator('.media-records > tbody > tr').count() >= 2, 'combined activity rows missing');
  assert((await page.locator('.media-records').textContent()).includes('Synthetic profile'), 'Netflix import label missing');
  await page.locator('#media-provider').selectOption('prime-video');
  phase = 'Prime provider filter';
  await page.locator('[data-media-action="apply-filters"]').click();
  await page.waitForFunction(() => [...document.querySelectorAll('.media-records > tbody > tr')].every(row => row.textContent.includes('Prime Video')));
  await page.locator('#media-provider').selectOption('all');
  await page.locator('#media-timezone').fill('America/Los_Angeles');
  await page.locator('[data-media-action="apply-filters"]').click();
  await chooseView('overview');
  const downloadPromise = page.waitForEvent('download');
  await page.locator('[data-media-action="export"]').click();
  const download = await downloadPromise;
  assert(download.suggestedFilename() === 'viewing-history.csv', 'combined export filename');
  const stream = await download.createReadStream();
  let exportText = '';
  for await (const chunk of stream) exportText += chunk.toString('utf8');
  assert(exportText.includes('netflix,activity') && exportText.includes('prime-video,playback'), 'combined export omitted a provider');
  assert(exportText.includes('display_timezone') && exportText.includes('America/Los_Angeles'),'download lost display timezone');
  assert(await page.evaluate(()=>![...Object.values(localStorage),...Object.values(sessionStorage)].some(value=>value.includes('Synthetic Film') || value.includes('Private search words'))),'private rows persisted in browser storage');
  await page.screenshot({path:'__SCREENSHOT_ROOT__/f023-wide.png',fullPage:true});
  await chooseView('sources');
  assert((await page.locator('[data-media-panel="sources"]').textContent()).includes('Prime Video'), 'source evidence missing');
  for (const locale of ['es','fr','ru','en']) {
    phase = `locale ${locale}`;
    await page.locator(`[data-language="${locale}"]`).click();
    await page.waitForFunction(locale => document.documentElement.lang === locale, locale);
    await page.locator('.media-workspace').waitFor();
    assert(!(await page.locator('.media-workspace').textContent()).includes('undefined'), 'locale copy missing');
  }
  await page.setViewportSize({width: 360, height: 800});
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), 'narrow viewport overflows');
  await chooseView('titles');
  await page.locator('[data-media-view="titles"]').focus();
  await page.keyboard.press('ArrowRight');
  await page.locator('[data-media-panel="activity"]').waitFor();
  assert(await page.locator('[data-media-view="activity"]').evaluate(node=>node===document.activeElement),'tabs lost keyboard focus');
  assert(await page.locator('.media-records').evaluate(table=>table.scrollWidth>table.parentElement.clientWidth),'narrow activity table compressed its columns instead of scrolling');
  await page.locator('.media-records').locator('..').focus();
  await page.keyboard.press('ArrowRight');
  await page.waitForFunction(()=>document.querySelector('.media-records').parentElement.scrollLeft>0);
  assert(await page.locator('mpr-header').count() === 1 && await page.locator('mpr-footer').count() === 1, 'shared shell missing');
  await page.screenshot({path:'__SCREENSHOT_ROOT__/f023-narrow.png',fullPage:true});
  phase = 'independent cancellation and deletion';
  await page.locator('#media-prime-file').setInputFiles('__PRIME_ZIP__');
  await page.locator('[data-media-action="preview-prime"]').click();
  await page.locator('.media-preview').waitFor();
  await page.locator('[data-media-action="cancel-prime"]').click();
  await page.locator('.media-preview').waitFor({state:'detached'});
  await page.locator('[data-media-action="delete-prime"]').click();
  await page.locator('[data-media-confirm="true"]').click();
  await chooseView('overview');
  await waitActivities(1);
  phase = 'route cleanup';
  await page.locator('[data-media-action="enrich-netflix"]').click();
  await page.locator('dialog[open]').waitFor();
  await page.evaluate(()=>{window.location.hash='#catalog';});
  await page.locator('.catalog-grid').waitFor();
  assert(await page.locator('dialog').count()===0, 'route change retained a private consent dialog');
  phase = 'response boundary';
  await page.route(`${baseURL}/api/viewing-history?*`,async route=>{
    const response = await route.fetch();
    const payload = await response.json();
    payload.overview.services[0].provider='unsupported-service';
    await route.fulfill({response,json:payload});
  });
  await page.evaluate(()=>{window.location.hash='#app/viewing-history';});
  await page.locator('[data-workspace-state="error"]').waitFor();
  assert(await page.locator('.media-workspace').count()===0,'invalid provider response reached rendering');
  assert(errors.length === 0, `browser errors: ${errors.join('; ')}`);
  } catch (failure) {
    throw new Error(`${phase}: ${failure.message}; ${apiStatuses.join('; ')}; ${await page.locator('body').innerText()}`);
  }
}
