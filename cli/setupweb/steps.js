// HA NOVA setup page, steps 3 ("You stay in control") and 4 ("Here's your
// home, at a glance"); step 3's chat-app row is chat.js. app.js hands in
// its helpers; the CLI sends data only, and every text goes in as
// textContent. A save posts the id of the plan the
// page shows; the CLI saves only a plan it stored for that preview.
'use strict';

window.novaSteps = (h) => {
  const { t, el, row, head, button, link, render, claudeWhere } = h;
  const say = (msg) => (msg ? t(msg.key, msg.args || {}) : '');
  // Switches and open details live here until "Continue" sends them.
  // chatFlip: a re-run's chat-app switch turned the other way (Done applies
  // it); moving: "Use another phone" shows the phones to pick from.
  const local = { rule: true, link: false, others: false, moving: false, drafts: {}, chatAsk: false, chatFlip: false, chatSeen: '', how: {} };

  const focusId = (id) => { const node = document.getElementById(id); if (node) node.focus(); };
  const toast = (text) => {
    const node = el('div', { class: 'toast', role: 'status' });
    document.body.append(node);
    setTimeout(() => { node.textContent = text; }, 50); // filled after insertion, so it is announced
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

  // A row's status mark (style.css): on ✓ green, waiting a spinner, needs
  // you amber, off or optional muted with the row's own glyph.
  const mark = (status, glyph = '') => el('span', { class: `ic ${status}`, 'aria-hidden': 'true' }, glyph);
  const done = (problem) => (problem ? mark('fail', '!') : mark('ok', '✓')); // on, with a problem: the amber !
  const spinner = () => mark('run');
  const muted = (glyph) => mark('todo', glyph);
  const needs = () => mark('todo amber', '!'); // its own glyph: not by color alone
  // A row; vid names its value line for a switch's aria-describedby.
  const line = (glyph, key, value, below, side, vid) =>
    el('li', {}, glyph, el('div', {}, el('div', { class: 'k' }, key), value ? el('div', { class: 'v', ...(vid ? { id: vid } : {}) }, value) : null, below), side || el('span'));
  const toggle = (id, label, on, onclick, disabled) =>
    el('button', { id, class: 'switch', type: 'button', role: 'switch', 'aria-checked': String(on), 'aria-label': label, 'aria-describedby': `${id}-v`, onclick, ...(disabled ? { disabled: '' } : {}) });
  // Where focus goes when the clicked control is gone after a re-render
  // (app.js reads data-focus-after).
  const focusAfter = (ids) => ({ tabindex: '-1', 'data-focus-after': ids.join(' ') });
  const tagOn = () => el('span', { class: 'tag ok' }, t('setup.page.control.on'));
  // A switch applies on Done (or Continue) and says so under it. Buttons
  // act at once: the phone's notification, the chat-app question.
  const finishLabel = () => t(h.state().screen === 'rerun' ? 'setup.page.rerun.done' : 'setup.page.control.finish');
  const later = (key = 'setup.page.control.signin_later') => el('div', { class: 'v' }, t(key, { button: finishLabel() }));
  const flip = (name, said) => () => {
    local[name] = !local[name];
    render(true);
    if (local[name]) h.announce(said ? t(said) : t('setup.page.control.signin_later', { button: finishLabel() }));
  };
  const problem = (msg) => (msg ? el('p', { class: 'problem', role: 'alert' }, say(msg)) : null);

  // Step 3.
  const hint = (os) => t({ ios: 'setup.page.control.phone_nothing_ios', android: 'setup.page.control.phone_nothing_android' }[os] || 'setup.page.control.phone_nothing');
  const pickPhones = (phones) => el('div', { class: 'pick' }, phones.map((p, i) =>
    button(t('setup.page.control.phone_choice', { phone: p.name, person: p.person }), 'phone', 'ghost btn-small', p.id, `phone-${i}`)));
  const recheck = () => button(t('setup.page.button.recheck'), 'recheck', 'ghost btn-small', '', 'phone-recheck');
  const phoneRow = (c) => {
    const key = t('setup.page.control.phone_key');
    const phones = c.phones || [];
    if (c.phone !== 'linked') local.moving = false;
    // No tap after 30 s, or the ask timed out: where the phone turns on notifications.
    const asking = c.phone === 'waiting' || c.phone === 'moving' ? `${c.phone} ${c.phone_name}` : '';
    if (asking !== local.asking) {
      const n = (local.askN = (local.askN || 0) + 1);
      local.asking = asking;
      local.quiet = false;
      const os = c.phone === 'waiting' ? c.phone_os : ''; // moving asks two phones: the neutral line
      if (asking) setTimeout(() => { if (local.askN === n) { local.quiet = true; render(true); h.announce(hint(os)); } }, 30000);
    }
    const nothing = (os) => el('div', { class: 'v' }, hint(os));
    const quiet = (os) => (local.quiet ? nothing(os) : null);
    // A timeout says where notifications turn on, in the same alert.
    const phoneProblem = () => (c.phone_problem && c.phone_problem.key === 'setup.page.control.phone_timeout'
      ? el('p', { class: 'problem', role: 'alert' }, `${say(c.phone_problem)} ${hint(c.phone_os)}`) : problem(c.phone_problem));
    switch (c.phone) {
      case 'linked': {
        // Use another phone: the phone linked now OKs the move (confirm-phone's rule).
        const move = phones.length && h.accepts('phone');
        // Done, and what happens from now on; with the window, either answer counts.
        const value = t(c.window && !h.state().web ? 'setup.page.control.phone_value' : 'setup.page.control.phone_value_only', { phone: c.phone_name });
        if (move && local.moving) {
          return el('li', focusAfter(['b-phone-move']), done(), el('div', {}, el('div', { class: 'k' }, key), el('div', { class: 'v' }, value),
            el('div', { class: 'v' }, t('setup.page.control.phone_move_pick', { phone: c.phone_name })), pickPhones(phones),
            el('button', { id: 'b-phone-move-cancel', class: 'linkish', type: 'button', onclick: () => { local.moving = false; render(true); focusId('b-phone-move'); } }, t('setup.page.control.not_now'))), el('span'));
        }
        const side = move ? el('button', { id: 'b-phone-move', class: 'btn btn-ghost btn-small', type: 'button', onclick: () => { local.moving = true; render(true); focusId('b-phone-0'); } }, t('setup.page.control.phone_move')) : tagOn();
        return line(done(c.phone_problem), key, value, phoneProblem(), side);
      }
      case 'moving':
        return el('li', focusAfter(phones.map((p, i) => `b-phone-${i}`)), spinner(),
          el('div', {}, el('div', { class: 'k' }, key), el('div', { class: 'v', role: 'status' }, t('setup.page.control.phone_moving', { old: c.old_phone, phone: c.phone_name })), quiet('')), el('span'));
      case 'waiting':
        return el('li', focusAfter(['b-phone-0', ...phones.map((p, i) => `b-phone-${i}`)]), spinner(),
          el('div', {}, el('div', { class: 'k' }, key), el('div', { class: 'v', role: 'status' }, t({ ios: 'setup.page.control.phone_waiting_ios', android: 'setup.page.control.phone_waiting_android' }[c.phone_os] || 'setup.page.control.phone_waiting', { phone: c.phone_name })), quiet(c.phone_os)), el('span'));
      case 'none':
        return line(muted('·'), key, t('setup.page.control.phone_none'), el('div', { class: 'v' }, say(c.phone_why)), recheck());
      case 'unread': // the button is disabled while it reads again
        return line(muted('·'), key, t('setup.page.control.phone_unread'), null, recheck());
      case 'pick': {
        // The user's own phones first (c.own of them); someone else's only on an explicit click.
        const own = c.own || 0;
        const more = !local.others && phones.length > own
          ? el('button', { id: 'b-phone-other', class: 'linkish', type: 'button', onclick: () => { local.others = true; render(true); } }, t(own ? 'setup.page.control.phone_other' : 'setup.page.control.phone_someone'))
          : null;
        if (own === 1 && !local.others) { // a button: the notification goes out on this click
          return line(muted('◎'), key, t('setup.page.control.phone_offer', { phone: phones[0].name, person: phones[0].person }), [phoneProblem(), more],
            button(t('setup.page.control.phone_use'), 'phone', 'ghost btn-small', phones[0].id, 'phone-0'));
        }
        if (own === 0 && !local.others) return line(muted('◎'), key, say(c.phone_why), [phoneProblem(), more], recheck());
        return line(muted('◎'), key, t('setup.page.control.phone_pick'), [phoneProblem(), pickPhones(local.others ? phones : phones.slice(0, own)), more]);
      }
      default:
        return null; // can't be linked here: no row (confirm-phone settles it later)
    }
  };

  // Chat apps: their own file (chat.js), with these helpers and switches.
  const { chatRow, addClaude, claudeHelp, claudeNext } = window.novaChat({ ...h, say, local, toast, line, toggle, focusAfter, focusId, tagOn, flip, problem, mark, done, spinner, muted, needs });

  const control = (status, overview) => {
    const c = h.state().control || {};
    // Claude or ChatGPT in the browser: chat apps first, and the window on
    // this computer never asks for them. Here: chat apps are the first
    // optional item, right after the confirmations, as a card.
    const web = h.state().web;
    const chat = chatRow(c);
    const rows = [
      web ? chat : null,
      web ? null : line(c.window ? done() : muted('·'), t('setup.page.control.window_key'),
        t(c.window ? 'setup.page.control.window_value' : 'setup.page.control.window_off'), null, c.window ? tagOn() : null),
      phoneRow(c),
      web || !chat ? null : Object.assign(chat, { className: 'feature' }),
    ];
    const busy = !h.accepts('finish');
    if (c.rule === 'offer') {
      const label = t('setup.page.control.rule_key');
      rows.push(line(muted('⚙'), label, t('setup.page.control.rule_value', { count: c.rule_count }), local.rule ? later('setup.page.control.rule_later') : null,
        toggle('b-rule', label, local.rule, () => { local.rule = !local.rule; render(true); }, busy), 'b-rule-v'));
    } else if (c.rule === 'on') {
      rows.push(line(done(), t('setup.page.control.rule_key'), t('setup.page.control.rule_on', { count: c.rule_count }), null, tagOn()));
    }
    const ruleRows = rows.slice(4);
    let linkRow;
    if (c.link === 'offer') {
      const label = t('setup.page.control.link_key');
      rows.push(linkRow = line(muted('⇄'), label, t('setup.page.control.link_value'), local.link ? later() : null,
        toggle('b-link', label, local.link, flip('link'), busy), 'b-link-v'));
    } else if (c.link === 'run') {
      rows.push(linkRow = el('li', {}, spinner(), el('div', {}, el('div', { class: 'k' }, t('setup.page.control.link_key')),
        el('div', { class: 'v', role: 'status' }, t('setup.page.control.link_run'))), el('span')));
    } else if (c.link === 'on') {
      rows.push(linkRow = line(done(), t('setup.page.control.link_key'), t('setup.page.control.link_on'), null, tagOn()));
    }
    // An open chat consent is a question: Finish waits for its answer.
    const asking = local.chatAsk && c.chat && c.chat.state === 'offer';
    const choices = [];
    if (c.rule === 'offer' && local.rule) choices.push('rule');
    if (c.link === 'offer' && local.link) choices.push('link');
    if (c.chat && local.chatFlip && (c.chat.state === 'page_on' || c.chat.state === 'page_off')) choices.push(c.chat.state === 'page_on' ? 'chat_off' : 'chat_on');
    // A switch that signs in on the button says so on it too.
    const finish = choices.some((choice) => choice !== 'rule') ? t(status ? 'setup.page.rerun.done_signin' : 'setup.page.control.finish_signin') : finishLabel();
    return [...(status ? [...head(t('setup.page.rerun.eyebrow'), t('setup.page.rerun.title'), t('setup.page.rerun.lead')), overview ? null : status]
      : head(t('setup.page.control.eyebrow'), t('setup.page.control.title'), t('setup.page.control.lead'))),
      ...(overview ? overview({ window: rows[1], phone: rows[2], chat, link: linkRow }, ruleRows) : [el('ul', { class: 'rows tall' }, rows)]),
      el('div', { class: 'actions' }, asking
        ? el('button', { id: 'b-finish', class: 'btn btn-primary', type: 'button', disabled: '' }, finish)
        : button(finish, 'finish', 'primary', choices.join(','))),
      asking ? el('p', { class: 'fine' }, t('setup.chat.answer_first')) : null,
      c.phone === 'pick' && (c.own || local.others) ? el('p', { class: 'fine' }, t('setup.page.control.fine')) : null,
      status && h.state().accepts.includes('server') ? el('p', { class: 'fine' }, el('button', { id: 'b-server', class: 'linkish', type: 'button', onclick: () => h.post('server') }, t('setup.page.rerun.add_server'))) : null];
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
  const roomLines = (changes, key = 'setup.page.home.rooms_line') => {
    const groups = new Map();
    for (const c of changes || []) {
      const area = c.area || t('setup.page.home.no_area');
      if (!groups.has(area)) groups.set(area, []);
      groups.get(area).push(c.name);
    }
    return [...groups].map(([area, devices]) => el('div', {}, t(key, { area, devices })));
  };
  const saveLabels = { rooms: 'setup.page.home.save_rooms', reminder: 'setup.page.home.save_reminder', automation: 'setup.page.home.save_automation' };

  const previewBlock = (slot, pv) => {
    if (!pv) return null;
    const parts = [];
    const save = (key) => button(t(key), 'save', 'primary btn-small', pv.plan_id, `save-${slot}`);
    if (pv.state === 'loading' || pv.state === 'saving') parts.push(el('div', { class: 'busy' }, spinner()));
    if (pv.notice) parts.push(el('div', { class: 'add' }, say(pv.notice)));
    const ok = pv.state === 'saved'; // unverified: saved, but no ✓
    if (pv.kind === 'template') {
      parts.push(...summary(pv));
      if (pv.draft) {
        const details = el('details', {}, el('summary', {}, t('setup.page.home.draft')), el('pre', {}, pv.draft));
        details.open = Boolean(local.drafts[slot]);
        details.addEventListener('toggle', () => { local.drafts[slot] = details.open; });
        parts.push(details);
      }
      if (pv.state === 'preview') parts.push(el('div', { class: 'add' }, t('setup.page.home.template_note', { button: t(saveLabels[slot]) })), save(saveLabels[slot]));
      if (ok) parts.push(el('div', { class: 'add' }, t('setup.page.home.template_saved')));
    } else if (pv.kind === 'undo') {
      parts.push(el('div', {}, t('setup.page.home.undo_lead')), ...roomLines(pv.changes, 'setup.page.home.undo_line'));
      if (pv.state === 'preview') parts.push(el('div', { class: 'add' }, t('setup.page.home.undo_note')), save('setup.page.home.undo_save'));
      if (ok) parts.push(el('div', { class: 'add' }, t('setup.page.home.undone')));
    } else if (pv.kind === 'rooms') {
      if (ok) parts.push(el('div', { class: 'add' }, t('setup.page.home.rooms_saved')));
      parts.push(...roomLines(pv.changes));
      if (pv.state === 'preview') parts.push(el('div', { class: 'add' }, t('setup.page.home.rooms_note')), save('setup.page.home.save_rooms'));
    }
    if (pv.problem) parts.push(problem(pv.problem));
    if (pv.evicted && pv.evicted.length) parts.push(el('div', {}, t('setup.page.home.evicted', { names: pv.evicted })));
    if (pv.undo && pv.state !== 'loading' && pv.state !== 'saving') parts.push(button(t('setup.page.home.undo'), 'undo', 'ghost btn-small', slot, `undo-${slot}`));
    return el('li', { class: 'pvrow' }, el('span'),
      el('div', { class: 'preview', id: `pv-panel-${slot}`, ...focusAfter([`b-pv-${slot}`, `b-save-${slot}`, `b-undo-${slot}`]) }, parts), el('span'));
  };

  // Results and problems are said once, in the page's live region.
  const announced = {};
  const announce = (hm) => {
    for (const [slot, pv] of Object.entries(hm.previews || {})) {
      const key = `${pv.state} ${pv.kind || ''} ${pv.problem ? pv.problem.key : ''}`;
      if (announced[slot] === key) continue;
      announced[slot] = key;
      const words = { saved: { template: 'setup.page.home.template_saved', undo: 'setup.page.home.undone', rooms: 'setup.page.home.rooms_saved' } }[pv.state];
      const text = pv.problem ? say(pv.problem) : (words && words[pv.kind] ? t(words[pv.kind]) : '');
      if (text) h.announce(text);
    }
  };

  // The action that opens a preview; again after a problem.
  const previewButton = (slot, label, pv) =>
    (!pv || (pv.state === 'problem' && pv.retry) ? button(t(label), 'preview', 'ghost btn-small', slot, `pv-${slot}`) : null);

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
          title = t(it.count === 0 ? 'setup.page.home.no_room_none' : 'setup.page.home.no_room', args);
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
      const kind = name === 'automations' && n === 0 ? 'muted' : '';
      const hint = name === 'automations' && n === 0 && hm.automation ? el('span', { class: 'hint' }, t('setup.page.home.stat_hint')) : null;
      return el('li', { class: kind }, icon(icons[name]), el('b', {}, h.format(n)), t(key, { count: n }), hint);
    }));
  };

  // Copyable phrases: a tap copies one.
  const chips = (phrases, claude) => el('div', { class: 'chips' }, phrases.map((text, i) => el('button', {
    id: `b-chip-${i}`, class: 'chip', type: 'button',
    onclick: () => {
      const copied = t(claude ? 'setup.page.home.copied_claude' : 'setup.page.home.copied');
      (navigator.clipboard ? navigator.clipboard.writeText(text) : Promise.reject(new Error('no clipboard')))
        .then(() => toast(copied), () => toast(t('setup.page.home.copy_failed', { text })));
    },
  }, t('setup.page.home.chip', { text }))));
  const prompts = (hm) => chips([t('setup.page.home.prompt_on'), t('setup.page.home.prompt_kitchen'),
    (hm.stats && hm.stats.locks) ? t('setup.page.home.prompt_unlock') : t('setup.page.home.prompt_lights')], hm.claude);

  // `ha-nova try` from step 1: the shared demo house is connected.
  const demo = () => [...head(t('setup.page.demo.eyebrow'), t('setup.page.demo.title'), t('setup.page.demo.lead')),
    chips([t('setup.page.demo.prompt_used'), t('setup.page.demo.prompt_kitchen'), t('setup.page.demo.prompt_unlock')],
      (h.state().clients || []).includes('Claude Code')),
    // The demo's own Home Assistant, to watch next to the chat (setup_flow_page.go).
    el('div', { class: 'actions' }, link(t('setup.page.demo.watch'), (h.state().links || {}).demo, 'ghost btn-small', 'demo-watch')),
    el('p', { class: 'fine' }, t('setup.page.demo.fine'))];

  const home = () => {
    const hm = h.state().home || {};
    const out = [...head(t('setup.page.home.eyebrow'), t('setup.page.home.title'), null)];
    for (const note of hm.notes || []) out.push(h.note(note));
    if (hm.chat_link) out.push(el('div', { class: 'actions addclaude' }, addClaude(hm.chat_link, 'ghost btn-small', 'chat-claude-4')),
      ...claudeHelp(hm.org_link, 'chat-claude-org-4', 'setup.chat.no_form_above'), claudeNext(hm.chat_phone, 'chat-next-4'));
    if (hm.loading) out.push(el('ul', { class: 'rows' }, row('run', t('setup.page.home.loading'))));
    if (hm.failed) out.push(el('p', { class: 'problem' }, t('setup.page.home.failed')));
    if (hm.stats) out.push(stats(hm));
    const insights = insightRows(hm);
    if (insights.length) out.push(el('h2', {}, t('setup.page.home.worth')), el('ul', { class: 'rows tall' }, insights));
    if (hm.automation) {
      const pv = (hm.previews || {}).automation;
      const uses = (hm.automation.uses || []).filter((u) => u.role !== 'phone').map((u) => u.name);
      out.push(el('h2', {}, t('setup.page.home.made')), el('ul', { class: 'rows tall' },
        line(el('span', { class: 'ic ok', 'aria-hidden': 'true' }, '✦'), templateTitle(hm.automation), t('setup.page.home.uses', { names: uses }), null,
          previewButton('automation', 'setup.page.home.preview', pv)),
        previewBlock('automation', pv)));
    }
    if (!hm.loading) out.push(el('h2', {}, t('setup.page.home.ask')), prompts(hm));
    if (hm.claude) out.push(claudeWhere(['Claude Code']));
    out.push(el('div', { class: 'actions' },
      hm.claude ? button(t('setup.page.home.open_claude'), 'claude') : null,
      button(t('setup.page.home.done'), 'done', hm.claude ? 'ghost' : 'primary')));
    if (hm.opened) out.push(el('p', { class: 'fine', role: 'status' }, t(hm.opened === 'terminal' ? 'setup.page.home.opened' : 'setup.page.home.manual')));
    out.push(el('p', { class: 'fine' }, t('setup.page.home.fine')));
    announce(hm);
    return out;
  };

  // A line to copy, with its Copy button (as chat.js's address).
  const copybar = (text, id, copied, failed) => el('div', { class: 'copybar' }, el('code', { id }, text),
    el('button', { id: `b-${id}-copy`, class: 'btn btn-ghost btn-small', type: 'button', onclick: () => (navigator.clipboard ? navigator.clipboard.writeText(text) : Promise.reject(new Error('no clipboard')))
      .then(() => toast(t(copied)), () => toast(t(failed))) }, t('setup.chat.copy')));

  return { control, home, demo, copybar };
};
