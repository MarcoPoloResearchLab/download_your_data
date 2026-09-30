async page => {
  const baseURL='__BASE_URL__';
  const assert=(condition,message)=>{if(!condition)throw new Error(message);};
  const errors=[];page.on('pageerror',error=>errors.push(error.message));
  await page.context().addCookies([{name:'__SESSION_COOKIE__',value:'__SESSION_TOKEN__',url:baseURL,httpOnly:true,sameSite:'Lax'}]);
  await page.setViewportSize({width:1280,height:1000});
  await page.goto(`${baseURL}/#app/viewing-history`,{waitUntil:'domcontentloaded'});
  await page.waitForFunction(()=>window.MPRUI?.testing);
  await page.evaluate(()=>window.MPRUI.testing.authenticate(document.querySelector('#app-header'),{user_id:'browser-netflix-user',user_email:'browser@example.invalid',user_display_name:'Browser Contract',user_avatar_url:'https://lh3.googleusercontent.com/a/browser-contract',display:'Browser Contract',avatar_url:'https://lh3.googleusercontent.com/a/browser-contract'}));
  await page.locator('.media-workspace').waitFor();
  await page.locator('#media-files').setInputFiles('__VIEWING_CSV__');
  await page.locator('[data-media-action="cancel-netflix"]').waitFor();
  // The first deterministic title request blocks until cancellation.
  await page.waitForFunction(()=>document.querySelector('[data-media-kpi="activities"]')?.textContent==='4');
  await page.locator('[data-media-action="cancel-netflix"]').click();
  await page.locator('.media-processing').waitFor({state:'detached'});
  await page.locator('[data-media-action="retry-analysis"]').click();
  await page.locator('[role="alert"]').waitFor();await page.locator('.media-processing').waitFor({state:'detached'});
  assert(await page.locator('[data-media-kpi="activities"]').innerText()==='4','failed replacement discarded the active history');
  await page.locator('[data-media-action="retry-analysis"]').click();
  await page.locator('[data-media-chart="genres"] .bar-row').first().waitFor();await page.locator('.media-processing').waitFor({state:'detached'});
  assert(await page.locator('[data-media-chart]').count()===6,'Netflix analysis does not use the approved dashboard');
  assert(await page.locator('dialog[open]').count()===0,'analysis opened a setup dialog');
  const promise=page.waitForEvent('download');await page.locator('[data-media-action="export"]').click();const download=await promise;const stream=await download.createReadStream();let csv='';for await(const chunk of stream)csv+=chunk.toString('utf8');assert(csv.includes('Synthetic Film')&&csv.includes('netflix,activity'),'CSV lost source evidence');
  await page.locator('[data-language="es"]').click();await page.waitForFunction(()=>document.documentElement.lang==='es');
  await page.locator('[data-media-action="add-files"]').click();await page.locator('#media-files').setInputFiles('__VIEWING_CSV__');
  await page.locator('.media-processing').waitFor({state:'visible'});await page.locator('.media-processing').waitFor({state:'detached'});
  assert(await page.locator('[data-media-kpi="activities"]').innerText()==='4','replacement duplicated activities');
  await page.locator('[data-language="en"]').click();await page.waitForFunction(()=>document.documentElement.lang==='en');
  await page.setViewportSize({width:320,height:800});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth),'Netflix dashboard overflows at 320px');
  await page.locator('[data-media-view="history"]').focus();await page.keyboard.press('ArrowLeft');assert(await page.locator('[data-media-view="overview"]').getAttribute('aria-selected')==='true','keyboard tabs failed');
  await page.locator('[data-media-action="manage-files"]').click();await page.locator('[data-media-action="delete-netflix"]').click();await page.locator('dialog[open]').waitFor();await page.keyboard.press('Escape');assert(await page.locator('[data-media-kpi="activities"]').innerText()==='4','dismissed deletion removed data');
  await page.locator('[data-media-action="delete-netflix"]').click();await page.evaluate(()=>{location.hash='#catalog';});await page.locator('.catalog').waitFor();assert(await page.locator('dialog').count()===0,'route cleanup retained a private dialog');
  await page.evaluate(()=>{location.hash='#app/viewing-history';});await page.locator('.media-workspace').waitFor();await page.locator('[data-media-action="manage-files"]').click();await page.locator('[data-media-action="delete-netflix"]').click();await page.locator('[data-media-confirm="true"]').click();await page.locator('#media-files').waitFor();
  assert(await page.locator('[data-media-chart]').count()===0,'provider deletion retained charts');
  assert(errors.length===0,errors.join('; '));
}
