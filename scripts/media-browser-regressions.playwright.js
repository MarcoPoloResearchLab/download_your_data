async page => {
  const baseURL='__BASE_URL__',scenario='__REGRESSION__';
  const errors=[];page.on('pageerror',error=>errors.push(error.message));
  const assert=(condition,message)=>{if(!condition)throw new Error(message);};
  const activities=expected=>page.waitForFunction(expected=>document.querySelector('[data-media-kpi="activities"]')?.textContent===String(expected),expected).catch(async failure=>{throw new Error(`Expected ${expected} activities: ${await page.locator('.media-workspace').innerText()}`);});
  await page.context().addCookies([{name:'__SESSION_COOKIE__',value:'__SESSION_TOKEN__',url:baseURL,httpOnly:true,sameSite:'Lax'}]);
  await page.goto(`${baseURL}/#app/viewing-history`,{waitUntil:'domcontentloaded'});
  await page.waitForFunction(()=>window.MPRUI?.testing);
  await page.evaluate(()=>window.MPRUI.testing.authenticate(document.querySelector('#app-header'),{user_id:'browser-media-user',user_email:'browser@example.invalid',user_display_name:'Media contract',user_avatar_url:'https://lh3.googleusercontent.com/a/browser-contract',display:'Media contract',avatar_url:'https://lh3.googleusercontent.com/a/browser-contract'}));
  await page.locator('.media-workspace').waitFor();
  if(scenario==='analysis-start-failure'||scenario==='selection-failure'){
    if(scenario==='analysis-start-failure')await page.route(`${baseURL}/api/providers/netflix/generations`,route=>route.request().method()==='POST'&&route.request().postDataJSON().analysis_level==='tmdb'?route.fulfill({status:503,json:{error:{code:'analysis_unavailable'}}}):route.continue());
    else await page.route(`${baseURL}/api/providers/prime-video/generations/*/selection`,route=>route.fulfill({status:503,json:{error:{code:'analysis_unavailable'}}}));
    await page.route(`${baseURL}/api/providers/netflix/generations/*/viewing-activity`,async route=>{
      const response=await route.fetch();
      const deadline=Date.now()+10000;
      while(true){const snapshot=await page.request.get(`${baseURL}/api/providers/netflix`);const state=await snapshot.json();if(state.active_generation?.state==='ready')break;assert(Date.now()<deadline,'raw Netflix fixture did not activate');}
      await route.fulfill({response});
    });
    await page.evaluate(async baseURL=>{const response=await fetch(`${baseURL}/fixture/release-enrichment`);if(!response.ok)throw new Error('fixture release failed');},baseURL);
    const completedProvider=scenario==='selection-failure'?'netflix':'prime-video';
    const independentlyAnalyzed=page.waitForResponse(async response=>response.url()===`${baseURL}/api/providers/${completedProvider}`&&response.status()===200&&(await response.json()).active_generation?.analysis_level==='tmdb');
    await page.locator('#media-files').setInputFiles(['__VIEWING_CSV__','__PRIME_ZIP__']);
    await activities(scenario==='selection-failure'?1:121);
    await independentlyAnalyzed;
    assert(await page.locator('[data-media-chart="genres"] .bar-value').innerText()==='1','one service failure blocked independent title analysis');
    assert(await page.locator('.media-workspace [role="alert"]').count()===1,'analysis failure is not visible');
    if(scenario==='selection-failure')assert(await page.locator('[data-media-action="cancel-prime-video"]').isVisible(),'Prime cancellation disappeared after selection failure');
    assert(errors.length===0,errors.join('; '));return;
  }
  if(scenario==='upload-recovery'){
    let reject=true;await page.route(`${baseURL}/api/providers/prime-video/generations/*/archive`,route=>{if(reject){reject=false;return route.fulfill({status:413,json:{error:{code:'upload_too_large'}}});}return route.continue();});
    await page.locator('#media-files').setInputFiles('__PRIME_ZIP__');await page.locator('[role="alert"]').waitFor();
    await page.locator('[data-media-action="cancel-prime-video"]').click();await page.locator('.media-processing').waitFor({state:'detached'});
  }
  await page.locator('[data-media-action="add-files"]').click();await page.locator('#media-files').setInputFiles('__PRIME_ZIP__');await activities(120);
  const release=()=>page.evaluate(async baseURL=>{const response=await fetch(`${baseURL}/fixture/release-enrichment`);if(!response.ok)throw new Error('fixture release failed');},baseURL);
  if(scenario==='chart-inspection'){
    await release();await page.locator('.media-processing').waitFor({state:'detached'});
    const inspect=async(name,fraction)=>{
      await page.mouse.move(0,0);
      const chart=page.locator(`[data-media-chart="${name}"]`);
      await chart.locator('.media-plot').scrollIntoViewIfNeeded();
      await page.waitForFunction(name=>{const svg=document.querySelector(`[data-media-chart="${name}"] svg`);return svg&&Math.abs(svg.viewBox.baseVal.width-svg.getBoundingClientRect().width)<1;},name);
      const box=await chart.locator('[data-chart-hit]').boundingBox();assert(box,'chart hit area is absent');
      await chart.locator('[data-chart-hit]').hover({position:{x:box.width*fraction,y:box.height*.9}});
      const tooltip=chart.locator('.media-chart-tooltip');await tooltip.waitFor({state:'visible'});
      return {label:await tooltip.locator('strong').innerText(),values:await tooltip.locator('div > span:last-child').allTextContents()};
    };
    for(const width of [1440,320]){
      await page.setViewportSize({width,height:900});
      const month=await inspect('monthly',.26);assert(month.label==='Feb'&&month.values.join(',')==='0,117',`line inspection changed at ${width}: ${JSON.stringify(month)}`);
      for(const sample of [{position:.11,day:'Mon',count:'1'},{position:.26,day:'Tue',count:'2'},{position:.99,day:'Sun',count:'0'}]){
        const weekday=await inspect('weekday-genres',sample.position);
        assert(weekday.label===sample.day&&weekday.values.join(',')===sample.count,`weekday inspection differs at ${width}: expected ${sample.day} ${sample.count}, got ${JSON.stringify(weekday)}`);
      }
    }
  }else if(scenario==='drafts'){
    await page.locator('#media-title-filter').fill('Synthetic Title');await activities(119);
    await page.locator('.media-more summary').click();await page.locator('#media-timezone').fill('America/Los_Angeles');
    await page.locator('[data-media-view="history"]').click();await page.locator('[data-media-view="overview"]').click();
    await release();await page.locator('.media-processing').waitFor({state:'detached'});
    assert(await page.locator('#media-title-filter').inputValue()==='Synthetic Title','title filter lost during automatic update');
    assert(await page.locator('#media-timezone').inputValue()==='America/Los_Angeles','timezone lost during automatic update');
    await page.locator('#media-title-filter').fill('Synthetic Title 01');await activities(10);
    assert(await page.locator('#media-title-filter').evaluate(node=>node===document.activeElement),'filter redraw lost keyboard focus');
  }else if(scenario==='pagination'){
    await page.locator('[data-media-view="history"]').click();assert(await page.locator('.media-records > tbody > tr').count()===100,'first page incomplete');
    await page.locator('[data-media-action="next"]').click();await page.waitForFunction(()=>document.querySelectorAll('.media-records > tbody > tr').length===20);
    await release();await page.locator('.media-processing').waitFor({state:'detached'});await page.waitForFunction(()=>document.querySelectorAll('.media-records > tbody > tr').length===100);
    await page.locator('[data-media-view="overview"]').click();await activities(120);
    assert(await page.locator('[data-media-chart="media-types"]').innerText().then(text=>text.includes('120')),'chart totals use the page size');
  }else if(scenario==='charts'){
    await release();await page.locator('.media-processing').waitFor({state:'detached'});
    const monthly=await page.locator('[data-media-chart="monthly"] .chart-data tbody tr').evaluateAll(rows=>rows.map(row=>Array.from(row.querySelectorAll('td')).reduce((sum,cell)=>sum+Number(cell.textContent),0)));assert(monthly.join(',')==='40,0,80','monthly chart lost activity counts or a zero-activity month');
    await page.locator('.media-more summary').click();await page.locator('#media-start').fill('2026-01-01');await page.locator('#media-end').fill('2026-01-31');await activities(40);
    assert(await page.locator('[data-media-chart="monthly"] svg desc').textContent().then(text=>!text.includes('Mar')),'monthly graph ignores date filter');
    await page.locator('#media-type').selectOption('movie');await activities(1);
    assert(await page.locator('[data-media-chart="genres"] .bar-value').innerText()==='1','genre counts ignore content filter');
    await page.locator('[data-media-action="clear-filters"]').click();await activities(120);
    await page.locator('#media-title-filter').fill('No matching title');await activities(0);
    assert(await page.locator('[data-media-chart] .empty-copy').count()===6,'empty charts retain values');
    await page.locator('#media-title-filter').fill('');await activities(120);
    await page.setViewportSize({width:320,height:800});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=document.documentElement.clientWidth),'chart page overflows');
    await page.screenshot({path:'__SCREENSHOT_ROOT__/f024-charts-narrow.png',fullPage:true});
  }else await release();
  assert(errors.length===0,errors.join('; '));
}
