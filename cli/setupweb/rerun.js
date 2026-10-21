// HA NOVA setup page on a re-run (spec §4): what is set up, then step 3's
// rows (steps.js control) on the same screen. app.js hands in its helpers;
// the CLI sends data only, and every text goes in as textContent.
'use strict';

window.novaRerun = (h) => {
  const { t, el, row, head, button, link, control } = h;
  const say = (msg) => (msg ? t(msg.key, msg.args || {}) : '');
  const problem = (msg) => (msg ? el('p', { class: 'problem', role: 'alert' }, say(msg)) : null);

  // A re-run (spec §4): what is set up. All fine (rerun): step 3's rows on
  // the same screen, and AI assistants found since get a button. Else
  // (rerun_fix) Fix it; while the NOVA App doesn't answer, Check again first.
  const statusKeys = { ha: 'setup.page.ha.key', app: 'setup.page.app.key', computer: 'setup.page.rerun.computer_key', clients: 'setup.page.clients.key' };
  const rerun = () => {
    const s = h.state();
    const links = s.links || {};
    const fresh = s.new_clients || [];
    const side = (r) => {
      if (s.control && r.id === 'app') return { side: link(t('setup.page.link.app'), links.app, 'ghost btn-small', 'app') }; // needs an update
      if (r.id === 'clients' && fresh.length && s.accepts.includes('clients')) {
        return { below: el('div', { class: 'actions' }, button(t('setup.page.rerun.add_clients', { clients: fresh }), 'clients', 'ghost btn-small')) };
      }
      return null;
    };
    const status = el('ul', { class: 'rows' }, (s.rows || []).map((r) => row(r.state, t(statusKeys[r.id]), say(r.msg), side(r))));
    if (s.control) return control(status);
    return [...head(t('setup.page.rerun.eyebrow'), t('setup.page.rerun.title_fix'), t('setup.page.rerun.lead_fix'), true),
      problem(s.problem), status,
      el('div', { class: 'actions' },
        s.problem ? button(t('setup.page.button.recheck'), 'recheck') : null,
        s.fix === 'app_update' ? button(t('setup.page.rerun.update_app'), 'fix', 'primary')
          : button(t('setup.page.rerun.fix'), 'fix', s.problem ? 'ghost' : 'primary'),
        link(t('setup.page.link.app'), links.app, 'ghost', 'app'))];
  };
  // Fix it, or the skills refresh, while the usual steps run.
  const repair = () => [...head(t('setup.page.rerun.eyebrow'), t('setup.page.rerun.repair_title'), t('setup.page.connect.lead_signin'), true),
    el('ul', { class: 'rows' }, (h.state().rows || []).map((r) => row(r.state, say(r.msg), null, null, true)))];
  // How a re-run ended: what Done changed.
  const saved = () => [...head(t('setup.page.saved.eyebrow'), t('setup.page.saved.title'), t('setup.page.saved.lead')),
    ...((h.state().home || {}).notes || []).map((note) => el('p', { class: 'note' }, say(note)))];

  return { rerun, rerun_fix: rerun, repair, saved };
};
