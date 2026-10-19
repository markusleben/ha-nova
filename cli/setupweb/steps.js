// HA NOVA setup page, steps 3 ("You stay in control") and 4 ("Here's your
// home, at a glance"). app.js hands in its helpers; the CLI sends data only,
// and every text goes in as textContent. A save posts the id of the plan the
// page shows; the CLI saves only a plan it stored for that preview.
'use strict';

window.novaSteps = (h) => {
  const { t, el, row, head, button, render } = h;
  const say = (msg) => (msg ? t(msg.key, msg.args || {}) : '');
  // Switches and open details live here until "Finish setup" sends them.
  const local = { rule: true, link: false, others: false, drafts: {} };

  const toast = (text) => {
    const node = el('div', { class: 'toast', role: 'status' }, text);
    document.body.append(node);
    setTimeout(() => node.remove(), 2600);
  };

  const SVG = 'http://www.w3.org/2000/svg';
  const icon = (shapes) => {
    const svg = document.createElementNS(SVG, 'svg');
    svg.setAttribute('viewBox', '0 0 32 32');
    svg.setAttribute('aria-hidden', 'true');
    for (const [tag, attrs] of shapes) {
      const shape = document.createElementNS(SVG, tag);
      for (const [name, value] of Object.entries(attrs)) shape.setAttribute(name, value);
      svg.append(shape);
    }
    return svg;
  };
  const icons = {
    rooms: [['path', { d: 'M5 15 16 6l11 9' }], ['path', { d: 'M8 13v13h16V13' }], ['path', { d: 'M14 26v-7h4v7' }]],
    devices: [['rect', { x: 9, y: 9, width: 14, height: 14, rx: 3 }], ['path', { d: 'M13 9V5M19 9V5M13 27v-4M19 27v-4M9 13H5M9 19H5M27 13h-4M27 19h-4' }]],
    lights: [['path', { d: 'M11 18a7 7 0 1 1 10 0c-1.3 1.2-2 2.6-2 4h-6c0-1.4-.7-2.8-2-4z' }], ['path', { d: 'M13 26h6' }]],
    locks: [['rect', { x: 8, y: 14, width: 16, height: 12, rx: 2 }], ['path', { d: 'M11 14v-3a5 5 0 0 1 10 0v3' }], ['path', { d: 'M16 19v3' }]],
    automations: [['path', { d: 'M17 4 8 18h7l-1 10 9-14h-7z' }]],
  };

  const guard = (glyph) => el('span', { class: 'ic guard', 'aria-hidden': 'true' }, glyph);
  const spinner = () => el('span', { class: 'ic run amber', 'aria-hidden': 'true' });
  const line = (glyph, key, value, below, side) =>
    el('li', {}, glyph, el('div', {}, el('div', { class: 'k' }, key), value ? el('div', { class: 'v' }, value) : null, below), side || el('span'));
  const toggle = (id, label, on, onclick, disabled) =>
    el('button', { id, class: 'switch', type: 'button', role: 'switch', 'aria-checked': String(on), 'aria-label': label, onclick, ...(disabled ? { disabled: '' } : {}) });
  const tagOn = () => el('span', { class: 'tag amber' }, t('setup.page.control.on'));
  const problem = (msg) => (msg ? el('p', { class: 'problem', role: 'alert' }, say(msg)) : null);

  // Step 3.
  const phoneRow = (c) => {
    const key = t('setup.page.control.phone_key');
    const phones = c.phones || [];
    switch (c.phone) {
      case 'linked':
        return line(guard('✓'), key, t('setup.page.control.phone_value', { phone: c.phone_name }), null, tagOn());
      case 'waiting':
        return line(guard('◎'), key, t('setup.page.control.phone_waiting', { phone: c.phone_name }), null, spinner());
      case 'none':
        return line(guard('·'), key, t('setup.page.control.phone_none'));
      case 'pick': {
        if (phones.length === 1 || (c.proposed && !local.others)) {
          const other = phones.length > 1
            ? el('button', { id: 'b-phone-other', class: 'linkish', type: 'button', onclick: () => { local.others = true; render(true); } }, t('setup.page.control.phone_other'))
            : null;
          return line(guard('◎'), key, t('setup.page.control.phone_value', { phone: phones[0].name }), [problem(c.phone_problem), other],
            toggle('b-phone-0', key, false, () => h.post('phone', phones[0].id), !h.accepts('phone')));
        }
        const pick = el('div', { class: 'pick' }, phones.map((p, i) =>
          button(t('setup.page.control.phone_choice', { phone: p.name, person: p.person }), 'phone', 'ghost btn-small', p.id, `phone-${i}`)));
        return line(guard('◎'), key, t('setup.page.control.phone_pick'), [problem(c.phone_problem), pick]);
      }
      default:
        return line(guard('·'), key, t('setup.page.control.phone_unavailable'));
    }
  };

  const control = () => {
    const c = h.state().control || {};
    const rows = [
      line(guard(c.window ? '✓' : '·'), t('setup.page.control.window_key'),
        t(c.window ? 'setup.page.control.window_value' : 'setup.page.control.window_off'), null, c.window ? tagOn() : null),
      phoneRow(c),
    ];
    const busy = !h.accepts('finish');
    if (c.rule === 'offer') {
      const label = t('setup.page.control.rule_key');
      rows.push(line(guard('⚙'), label, t('setup.page.control.rule_value'), null,
        toggle('b-rule', label, local.rule, () => { local.rule = !local.rule; render(true); }, busy)));
    } else if (c.rule === 'on') {
      rows.push(line(guard('⚙'), t('setup.page.control.rule_key'), t('setup.page.control.rule_on'), null, tagOn()));
    }
    if (c.link === 'offer') {
      const label = t('setup.page.control.link_key');
      rows.push(line(guard('⇄'), label, t('setup.page.control.link_value'), null,
        toggle('b-link', label, local.link, () => { local.link = !local.link; render(true); }, busy)));
    } else if (c.link === 'run') {
      rows.push(line(guard('⇄'), t('setup.page.control.link_key'), t('setup.page.control.link_run'), null, spinner()));
    }
    const choices = [];
    if (c.rule === 'offer' && local.rule) choices.push('rule');
    if (c.link === 'offer' && local.link) choices.push('link');
    return [...head(t('setup.page.control.eyebrow'), t('setup.page.control.title'), t('setup.page.control.lead'), true),
      el('ul', { class: 'rows' }, rows),
      el('div', { class: 'actions' }, button(t('setup.page.control.finish'), 'finish', 'primary', choices.join(','))),
      c.phone === 'pick' ? el('p', { class: 'fine' }, t('setup.page.control.fine')) : null];
  };

  // Step 4.
  const named = (uses, role) => (uses || []).filter((u) => u.role === role).map((u) => u.name);
  const templateTitle = (m) => {
    const trigger = (m.uses || []).find((u) => u.role === 'trigger');
    switch (m.template) {
      case 'door_light_after_dark':
        return t(trigger && trigger.entity_id.startsWith('lock.') ? 'setup.page.home.t_door_light_lock' : 'setup.page.home.t_door_light_door', { trigger: trigger ? trigger.name : '' });
      case 'lights_off_when_away':
        return t('setup.page.home.t_lights_off');
      default:
        return t('setup.page.home.t_reminder');
    }
  };
  // When / Only / Do, worded from the plan's own template and entities.
  const summary = (pv) => {
    const lines = [];
    const trigger = (pv.uses || []).find((u) => u.role === 'trigger');
    switch (pv.template) {
      case 'door_light_after_dark':
        lines.push(t('setup.page.home.when', { text: t(trigger && trigger.entity_id.startsWith('lock.') ? 'setup.page.home.when_unlocked' : 'setup.page.home.when_opens', { trigger: trigger ? trigger.name : '' }) }),
          t('setup.page.home.only', { text: t('setup.page.home.only_dark') }),
          t('setup.page.home.do', { text: t('setup.page.home.do_light_on', { light: named(pv.uses, 'light')[0] || '' }) }));
        break;
      case 'lights_off_when_away':
        lines.push(t('setup.page.home.when', { text: t('setup.page.home.when_away') }),
          t('setup.page.home.do', { text: t('setup.page.home.do_lights_off', { count: named(pv.uses, 'light').length }) }));
        break;
      default:
        lines.push(t('setup.page.home.when', { text: t('setup.page.home.when_battery') }),
          t('setup.page.home.do', { text: t('setup.page.home.do_notify', { phone: named(pv.uses, 'phone')[0] || '' }) }));
    }
    return lines.map((text) => el('div', {}, text));
  };
  // Devices grouped by the room they end up in.
  const roomLines = (changes) => {
    const groups = new Map();
    for (const c of changes || []) {
      const area = c.area || t('setup.page.home.no_area');
      if (!groups.has(area)) groups.set(area, []);
      groups.get(area).push(c.name);
    }
    return [...groups].map(([area, devices]) => el('div', {}, t('setup.page.home.rooms_line', { area, devices })));
  };
  const saveLabels = { rooms: 'setup.page.home.save_rooms', reminder: 'setup.page.home.save_reminder', automation: 'setup.page.home.save_automation' };

  const previewBlock = (slot, pv) => {
    if (!pv) return null;
    const parts = [];
    const save = (key) => button(t(key), 'save', 'primary btn-small', pv.plan_id, `save-${slot}`);
    if (pv.state === 'loading' || pv.state === 'saving') parts.push(el('div', { class: 'busy' }, spinner()));
    if (pv.notice) parts.push(el('div', { class: 'add' }, say(pv.notice)));
    if (pv.kind === 'template') {
      parts.push(...summary(pv));
      if (pv.draft) {
        const details = el('details', {}, el('summary', {}, t('setup.page.home.draft')), el('pre', {}, pv.draft));
        details.open = Boolean(local.drafts[slot]);
        details.addEventListener('toggle', () => { local.drafts[slot] = details.open; });
        parts.push(details);
      }
      if (pv.state === 'preview') parts.push(el('div', { class: 'add' }, t('setup.page.home.template_note', { button: t(saveLabels[slot]) })), save(saveLabels[slot]));
      if (pv.state === 'saved') parts.push(el('div', { class: 'add' }, t('setup.page.home.template_saved')));
    } else if (pv.kind === 'undo') {
      parts.push(el('div', {}, t('setup.page.home.undo_lead')), ...roomLines(pv.changes));
      if (pv.state === 'preview') parts.push(el('div', { class: 'add' }, t('setup.page.home.undo_note')), save('setup.page.home.undo_save'));
      if (pv.state === 'saved') parts.push(el('div', { class: 'add' }, t('setup.page.home.undone')));
    } else if (pv.kind === 'rooms') {
      if (pv.state === 'saved') parts.push(el('div', { class: 'add' }, t('setup.page.home.rooms_saved')));
      parts.push(...roomLines(pv.changes));
      if (pv.state === 'preview') parts.push(el('div', { class: 'add' }, t('setup.page.home.rooms_note')), save('setup.page.home.save_rooms'));
    }
    if (pv.problem) parts.push(problem(pv.problem));
    if (pv.evicted && pv.evicted.length) parts.push(el('div', {}, t('setup.page.home.evicted', { names: pv.evicted })));
    if (pv.undo) parts.push(button(t('setup.page.home.undo'), 'undo', 'ghost btn-small', slot, `undo-${slot}`));
    return el('li', { class: 'pvrow' }, el('span'), el('div', { class: 'preview' }, parts), el('span'));
  };

  // The action that opens a preview; again after a problem.
  const previewButton = (slot, label, pv) =>
    (!pv || (pv.state === 'problem' && !pv.undo) ? button(t(label), 'preview', 'ghost btn-small', slot, `pv-${slot}`) : null);

  const insightRows = (hm) => {
    const rows = [];
    for (const it of hm.insights || []) {
      let title = '';
      let more = '';
      let slot = '';
      let label = '';
      const args = { name: it.name || '', value: it.value || '', count: it.count };
      switch (it.kind) {
        case 'battery_low':
          title = t('setup.page.home.battery_low', args);
          more = t('setup.page.home.battery_low_more', args);
          if (it.action === 'battery_reminder') [slot, label] = ['reminder', 'setup.page.home.remind'];
          break;
        case 'no_room':
          title = t('setup.page.home.no_room', args);
          more = t('setup.page.home.no_room_more');
          if (it.action === 'rooms') [slot, label] = ['rooms', 'setup.page.home.suggest'];
          break;
        case 'automation_unavailable':
          title = t('setup.page.home.automation_unavailable', args);
          more = t('setup.page.home.automation_unavailable_more');
          break;
        case 'device_offline':
          title = t('setup.page.home.device_offline', args);
          more = t('setup.page.home.device_offline_more');
          break;
        case 'automation_never_ran':
          title = t('setup.page.home.automation_never_ran', args);
          more = t('setup.page.home.automation_never_ran_more');
          break;
        default:
          continue;
      }
      const pv = slot ? (hm.previews || {})[slot] : null;
      rows.push(line(el('span', { class: 'ic todo', 'aria-hidden': 'true' }, '!'), title, more, null, slot ? previewButton(slot, label, pv) : null));
      if (pv) rows.push(previewBlock(slot, pv));
    }
    return rows;
  };

  const stats = (hm) => {
    const s = hm.stats;
    const tiles = [['rooms', 'setup.page.home.stat_rooms'], ['devices', 'setup.page.home.stat_devices'], ['lights', 'setup.page.home.stat_lights'],
      ['locks', 'setup.page.home.stat_locks'], ['automations', 'setup.page.home.stat_automations']];
    return el('ul', { class: 'stat' }, tiles.map(([name, key]) => {
      const n = s[name] || 0;
      const kind = name === 'locks' ? 'guard' : (name === 'automations' && n === 0 ? 'muted' : '');
      const hint = name === 'automations' && n === 0 && hm.automation ? el('span', { class: 'hint' }, t('setup.page.home.stat_hint')) : null;
      return el('li', { class: kind }, icon(icons[name]), el('b', {}, h.format(n)), t(key, { count: n }), hint);
    }));
  };

  const prompts = (hm) => {
    const phrases = [t('setup.page.home.prompt_on'), t('setup.page.home.prompt_kitchen'),
      (hm.stats && hm.stats.locks) ? t('setup.page.home.prompt_unlock') : t('setup.page.home.prompt_lights')];
    return el('div', { class: 'chips' }, phrases.map((text, i) => el('button', {
      id: `b-chip-${i}`, class: 'chip', type: 'button',
      onclick: () => {
        const copied = t(hm.claude ? 'setup.page.home.copied_claude' : 'setup.page.home.copied');
        (navigator.clipboard ? navigator.clipboard.writeText(text) : Promise.reject(new Error('no clipboard')))
          .then(() => toast(copied), () => toast(t('setup.page.home.copy_failed', { text })));
      },
    }, `“${text}”`)));
  };

  const home = () => {
    const hm = h.state().home || {};
    const out = [...head(t('setup.page.home.eyebrow'), t('setup.page.home.title'), null)];
    for (const note of hm.notes || []) out.push(el('p', { class: 'note' }, say(note)));
    if (hm.loading) out.push(el('ul', { class: 'rows' }, row('run', t('setup.page.home.loading'))));
    if (hm.failed) out.push(el('p', { class: 'problem' }, t('setup.page.home.failed')));
    if (hm.stats) out.push(stats(hm));
    const insights = insightRows(hm);
    if (insights.length) out.push(el('h2', {}, t('setup.page.home.worth')), el('ul', { class: 'rows' }, insights));
    if (hm.automation) {
      const pv = (hm.previews || {}).automation;
      const uses = (hm.automation.uses || []).filter((u) => u.role !== 'phone').map((u) => u.name);
      out.push(el('h2', {}, t('setup.page.home.made')), el('ul', { class: 'rows' },
        line(el('span', { class: 'ic ok', 'aria-hidden': 'true' }, '✦'), templateTitle(hm.automation), t('setup.page.home.uses', { names: uses }), null,
          previewButton('automation', 'setup.page.home.preview', pv)),
        previewBlock('automation', pv)));
    }
    if (!hm.loading) out.push(el('h2', {}, t('setup.page.home.ask')), prompts(hm));
    out.push(el('div', { class: 'actions' },
      hm.claude ? button(t('setup.page.home.open_claude'), 'claude') : null,
      button(t('setup.page.home.done'), 'done', hm.claude ? 'ghost' : 'primary')));
    if (hm.opened) out.push(el('p', { class: 'fine', role: 'status' }, t(hm.opened === 'terminal' ? 'setup.page.home.opened' : 'setup.page.home.manual')));
    out.push(el('p', { class: 'fine' }, t('setup.page.home.fine')));
    return out;
  };

  return { control, home };
};
