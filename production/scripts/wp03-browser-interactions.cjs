// Native-input additions to the disposable real-API/PostgreSQL E2E fixture.
const assert = require('node:assert/strict');
const { randomUUID } = require('node:crypto');
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

module.exports = async function interactions({ send, evaluate, wait, read, click, field, reload,
  originalProject, sampleTask, setPhase }) {
  setPhase('wp03-native-input');
  const started = performance.now();
  const { csrf } = await read('/auth/me');
  const write = (suffix, method, body) => evaluate(`fetch('/api/v1'+${JSON.stringify(suffix)},
    {method:${JSON.stringify(method)},headers:{'Content-Type':'application/json','X-CSRF-Token':${JSON.stringify(csrf)}},
    body:${JSON.stringify(JSON.stringify(body))}}).then(async r=>{if(!r.ok)throw Error('synthetic interaction write failed');return r.json()})`);
  const calendar = (await read('/calendars'))[0];
  const created = await write('/projects', 'POST', { title: 'WP03 native synthetic',
    start: '2026-10-23', calendar_id: calendar.id, duration_mode: 'working' });
  const project = (await read('/projects/' + created.id)).project;
  const ids = [randomUUID(), randomUUID(), randomUUID()];
  project.tasks = ids.map((id, index) => ({ ...sampleTask, id, parent: '',
    title: 'WP03 task ' + 'ABC'[index], brief: '', description: '', result: '',
    status: 'todo', responsibility: 'customer', customer_owner: '', customer_email: '',
    customer_due: '', requested_start: '2026-10-23', start: '2026-10-23', end: '2026-10-23',
    planned_minutes: 0, actual_minutes: 0, duration_days: 1, assignments: [], summary: false }));
  const { loads, warnings, provisional_years, calculation_state, ...payload } = project;
  await write('/projects/' + created.id, 'PUT', { project: payload, override_reason: '' });
  // Reload only to obtain the synthetic new sidebar entry through the real API.
  await reload("[...document.querySelectorAll('nav button')].some(x=>x.textContent.includes('WP03 native synthetic'))", 'native fixture sidebar');
  await evaluate("[...document.querySelectorAll('nav button')].find(x=>x.textContent.includes('WP03 native synthetic')).click()");
  await wait("document.querySelector('.project-header h1')?.textContent==='WP03 native synthetic'", 'native fixture selected');
  const rect = selector => evaluate(`(()=>{const e=document.querySelector(${JSON.stringify(selector)});
    if(!e)throw Error('synthetic target absent');const r=e.getBoundingClientRect();
    return{x:r.x+r.width/2,y:r.y+r.height/2,w:r.width,h:r.height}})()`);
  const mouse = (type, p, more = {}) => send('Input.dispatchMouseEvent', {
    type, x: p.x, y: p.y, button: 'left', ...more });
  const pointerClick = async selector => {
    await evaluate(`document.querySelector(${JSON.stringify(selector)}).scrollIntoView({block:'center',inline:'center'})`);
    const point = await rect(selector);
    assert(await evaluate(`(()=>{const e=document.querySelector(${JSON.stringify(selector)}),hit=document.elementFromPoint(${point.x},${point.y});return hit===e||e.contains(hit)})()`), 'native click target is scrolled into view and hit-testable');
    await mouse('mouseMoved', point, { button: 'none' });
    await mouse('mousePressed', point, { clickCount: 1, buttons: 1 });
    await mouse('mouseReleased', point, { clickCount: 1 });
    await sleep(25);
  };
  const drag = async (from, to) => {
    await mouse('mouseMoved', from, { button: 'none' });
    await mouse('mousePressed', from, { clickCount: 1, buttons: 1 });
    for (let step = 1; step <= 4; step++) {
      await mouse('mouseMoved', { x: from.x + (to.x - from.x) * step / 4,
        y: from.y + (to.y - from.y) * step / 4 }, { buttons: 1 });
      await sleep(20);
    }
    await mouse('mouseReleased', to, { clickCount: 1 });
    await sleep(25);
  };
  const key = async (name, code, virtual, modifiers = 0) => {
    await send('Input.dispatchKeyEvent', { type: 'keyDown', key: name, code,
      windowsVirtualKeyCode: virtual, nativeVirtualKeyCode: virtual, modifiers,
      ...(name === 'Enter' && modifiers === 0 ? { text: '\r', unmodifiedText: '\r' } : {}) });
    await send('Input.dispatchKeyEvent', { type: 'keyUp', key: name, code,
      windowsVirtualKeyCode: virtual, nativeVirtualKeyCode: virtual, modifiers });
    await sleep(25);
  };
  const state = async () => (await read('/projects/' + created.id)).project;
  const card = id => `[data-task="${id}"]>.task-card`;
  const port = (id, side) => `[data-id="${id}"][data-port="${side}"]`;
  const camera = () => evaluate("document.querySelector('[data-map-content]').getAttribute('transform')");
  const scale = value => Number(value.match(/scale\(([^)]+)\)/)[1]);
  setPhase('wp03-native-gantt');
  await click('Гант');
  await wait(`!!document.querySelector(${JSON.stringify(card(ids[0]))})`, 'movable bar');
  const bar = await rect(card(ids[0]));
  await drag(bar, { x: bar.x + 128, y: bar.y });
  await wait(`document.querySelector('[data-duration="${ids[0]}"]')?.textContent`, 'bar duration');
  for (let n = 0; n < 100 && (await state()).tasks.find(t => t.id === ids[0]).start !== '2026-10-27'; n++) await sleep(30);
  assert.equal((await state()).tasks.find(t => t.id === ids[0]).start, '2026-10-27', 'actual bar move saved four date days');
  assert.equal(await evaluate("!!document.querySelector('dialog[open]')"), false, 'gesture applies without preview');
  const movedRevision = (await state()).revision;
  await drag(await rect(port(ids[0], 'out')), await rect(port(ids[1], 'in')));
  await wait("document.querySelectorAll('dialog form select').length===3", 'Gantt ports open link editor');
  assert.equal(await evaluate("document.querySelectorAll('dialog form select')[0].value"), ids[0]);
  assert.equal(await evaluate("document.querySelectorAll('dialog form select')[1].value"), ids[1]);
  await key('Escape', 'Escape', 27);
  await wait("!document.querySelector('dialog')", 'cancel Gantt link');
  assert.equal((await state()).revision, movedRevision, 'cancelled Gantt wire does not mutate the project');

  setPhase('wp03-native-map-camera');
  await click('Майнд-мэп'); await click('Вписать');
  const viewRevision = (await state()).revision;
  const beforeWheel = await camera(), viewport = await rect('.map-viewport');
  await send('Input.dispatchMouseEvent', { type: 'mouseWheel', x: viewport.x, y: viewport.y, deltaX: 0, deltaY: 120 });
  await wait(`document.querySelector('[data-map-content]').getAttribute('transform')!==${JSON.stringify(beforeWheel)}`, 'native wheel zoom');
  const afterWheel = await camera(); assert(scale(afterWheel) < scale(beforeWheel), 'wheel zooms out');
  const background = await evaluate(`(()=>{const r=document.querySelector('.map-viewport').getBoundingClientRect();
    for(let y=r.top+30;y<r.bottom-25;y+=35)for(let x=r.left+30;x<r.right-80;x+=35){
      const e=document.elementFromPoint(x,y);if(e?.closest('.map-viewport svg')&&!e.closest('[data-task],[data-link]'))return{x,y};}
    throw Error('no synthetic background');})()`);
  await drag(background, { x: background.x + 45, y: background.y + 30 });
  await wait(`document.querySelector('[data-map-content]').getAttribute('transform')!==${JSON.stringify(afterWheel)}`, 'native background pan');
  assert.notEqual(await camera(), afterWheel, 'native background drag pans');
  assert.equal(scale(await camera()), scale(afterWheel), 'pan preserves zoom');
  await click('Вписать');
  const nodeBefore = await evaluate(`document.querySelector(${JSON.stringify(card(ids[0]))}).getAttribute('x')`);
  const node = await rect(card(ids[0]));
  await drag(node, { x: node.x + 35, y: node.y + 25 });
  await wait(`Number(document.querySelector(${JSON.stringify(card(ids[0]))}).getAttribute('x'))>${JSON.stringify(Number(nodeBefore))}`, 'native node position');
  assert(Number(await evaluate(`document.querySelector(${JSON.stringify(card(ids[0]))}).getAttribute('x')`)) > Number(nodeBefore), 'native node move changes map position');
  assert.equal((await state()).revision, viewRevision, 'wheel/pan/node positioning does not mutate dates/project');

  const connect = async (from, to) => {
    await click('Вписать');
    const source = await rect(port(from, 'out')), target = await rect(port(to, 'in'));
    for (const [point, id, side] of [[source, from, 'out'], [target, to, 'in']]) {
      assert(await evaluate(`(()=>{const e=document.elementFromPoint(${point.x},${point.y})?.closest('[data-port]');return e?.dataset.id===${JSON.stringify(id)}&&e?.dataset.port===${JSON.stringify(side)}})()`), 'native dependency endpoint is actually hit-testable');
    }
    setPhase('wp03-native-port-drag');
    await drag(source, target);
    await wait("!!document.querySelector('dialog form select')", 'native port drag opens link editor');
    setPhase('wp03-native-port-editor');
    assert.equal(await evaluate("document.querySelectorAll('dialog form select')[0].value"), from);
    assert.equal(await evaluate("document.querySelectorAll('dialog form select')[1].value"), to);
  };
  const submitLink = async () => {
    setPhase('wp03-native-link-submit');
    assert(await evaluate("document.querySelector('dialog form').checkValidity()"), 'native link form is valid before keyboard submit');
    await evaluate("(()=>{const f=document.querySelector('dialog form');f.dataset.nativeSubmitted='false';f.addEventListener('submit',()=>f.dataset.nativeSubmitted='true',{once:true});f.querySelector('button:not([type])').focus()})()");
    await key('Enter', 'Enter', 13);
    await wait("!document.querySelector('dialog form')||document.querySelector('dialog form').dataset.nativeSubmitted==='true'", 'native Enter submits form');
  };
  const apply = async () => {
    setPhase('wp03-native-link-preview');
    await wait("document.querySelector('dialog h2')?.textContent==='Проверка изменений'", 'real link preview');
    await click('Применить изменения');
    await wait("!document.querySelector('dialog[open]')", 'real link saved');
  };
  setPhase('wp03-native-dependencies');
  await connect(ids[0], ids[1]); await submitLink(); await apply();
  assert.equal((await state()).links.length, 1);
  await connect(ids[0], ids[2]); await submitLink(); await apply();
  assert.equal((await state()).links.length, 2);
  await click('Вписать');
  setPhase('wp03-native-bundle');
  const linePoint = await evaluate(`(()=>{const p=document.querySelector('[data-link="0"] path[stroke="transparent"]');
    const point=p.getPointAtLength(p.getTotalLength()*.45),m=p.getScreenCTM();return{x:m.a*point.x+m.c*point.y+m.e,y:m.b*point.x+m.d*point.y+m.f};})()`);
  await mouse('mouseMoved', linePoint, { button: 'none' }); await sleep(25);
  assert.equal(await evaluate("!!document.querySelector('.wire-fan')"), false, 'hover does not open bundle');
  await mouse('mousePressed', linePoint, { buttons: 1, clickCount: 1 });
  await mouse('mouseReleased', linePoint, { clickCount: 1 });
  await wait("document.querySelectorAll('.wire-fan button[style]').length===2", 'bundle click opens parallel choices');
  const fanItem = await rect('.wire-fan button[style]');
  await mouse('mouseMoved', fanItem, { button: 'none' });
  await wait(`document.querySelector(${JSON.stringify(card(ids[1]))}).getAttribute('stroke')==='#086ad8'`, 'bundle highlights both endpoints');
  assert.equal(await evaluate(`document.querySelector(${JSON.stringify(card(ids[0]))}).getAttribute('stroke')`), '#086ad8');
  // Double click the selected bundle entry to edit its actual relationship.
  for (const count of [1, 2]) {
    await mouse('mousePressed', fanItem, { buttons: 1, clickCount: count });
    await mouse('mouseReleased', fanItem, { clickCount: count });
  }
  await wait("!!document.querySelector('dialog form select')", 'bundle editor');
  // Labels wrap selects, so use a stable positional selector for the mode.
  await evaluate("(()=>{const e=document.querySelectorAll('dialog form select')[2];Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype,'value').set.call(e,'delay');e.dispatchEvent(new Event('change',{bubbles:true}));})()");
  await field('dialog input[type=number]', '2'); await submitLink(); await apply();
  assert((await state()).links.some(link => link.from === ids[0] && link.to === ids[1] && link.mode === 'delay' && link.days === 2), 'native bundle editor saves interval');

  setPhase('wp03-native-cycle-retry');
  const beforeCycle = (await state()).revision;
  await connect(ids[1], ids[0]); await submitLink();
  await wait("!!document.querySelector('dialog [role=alert]')", 'cycle error is inside dialog');
  assert.equal(await evaluate("!!document.querySelector('.global-error')"), false, 'cycle has no unrelated global banner');
  assert.equal((await state()).revision, beforeCycle, 'rejected cycle leaves durable project untouched');
  await evaluate(`(()=>{const e=document.querySelectorAll('dialog form select')[1];Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype,'value').set.call(e,${JSON.stringify(ids[2])});e.dispatchEvent(new Event('change',{bubbles:true}));})()`);
  await submitLink(); await apply();
  assert.equal((await state()).links.length, 3, 'Enter/error/retry persists only corrected dependency');

  // Native modal keyboard checks: task Escape is intentionally non-dismissible.
  setPhase('wp03-native-keyboard');
  await click('Задачи');
  const title = `button.text-button.title`;
  await evaluate(`document.querySelector(${JSON.stringify(title)}).focus()`);
  await key('Enter', 'Enter', 13);
  await wait("!!document.querySelector('dialog input[maxlength=\"500\"]')", 'keyboard opens task');
  setPhase('wp03-native-keyboard-draft');
  await field('dialog input[maxlength="500"]', 'Unsaved native keyboard draft');
  await key('Escape', 'Escape', 27);
  assert.equal(await evaluate("document.querySelector('dialog input[maxlength=\"500\"]').value"), 'Unsaved native keyboard draft', 'task Escape retains editor and draft');
  const controls = await evaluate("[...document.querySelector('dialog').querySelectorAll('button,input,select,textarea,summary')].filter(x=>!x.disabled).length");
  setPhase('wp03-native-keyboard-tab');
  for (let n = 0; n < controls + 2; n++) {
    await key('Tab', 'Tab', 9);
    assert(await evaluate("document.querySelector('dialog').contains(document.activeElement)"), 'native Tab remains in modal');
    if (n === 0) {
      const focus = await evaluate(`(()=>{const e=document.activeElement,s=getComputedStyle(e),r=e.getBoundingClientRect();
        return{visible:e.matches(':focus-visible'),style:s.outlineStyle,width:parseFloat(s.outlineWidth),
        color:s.outlineColor,box:r.width>0&&r.height>0};})()`);
      assert(focus.visible && focus.box && focus.style !== 'none' && focus.width > 0 &&
        !['transparent', 'rgba(0, 0, 0, 0)'].includes(focus.color), 'keyboard focus has a visible computed outline');
    }
  }
  await key('Tab', 'Tab', 9, 8);
  assert(await evaluate("document.querySelector('dialog').contains(document.activeElement)"), 'native Shift-Tab remains in modal');
  setPhase('wp03-native-keyboard-close');
  await pointerClick('dialog header button');
  await wait("!document.querySelector('dialog')", 'close task by explicit cross');
  setPhase('wp03-native-keyboard-focus-return');
  await wait(`document.activeElement===document.querySelector(${JSON.stringify(title)})`, 'task focus return');
  assert(await evaluate(`document.activeElement===document.querySelector(${JSON.stringify(title)})`), 'native focus returns to task opener');
  assert(!(await state()).tasks.some(task => task.title === 'Unsaved native keyboard draft'), 'cancelled task draft never persisted');
  await click('Майнд-мэп');
  setPhase('wp03-native-keyboard-link-escape');
  await connect(ids[2], ids[0]);
  await key('Escape', 'Escape', 27);
  await wait("!document.querySelector('dialog')", 'ordinary link dialog supports native Escape');
  assert.equal((await state()).links.length, 3, 'dismissed new link never persisted');

  const final = await state();
  await write('/projects/' + created.id, 'DELETE', { revision: final.revision });
  const back = await read('/projects/' + originalProject);
  assert(back.project.id === originalProject, 'original fixture preserved');
  await reload("document.querySelector('.project-header h1')?.textContent==='E2E synthetic project'", 'original fixture restored');
  console.log('WP03 native browser PASS: bar save/Gantt wire cancel, wheel/pan/node, port links/edit/cycle/retry, bundle selection/endpoints, Tab/Shift-Tab/Enter/Escape/focus/draft; elapsed_ms=' + Math.round(performance.now() - started));
};
