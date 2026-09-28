async page => {
  const baseURL = '__BASE_URL__';
  const scenario = '__REGRESSION__';
  const assert = (condition,message) => { if (!condition) throw new Error(message); };
  await page.goto(`${baseURL}/#app/viewing-history`,{waitUntil:'domcontentloaded'});
  await page.waitForFunction(()=>window.MPRUI?.testing);
  await page.locator('[data-auth-state]').waitFor();
  await page.context().addCookies([{name:'__SESSION_COOKIE__',value:'__SESSION_TOKEN__',url:baseURL,httpOnly:true,sameSite:'Lax'}]);
  await page.evaluate(()=>window.MPRUI.testing.authenticate(document.querySelector('#app-header'),{user_id:'browser-media-user',user_email:'browser@example.invalid',user_display_name:'Regression fixture',display:'Regression fixture',avatar_url:'https://lh3.googleusercontent.com/a/browser-contract',user_avatar_url:'https://lh3.googleusercontent.com/a/browser-contract'}));
  await page.locator('.media-workspace').waitFor();
  await page.locator('#media-prime-file').setInputFiles('__PRIME_ZIP__');
  if (scenario==='upload-recovery') {
    const archivePattern = `${baseURL}/api/providers/prime-video/generations/*/archive`;
    await page.route(archivePattern,route=>route.fulfill({status:413,contentType:'application/json',json:{error:{code:'upload_too_large'}}}));
    await page.locator('[data-media-action="preview-prime"]').click();
    await page.waitForFunction(()=>document.querySelector('.media-workspace [role="alert"]')?.textContent.includes('upload_too_large'));
    assert(await page.locator('[data-media-action="cancel-prime"]').count()===1,'failed upload hid pending cancellation');
    await page.locator('[data-media-action="cancel-prime"]').click();
    await page.waitForFunction(()=>!document.querySelector('[data-media-action="cancel-prime"]'));
    await page.unroute(archivePattern);
    await page.locator('#media-prime-file').setInputFiles('__PRIME_ZIP__');
    await page.locator('[data-media-action="preview-prime"]').click();
    await page.locator('.media-preview').waitFor();
    await page.locator('[data-media-action="confirm-prime"]').click();
    await page.waitForFunction(()=>document.querySelector('[data-media-kpi="activities"]')?.textContent==='120');
    return;
  }
  await page.locator('#media-prime-label').fill('Prime household');
  await page.locator('[data-media-action="preview-prime"]').click();
  await page.locator('.media-preview').waitFor();
  if (scenario==='drafts') {
    assert(await page.locator('#media-prime-label').inputValue()==='Prime household','preview discarded the import label');
    await page.locator('input[name="dataset"][value="searches"]').check();
    await page.locator('[data-media-view="sources"]').click();
    assert(await page.locator('input[name="dataset"][value="searches"]').isChecked(),'tab change discarded dataset selection');
  }
  await page.locator('[data-media-action="confirm-prime"]').click();
  await page.locator('[data-media-view="overview"]').click();
  await page.waitForFunction(()=>document.querySelector('[data-media-kpi="activities"]')?.textContent==='120');
  await page.locator('[data-media-action="enrich-prime"]').click();
  await page.locator('[data-media-confirm="true"]').click();
  await page.locator('[data-media-action="resume-prime"]').waitFor();
  if (scenario==='drafts') {
    await page.locator('#media-netflix-file').setInputFiles('__VIEWING_CSV__');
    await page.locator('#media-netflix-label').fill('Netflix profile');
    await page.locator('#media-title-filter').fill('Unapplied draft');
    const poll = page.waitForResponse(response=>response.url().includes('/api/viewing-history?') && response.status()===200);
    await (await poll).finished();
    await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));
    assert(await page.locator('#media-title-filter').inputValue()==='Unapplied draft','poll discarded the filter draft');
    assert(await page.locator('#media-netflix-label').inputValue()==='Netflix profile','poll discarded the Netflix label');
    assert(await page.locator('#media-netflix-file').evaluate(input=>input.files.length)===1,'poll discarded the selected file');
    await page.locator('[data-media-action="cancel-prime"]').click();
    await page.locator('[data-media-action="import-netflix"]').click();
    await page.waitForFunction(()=>document.querySelector('[data-media-kpi="activities"]')?.textContent==='121');
    await page.locator('[data-media-view="activity"]').click();
    await page.locator('#media-provider').selectOption('netflix');
    await page.locator('#media-title-filter').fill('');
    await page.locator('[data-media-action="apply-filters"]').click();
    await page.waitForFunction(()=>document.querySelector('.media-records')?.textContent.includes('Netflix profile'));
    await page.evaluate(()=>{location.hash='#catalog';});
    await page.locator('.catalog-grid').waitFor();
    await page.evaluate(()=>{location.hash='#app/viewing-history';});
    await page.locator('.media-workspace').waitFor();
    assert(await page.locator('#media-netflix-label').inputValue()==='','route change retained a private draft');
    return;
  }
  assert(scenario==='pagination','unknown regression scenario');
  await page.locator('[data-media-view="titles"]').click();
  await page.locator('[data-media-action="next-titles"]').click();
  await page.waitForFunction(()=>document.querySelectorAll('.media-titles > tbody > tr').length===20);
  await page.locator('[data-media-view="activity"]').click();
  await page.locator('[data-media-action="next"]').click();
  await page.waitForFunction(()=>document.querySelectorAll('.media-records > tbody > tr').length===20);
  let conflict = false;
  let released = false;
  let conflictStatus = 0;
  await page.route(`${baseURL}/api/viewing-history?*`,async route=>{
    if (!released && /[?&]cursor=/.test(route.request().url())) {
      released = true;
      await page.request.get(`${baseURL}/fixture/release-enrichment`);
      await page.evaluate(async baseURL=>{
        const deadline = Date.now()+10000;
        while (Date.now()<deadline) {
          const snapshot = await (await fetch(`${baseURL}/api/providers/prime-video`,{credentials:'include'})).json();
          if (snapshot.active_generation?.analysis_level==='tmdb' && snapshot.building_generation===null) return;
          await new Promise(resolve=>setTimeout(resolve,25));
        }
        throw new Error('fixture enrichment did not complete');
      },baseURL);
      const response = await route.fetch();
      conflictStatus = response.status();
      conflict = response.status()===409 && (await response.json()).error.code==='stale_cursor';
      await route.fulfill({response});
    } else await route.continue();
  });
  await page.waitForFunction(()=>!document.querySelector('[data-media-action="resume-prime"]') && document.querySelectorAll('.media-records > tbody > tr').length===100);
  assert(conflict,`generation race did not return the canonical cursor conflict: released=${released}, status=${conflictStatus}`);
  assert(await page.locator('.media-workspace [role="alert"]').count()===0,'cursor conflict stopped automatic updates');
  assert(await page.locator('[data-media-action="previous"]').isDisabled(),'activity cursor stack was retained');
  await page.locator('[data-media-view="titles"]').click();
  assert(await page.locator('.media-titles > tbody > tr').count()===100,'title cursor was retained');
  assert(await page.locator('[data-media-action="previous-titles"]').isDisabled(),'title cursor stack was retained');
}
