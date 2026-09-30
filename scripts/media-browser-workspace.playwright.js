async page => {
  const baseURL='__BASE_URL__';
  const assert=(condition,message)=>{if(!condition)throw new Error(message);};
  const errors=[];page.on('pageerror',error=>errors.push(error.message));
  await page.context().addCookies([{name:'__SESSION_COOKIE__',value:'__SESSION_TOKEN__',url:baseURL,httpOnly:true,sameSite:'Lax'}]);
  await page.setViewportSize({width:1440,height:1000});
  await page.goto(`${baseURL}/#app/viewing-history`,{waitUntil:'domcontentloaded'});
  await page.waitForFunction(()=>window.MPRUI?.testing);
  await page.evaluate(()=>window.MPRUI.testing.authenticate(document.querySelector('#app-header'),{user_id:'browser-media-user',user_email:'browser@example.invalid',user_display_name:'Media contract',user_avatar_url:'https://lh3.googleusercontent.com/a/browser-contract',display:'Media contract',avatar_url:'https://lh3.googleusercontent.com/a/browser-contract'}));
  await page.locator('.media-workspace').waitFor();
  await page.locator('[data-media-action="add-files"]').click();
  await page.locator('#media-files').setInputFiles('__VIEWING_CSV__');
  await page.waitForFunction(()=>document.querySelector('[data-media-chart="genres"] .bar-row'));
  assert(await page.locator('dialog[open]').count()===0,'automatic import opened a permission dialog');
  await page.locator('[data-media-action="add-files"]').click();
  await page.locator('#media-files').setInputFiles('__PRIME_ZIP__');
  await page.waitForFunction(()=>document.querySelector('[data-media-kpi="activities"]')?.textContent==='2');
  await page.waitForFunction(()=>document.querySelector('[data-media-chart="media-types"]')?.textContent.includes('2'));
  await page.locator('.media-processing').waitFor({state:'detached'});
  const names=['monthly','media-types','genres','original-languages','weekday-genres','year-genres'];
  for(const name of names)assert(await page.locator(`[data-media-chart="${name}"]`).isVisible(),`missing ${name} chart`);
  assert(await page.locator('[data-media-view]').count()===2,'redundant report tabs remain');
  assert(await page.locator('.media-workspace').innerText().then(text=>!/tmdb|consent|permission|enrich/i.test(text)),'technical setup is visible');
  assert(await page.locator('[data-media-action="apply-filters"]').count()===0,'manual filter action remains');
  await page.locator('[data-media-provider="netflix"]').click();
  await page.waitForFunction(()=>document.querySelector('[data-media-kpi="activities"]')?.textContent==='1');
  assert(!await page.locator('[data-media-kpi="watch-time"]').isVisible(),'Netflix time falsely uses metadata runtime');
  await page.locator('[data-media-provider="all"]').click();
  await page.locator('#media-title-filter').fill('No matching title');
  await page.waitForFunction(()=>document.querySelector('[data-media-kpi="activities"]')?.textContent==='0');
  for(const name of names)assert(await page.locator(`[data-media-chart="${name}"] .empty-copy`).isVisible(),`${name} has no empty state`);
  await page.locator('#media-title-filter').fill('');
  await page.waitForFunction(()=>document.querySelector('[data-media-kpi="activities"]')?.textContent==='2');
  const downloadPromise=page.waitForEvent('download');await page.locator('[data-media-action="export"]').click();
  const download=await downloadPromise;const stream=await download.createReadStream();let csv='';for await(const chunk of stream)csv+=chunk.toString('utf8');
  assert(csv.includes('netflix,activity')&&csv.includes('prime-video,playback'),'CSV lost a service');
  const chooseView=async view=>{await page.locator(`[data-media-view="${view}"]`).click();await page.locator(`[data-media-panel="${view}"]`).waitFor();};
  await chooseView('history');assert(await page.locator('.media-records tbody tr').count()>=2&&!await page.locator('.media-records').innerText().then(text=>text.includes('Private search words')),'automatic import includes unselected datasets');
  await chooseView('overview');
  for(const locale of ['es','fr','ru','en']){await page.locator(`[data-language="${locale}"]`).click();await page.waitForFunction(locale=>document.documentElement.lang===locale,locale);assert(!await page.locator('.media-workspace').innerText().then(text=>text.includes('undefined')),'missing localized text');}
  for(const width of [1440,736,360,320]){await page.setViewportSize({width,height:900});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth),`page overflows at ${width}`);for(const name of names)assert(await page.locator(`[data-media-chart="${name}"]`).isVisible(),`${name} hidden at ${width}`);}
  await page.screenshot({path:'__SCREENSHOT_ROOT__/f024-narrow.png',fullPage:true});await page.setViewportSize({width:1440,height:1000});await page.screenshot({path:'__SCREENSHOT_ROOT__/f024-wide.png',fullPage:true});
  assert(await page.evaluate(()=>![...Object.values(localStorage),...Object.values(sessionStorage)].some(value=>value.includes('Synthetic Film'))),'private history persisted in browser storage');
  assert(errors.length===0,errors.join('; '));
}
