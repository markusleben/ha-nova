// HA NOVA setup page on a re-run (spec §4): what is set up, then step 3's
// rows (steps.js control) on the same screen. app.js hands in its helpers;
// the CLI sends data only, and every text goes in as textContent.
'use strict';

window.novaRerun = (h) => {
  const { t, el, row, head, note, button, link, control, claudeWhere, codexWhere } = h;
  const say = (msg) => (msg ? t(msg.key, msg.args || {}) : '');
  const problem = (msg) => (msg ? el('p', { class: 'problem', role: 'alert' }, say(msg)) : null);
  const opened = {};
  const overviewRow = (controls) => (item) => {
    const s = h.state();
    let side;
    let below = [claudeWhere([item.label]), codexWhere([item.label])];
    if (item.action === 'clients' || item.action === 'window') {
      side = button(t('setup.overview.setup'), item.action, 'ghost btn-small', item.value, `overview-${item.id}`);
    } else if (item.action === 'nova') {
      side = link(t('setup.overview.check'), (s.links || {}).nova, 'ghost btn-small', 'overview-nova');
    } else if (controls[item.action]) {
      const details = el('details', {}, el('summary', { id: `overview-${item.id}` }, t(item.state === 'ok' ? 'setup.overview.manage' : 'setup.overview.setup')),
        el('ul', { class: 'rows' }, controls[item.action]));
      const chat = (s.control || {}).chat || {};
      const active = item.id === 'chat' ? chat.ask || ['run', 'signin', 'offer'].includes(chat.state) : item.id === 'phone' && ['waiting', 'moving'].includes((s.control || {}).phone);
      details.open = active || (opened[item.id] === undefined ? item.id === 'chat' && ['on', 'page_on'].includes(chat.state) : opened[item.id]);
      details.addEventListener('toggle', () => { opened[item.id] = details.open; });
      below = details;
    } else if (item.snippet) {
      below = h.copybar(item.snippet, 'other-mcp', 'setup.chat.copied', 'setup.chat.copy_failed');
      below.querySelector('code').setAttribute('style', 'white-space: pre-wrap');
    }
    return row(item.state === 'unknown' ? 'todo' : item.state, item.label, say(item.msg), { side, below });
  };
  // Grouped under plain headings; apps not found here fold under "More AI apps".
  const overview = (home) => (controls, rules) => {
    const items = h.state().overview || [];
    const rowOf = overviewRow(controls);
    // Important open items, on top: a click opens the row and scrolls there.
    const open = items.filter((item) => ['phone', 'chat_phone'].includes(item.id) && item.state === 'todo');
    const go = (item) => () => {
      if (item.id === 'phone') { opened.phone = true; h.render(true); }
      const node = document.getElementById(item.id === 'phone' ? 'overview-phone' : 'overview-nova');
      if (node) { node.scrollIntoView({ block: 'center', behavior: 'smooth' }); node.focus(); }
    };
    const attention = open.length ? el('div', { class: 'attention', role: 'note' }, el('strong', {}, t('setup.overview.attention')),
      el('ul', {}, open.map((item) => el('li', {}, el('button', { id: `b-attention-${item.id}`, class: 'linkish', type: 'button', onclick: go(item) }, `${item.label}: ${say(item.msg)}`))))) : null;
    return [attention, ...[['home', t('setup.overview.section_home')], ['apps', t('setup.overview.section_apps')],
      ['confirm', t('setup.overview.section_confirm')], ['chat', t('setup.overview.section_chat')]].flatMap(([section, heading]) => {
      const mine = items.filter((item) => item.section === section);
      const more = mine.filter((item) => item.more);
      const fold = more.length ? row('todo', t('setup.overview.more_label'), t('setup.overview.more', { apps: more.map((item) => item.label).join(', ') }),
        { below: el('details', {}, el('summary', { id: 'overview-more' }, t('setup.overview.show')), el('ul', { class: 'rows sub' }, more.map(rowOf))) }) : null;
      const rows = section === 'home' ? [...home, ...mine.map(rowOf)]
        : section === 'apps' ? [...mine.filter((item) => !item.more && item.id !== 'other').map(rowOf), ...rules, fold, ...mine.filter((item) => item.id === 'other').map(rowOf)]
          : mine.map(rowOf);
      return rows.some(Boolean) ? [el('h2', {}, heading), el('ul', { class: 'rows tall' }, rows)] : [];
    })];
  };

  // A re-run (spec §4): what is set up. All fine (rerun): step 3's rows on
  // the same screen, and AI assistants found since get a button. Else
  // (rerun_fix) Fix it; while the NOVA App doesn't answer, Check again first,
  // and a Home Assistant found at another address.
  // (rerun_switch) Still on the old shared access: Switch to your own
  // access, or Not now.
  const statusKeys = { ha: 'setup.page.ha.key', app: 'setup.page.app.key', computer: 'setup.page.rerun.computer_key', clients: 'setup.page.clients.key' };
  const rerun = () => {
    const s = h.state();
    const links = s.links || {};
    const fresh = s.new_clients || [];
    const side = (r) => {
      if (s.control && r.id === 'app') return { side: link(t('setup.page.link.app'), links.app, 'ghost btn-small', 'app') }; // needs an update
      if (r.id !== 'clients') return null;
      const okClients = r.state === 'ok' ? ((r.msg || {}).args || {}).clients : null;
      const where = [claudeWhere(okClients), codexWhere(okClients)];
      const add = fresh.length && s.accepts.includes('clients')
        ? el('div', { class: 'actions' }, button(t('setup.page.rerun.add_clients', { clients: fresh }), 'clients', 'ghost btn-small')) : null;
      return { below: [...where, add] };
    };
    const statusRows = (s.rows || []).filter((r) => !s.overview || r.id !== 'clients').map((r) => row(r.state, t(statusKeys[r.id]), say(r.msg), side(r)));
    const status = el('ul', { class: 'rows' }, statusRows);
    if (s.control) return control(status, s.overview ? overview(statusRows) : null);
    if (s.screen === 'rerun_switch') { // still on the old shared access: the switch comes first
      return [...head(t('setup.page.rerun.eyebrow'), t('setup.page.rerun.title_switch'), t('setup.page.rerun.lead_switch'), true), status,
        el('div', { class: 'actions' }, button(t('setup.page.rerun.switch'), 'switch'), button(t('setup.page.rerun.later'), 'terminal', 'ghost'))];
    }
    // Home Assistant doesn't answer, but one was found at another address:
    // offered first, as on a first run; the click goes on to step 1.
    const found = s.candidates || [];
    const several = found.length > 1;
    const away = found.length && s.problem && s.problem.key === 'setup.page.rerun.problem_app' ? { key: 'setup.page.rerun.problem_found' } : s.problem;
    const offer = found.length ? [
      el('p', { class: 'note', role: 'status' }, several ? t('setup.page.rerun.found_several') : t('setup.page.rerun.found_one', { host: found[0].host })),
      el('div', { class: 'actions offer' }, found.map((c, i) => button(several ? c.host : t('setup.page.rerun.use_found'), 'address', several ? 'ghost' : 'primary', c.url, `pick-${i}`)))] : [];
    return [...head(t('setup.page.rerun.eyebrow'), t('setup.page.rerun.title_fix'), t('setup.page.rerun.lead_fix'), true),
      problem(away), ...offer, status,
      el('div', { class: 'actions' },
        s.problem ? button(t('setup.page.button.recheck'), 'recheck', found.length || s.fix === 'app_update' ? 'ghost' : 'primary') : null,
        s.fix === 'app_update' ? button(t('setup.page.rerun.update_app'), 'fix', 'primary')
          : button(t('setup.page.rerun.fix'), 'fix', s.problem ? 'ghost' : 'primary'),
        link(t('setup.page.link.app'), links.app, 'ghost', 'app')),
      s.accepts.includes('change') ? el('p', { class: 'fine' }, el('button', { id: 'b-change-ha', class: 'linkish', type: 'button', onclick: () => h.post('change') }, t('setup.page.rerun.change_ha'))) : null];
  };
  // Fix it, or the skills refresh, while the usual steps run.
  const repair = () => [...head(t('setup.page.rerun.eyebrow'), t('setup.page.rerun.repair_title'), t('setup.page.connect.lead_signin')),
    el('ul', { class: 'rows' }, (h.state().rows || []).map((r) => row(r.state, say(r.msg))))];
  // How a re-run ended: what Done changed.
  const saved = () => [...head(t('setup.page.saved.eyebrow'), t('setup.page.saved.title'), t('setup.page.saved.lead')),
    ...((h.state().home || {}).notes || []).map(note)];

  return { rerun, rerun_fix: rerun, rerun_switch: rerun, repair, saved };
};
