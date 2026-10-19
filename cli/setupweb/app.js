// HA NOVA setup page. The CLI sends data only (screen, rows, catalog keys and
// their values, Home Assistant names); this script words them from the string
// catalog and formats numbers and lists. Every text goes in as textContent,
// never as HTML.
'use strict';

(() => {
  // Words follow the catalog's language (plural forms, "and" in lists);
  // numbers follow the browser's locale.
  const language = document.documentElement.lang || 'en';
  const plurals = new Intl.PluralRules(language);
  const numbers = new Intl.NumberFormat(navigator.language || language);
  const lists = typeof Intl.ListFormat === 'function' ? new Intl.ListFormat(language, { type: 'conjunction' }) : null;
  const screenEl = document.getElementById('screen');
  const dotsEl = document.getElementById('dots');
  const statusEl = document.getElementById('status');
  const finalScreens = ['done', 'closed', 'incomplete', 'finished', 'failed', 'stopped', 'idle', 'lost', 'elsewhere'];
  const steps = { looking: 1, found: 1, noclient: 1, connect: 2, control: 3, home: 4, done: 4, closed: 4, incomplete: 4 };
  const downloads = {
    claude_desktop: 'https://claude.com/download',
    claude_code: 'https://claude.com/claude-code',
  };
  const primaryLabels = { install: 'setup.page.button.install', update: 'setup.page.button.update', signin: 'setup.page.button.signin' };
  const fixLabels = { retry: 'setup.page.button.retry', owner: 'setup.page.button.owner' };
  const rowStatus = { ok: 'setup.page.status.ok', run: 'setup.page.status.run', fail: 'setup.page.status.fail', todo: 'setup.page.status.todo' };
  const endScreens = {
    terminal: ['setup.page.terminal.eyebrow', 'setup.page.terminal.title', 'setup.page.terminal.lead'],
    incomplete: ['setup.page.incomplete.eyebrow', 'setup.page.incomplete.title', 'setup.page.incomplete.lead', true],
    finished: ['setup.page.finished.eyebrow', 'setup.page.finished.title', 'setup.page.done.fine'],
    closed: ['setup.page.closed.eyebrow', 'setup.page.closed.title', 'setup.page.closed.lead'],
    failed: ['setup.page.failed.eyebrow', 'setup.page.failed.title', 'setup.page.failed.lead', true],
    stopped: ['setup.page.stopped.eyebrow', 'setup.page.stopped.title', 'setup.page.stopped.lead', true],
    idle: ['setup.page.idle.eyebrow', 'setup.page.idle.title', 'setup.page.idle.lead', true],
    lost: ['setup.page.lost.eyebrow', 'setup.page.lost.title', 'setup.page.lost.lead'],
    elsewhere: ['setup.page.elsewhere.eyebrow', 'setup.page.elsewhere.title', 'setup.page.elsewhere.lead'],
  };
  let catalog = {};
  let state = null;
  let shown = '';
  let lastScreen = '';
  let lastStep = 0;
  let draft = '';
  let editing = false;
  let posting = false;
  let failures = 0;
  let timer = 0;

  const format = (value) => {
    if (Array.isArray(value)) return lists ? lists.format(value.map(String)) : value.join(', ');
    if (typeof value === 'number') return numbers.format(value);
    return String(value);
  };

  // t words a catalog key; {name} placeholders take the values, plural
  // entries pick the CLDR form for {count}.
  const t = (key, args = {}) => {
    let entry = catalog[key];
    if (entry === undefined) return key;
    if (typeof entry === 'object') entry = entry[plurals.select(args.count)] || entry.other;
    return entry.replace(/\{([a-z_]+)\}/g, (match, name) => (name in args ? format(args[name]) : match));
  };
  const say = (msg) => (msg ? t(msg.key, msg.args || {}) : '');

  const el = (tag, props = {}, ...children) => {
    const node = document.createElement(tag);
    for (const [name, value] of Object.entries(props)) {
      if (name === 'class') node.className = value;
      else if (name === 'onclick') node.addEventListener('click', value);
      else node.setAttribute(name, value);
    }
    for (const child of children.flat()) {
      if (child === null || child === undefined || child === false) continue;
      node.append(typeof child === 'string' ? document.createTextNode(child) : child);
    }
    return node;
  };

  const safeURL = (value) => (/^https?:\/\//i.test(value || '') ? value : null);
  const focus = (id) => {
    const node = document.getElementById(id);
    if (node) node.focus();
  };

  const post = async (action, value) => {
    if (posting) return;
    posting = true;
    render(true);
    try {
      await fetch('/api/action', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify(value === undefined ? { action } : { action, value }),
      });
    } catch (_) { /* the next poll shows where setup stands */ }
    posting = false;
    schedule(0);
  };

  const accepts = (action) => state.accepts.includes(action) && !posting;
  // Buttons carry stable ids, so focus survives a re-render.
  const button = (label, action, kind = 'primary', value, id = action) =>
    el('button', { id: `b-${id}`, class: `btn btn-${kind}`, type: 'button', onclick: () => post(action, value), ...(accepts(action) ? {} : { disabled: '' }) }, label);
  const link = (label, href, kind, id) =>
    safeURL(href) ? el('a', { id: `l-${id}`, class: `btn btn-${kind}`, href, target: '_blank', rel: 'noopener noreferrer' }, label) : null;
  const icon = (status, amber) => {
    const glyph = { ok: '✓', fail: '!', todo: '·', run: '' }[status];
    return el('span', { class: `ic ${status}${amber && status === 'run' ? ' amber' : ''}`, 'aria-hidden': 'true' }, glyph);
  };
  const row = (status, key, value, extra, amber) =>
    el('li', {}, icon(status, amber),
      el('div', {}, el('div', { class: 'k' }, el('span', { class: 'visually-hidden' }, `${t(rowStatus[status])} `), key),
        value ? el('div', { class: 'v' }, value) : null, extra && extra.below),
      extra && extra.side ? extra.side : el('span'));
  const head = (eyebrow, title, lead, amber) => [
    el('p', { class: amber ? 'eyebrow amber' : 'eyebrow' }, eyebrow),
    el('h1', { tabindex: '-1' }, title),
    lead && !state.problem ? el('p', { class: 'lead' }, lead) : null,
  ];

  const addressField = () => {
    const input = el('input', { type: 'text', id: 'ha-address', autocomplete: 'off', spellcheck: 'false', 'aria-label': t('setup.page.ha.field'), placeholder: t('setup.page.ha.placeholder') });
    input.value = draft;
    input.addEventListener('input', () => { draft = input.value; });
    const submit = el('button', { id: 'b-address-submit', class: 'btn btn-ghost btn-small', type: 'submit', ...(accepts('address') ? {} : { disabled: '' }) }, t('setup.page.ha.connect'));
    const form = el('form', { class: 'field' }, input, submit);
    form.addEventListener('submit', (event) => {
      event.preventDefault();
      const value = draft.trim();
      if (value && accepts('address')) post('address', value);
    });
    return form;
  };

  const problemLine = () => (state.problem ? el('p', { class: 'problem', role: 'alert' }, say(state.problem)) : null);

  const screens = {
    looking: () => [...head(t('setup.page.looking.eyebrow'), t('setup.page.looking.title'), t('setup.page.looking.lead')),
      el('ul', { class: 'rows' }, row('run', t('setup.page.ha.key')))],

    noclient: () => [...head(t('setup.page.noclient.eyebrow'), t('setup.page.noclient.title'), t('setup.page.noclient.lead')),
      el('div', { class: 'actions' },
        link(t('setup.page.link.claude_desktop'), downloads.claude_desktop, 'ghost', 'desktop'),
        link(t('setup.page.link.claude_code'), downloads.claude_code, 'ghost', 'code'),
        button(t('setup.page.button.recheck'), 'recheck'))],

    found: () => {
      const haKnown = Boolean(state.ha);
      const several = (state.candidates || []).length > 1;
      const asking = !haKnown || several || editing || Boolean(state.ask_ha);
      let haRow;
      if (!asking) {
        haRow = row('ok', t('setup.page.ha.key'), state.ha, {
          side: el('button', { id: 'b-change', class: 'btn btn-ghost btn-small', type: 'button', onclick: () => { editing = true; render(true); focus('ha-address'); } }, t('setup.page.ha.change')),
        });
      } else {
        const pick = several ? el('div', { class: 'pick' }, state.candidates.map((c, i) => button(c.host, 'address', 'ghost btn-small', c.url, `pick-${i}`))) : null;
        haRow = row('todo', t(several ? 'setup.page.ha.several' : 'setup.page.ha.key'), null, { below: [pick, addressField()] });
      }
      const clients = state.clients || [];
      const clientRow = row('ok', t('setup.page.clients.key'), format(clients), {
        side: el('span', { class: 'tag cyan' }, t('setup.page.clients.tag', { count: clients.length })),
      });
      let appRow;
      if (!state.app || asking) appRow = row('todo', t('setup.page.app.key'), t('setup.page.app.unknown'));
      else if (state.app === 'running') appRow = row('ok', t('setup.page.app.key'), state.app_version ? t('setup.page.app.running_version', { version: state.app_version }) : t('setup.page.app.running'));
      else appRow = row('todo', t('setup.page.app.key'), t(state.app === 'old' ? 'setup.page.app.old' : 'setup.page.app.missing'), { side: el('span', { class: 'tag amber' }, t('setup.page.app.next')) });

      const primary = asking ? '' : state.primary;
      const actions = el('div', { class: 'actions' },
        primary ? button(t(primaryLabels[primary]), 'signin') : null,
        button(t(primary ? 'setup.page.button.code' : 'setup.page.button.terminal'), 'terminal', primary || asking ? 'ghost' : 'primary'));
      let fine = [];
      if (primary) {
        fine = [t(primary === 'signin' ? 'setup.page.found.fine_signin' : 'setup.page.found.fine_install'),
          t('setup.page.found.fine_access', { client: `${location.origin}/ha-nova` })];
      } else if (!asking && state.app) {
        fine = [t('setup.page.found.fine_terminal')];
      }
      let lead = haKnown ? 'setup.page.found.lead' : 'setup.page.found.lead_ask';
      if (several) lead = 'setup.page.found.lead_several';
      return [...head(t('setup.page.found.eyebrow'), t(haKnown ? 'setup.page.found.title' : 'setup.page.found.title_ask'), t(lead)),
        problemLine(),
        el('ul', { class: 'rows' }, haRow, clientRow, appRow),
        actions,
        ...fine.map((line) => el('p', { class: 'fine' }, line))];
    },

    connect: () => {
      const rows = state.rows || [];
      const installs = rows.some((r) => r.id === 'app_add');
      const waiting = safeURL(state.signin_url);
      const items = rows.map((r) => row(r.state, say(r.msg), null, null, r.id.startsWith('app_')));
      const actions = [];
      if (waiting) {
        actions.push(el('p', { class: 'fine' }, t('setup.page.signin.waiting')),
          el('div', { class: 'actions' },
            el('a', { id: 'l-signin', class: 'btn btn-primary', href: waiting }, t('setup.page.signin.open')),
            el('button', { id: 'b-stop', class: 'btn btn-ghost', type: 'button', onclick: () => post('stop') }, t('setup.page.signin.stop'))));
      } else if (state.accepts.length) {
        const links = state.links || {};
        actions.push(el('div', { class: 'actions' },
          fixLabels[state.fix] ? button(t(fixLabels[state.fix]), 'signin') : null,
          link(t('setup.page.link.guide'), links.guide, fixLabels[state.fix] ? 'ghost' : 'primary', 'guide'),
          link(t('setup.page.link.app'), links.app, 'ghost', 'app'),
          button(t('setup.page.button.code'), 'terminal', 'ghost')));
      }
      return [...head(t('setup.page.connect.eyebrow'), t('setup.page.connect.title'), t(installs ? 'setup.page.connect.lead' : 'setup.page.connect.lead_signin'), true),
        problemLine(), el('ul', { class: 'rows' }, items), ...actions];
    },

    done: () => [...head(t('setup.page.done.eyebrow'), t('setup.page.done.title'), t('setup.page.done.lead', { clients: state.clients || [] })),
      el('p', { class: 'fine' }, t('setup.page.done.fine'))],
  };
  // Steps 3 and 4 (steps.js).
  Object.assign(screens, window.novaSteps({ t, el, row, head, button, format, render, post, accepts, state: () => state }));
  for (const [screen, [eyebrow, title, lead, amber]] of Object.entries(endScreens)) {
    screens[screen] = () => head(t(eyebrow), t(title), t(lead), amber);
  }

  const renderDots = (screen) => {
    const step = steps[screen] || (screen === 'terminal' ? lastStep : 0);
    lastStep = step || lastStep;
    dotsEl.replaceChildren();
    if (!step) return;
    for (let i = 1; i <= 4; i++) dotsEl.append(el('b', { class: i <= step ? 'on' : '', 'aria-hidden': 'true' }));
    dotsEl.append(el('span', {}, t('setup.page.progress', { step, total: 4 })));
  };

  function render(force) {
    const key = JSON.stringify(state) + posting + editing;
    if (!force && key === shown) return;
    shown = key;
    const focused = document.activeElement && document.activeElement.id;
    const changed = state.screen !== lastScreen;
    screenEl.classList.toggle('enter', changed);
    lastScreen = state.screen;
    renderDots(state.screen);
    screenEl.replaceChildren(...(screens[state.screen] || screens.failed)().filter(Boolean));
    if (changed) {
      // A new screen: say its title once and move focus to it.
      const title = screenEl.querySelector('h1');
      statusEl.textContent = title ? title.textContent : '';
      if (title) title.focus({ preventScroll: true });
    } else if (focused) {
      focus(focused);
    }
  }

  // The sign-in opens in this tab once per sign-in URL; Back from Home
  // Assistant shows the waiting screen instead of a loop.
  const followSignIn = () => {
    const target = safeURL(state.signin_url);
    if (!target) return;
    let opened = null;
    try { opened = sessionStorage.getItem('nova-signin'); } catch (_) { /* private mode */ }
    if (opened === target) return;
    try { sessionStorage.setItem('nova-signin', target); } catch (_) { /* private mode */ }
    location.assign(target);
  };

  const show = (screen) => {
    state = { screen, accepts: [] };
    render();
  };

  function schedule(delay) {
    clearTimeout(timer);
    timer = setTimeout(poll, delay);
  }

  async function poll() {
    let response;
    try {
      response = await fetch('/api/state', { cache: 'no-store', credentials: 'same-origin' });
    } catch (_) {
      response = null;
    }
    if (response && response.status === 401) {
      show('elsewhere');
      return;
    }
    if (!response || !response.ok) {
      // Setup ended or stopped after this page last heard from it; the
      // terminal has the outcome.
      if (state && finalScreens.includes(state.screen)) return;
      if (++failures >= 3) {
        show('lost');
        return;
      }
      schedule(1000);
      return;
    }
    failures = 0;
    state = await response.json();
    if (state.screen !== 'found') editing = false;
    render();
    followSignIn();
    if (!finalScreens.includes(state.screen)) schedule(800);
  }

  fetch('/assets/strings.en.json', { cache: 'no-store' })
    .then((response) => response.json())
    .then((strings) => {
      catalog = strings;
      document.title = t('setup.page.title');
      if (location.hash === '#elsewhere') show('elsewhere');
      else poll();
    })
    .catch(() => show('lost'));
})();
