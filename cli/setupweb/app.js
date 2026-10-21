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
  const finalScreens = ['done', 'closed', 'saved', 'skipped', 'demo', 'incomplete', 'finished', 'failed', 'stopped', 'idle', 'lost', 'elsewhere'];
  const steps = { looking: 1, found: 1, noclient: 1, connect: 2, control: 3, home: 4, done: 4, closed: 4, skipped: 3, incomplete: 4 };
  const downloads = {
    claude_desktop: 'https://claude.com/download',
    claude_code: 'https://claude.com/claude-code',
  };
  const primaryLabels = { install: 'setup.page.button.install', update: 'setup.page.button.update', signin: 'setup.page.button.signin' };
  const fixLabels = { retry: 'setup.page.button.retry', owner: 'setup.page.button.owner', web: 'setup.page.button.web_recheck' };
  const rowStatus = { ok: 'setup.page.status.ok', run: 'setup.page.status.run', fail: 'setup.page.status.fail', todo: 'setup.page.status.todo' };
  const endScreens = {
    incomplete: ['setup.page.incomplete.eyebrow', 'setup.page.incomplete.title', 'setup.page.incomplete.lead', true],
    finished: ['setup.page.finished.eyebrow', 'setup.page.finished.title', 'setup.page.done.fine'],
    closed: ['setup.page.closed.eyebrow', 'setup.page.closed.title', 'setup.page.closed.lead'],
    skipped: ['setup.page.skipped.eyebrow', 'setup.page.skipped.title', 'setup.page.skipped.lead'],
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
  let sentAddress = false;
  let posting = false;
  let checking = false; // Check again was clicked: "Checking…" until setup waits for a click again
  let clicks = 0; // posts so far: a poll begun before (or during) one shows the old state
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
    clicks++;
    if (action === 'recheck') {
      checking = true;
      statusEl.textContent = t('setup.page.button.checking');
    }
    render(true);
    try {
      const response = await fetch('/api/action', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'same-origin',
        body: JSON.stringify(value === undefined ? { action } : { action, value }),
      });
      // A taken address ends "Change" once setup has checked it (poll).
      if (action === 'address' && response.ok) sentAddress = editing;
    } catch (_) { /* the next poll shows where setup stands */ }
    posting = false;
    schedule(0);
  };

  const accepts = (action) => state.accepts.includes(action) && !posting;
  // Buttons carry stable ids, so focus survives a re-render.
  const button = (label, action, kind = 'primary', value, id = action) =>
    el('button', { id: `b-${id}`, class: `btn btn-${kind}`, type: 'button', onclick: () => post(action, value), ...(accepts(action) ? {} : { disabled: '' }) },
      action === 'recheck' && checking && !accepts(action) ? t('setup.page.button.checking') : label);
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
  // "Use a six-digit code": a text link, since it goes on in the terminal.
  const codeLink = () => el('p', { class: 'fine' }, `${t('setup.page.found.fine_elsewhere')} `,
    el('button', { id: 'b-terminal', class: 'linkish', type: 'button', onclick: () => post('terminal'), ...(accepts('terminal') ? {} : { disabled: '' }) }, t('setup.page.button.code')));
  // "What's this?": what chat apps need (docs/reference/chat-apps.md), in a new tab.
  const chatHelp = () => {
    const href = safeURL((state.links || {}).chat);
    return href ? el('a', { id: 'l-chat-help', class: 'linkish', href, target: '_blank', rel: 'noopener noreferrer' }, t('setup.page.link.whats_this')) : null;
  };
  // Why the terminal (state.handover); a question there by default.
  const terminalLeads = { code: 'setup.page.terminal.lead_code', keyring_locked: 'setup.page.terminal.lead_keyring_locked', keyring_setup: 'setup.page.terminal.lead_keyring_setup' };

  const screens = {
    looking: () => [...head(t('setup.page.looking.eyebrow'), t('setup.page.looking.title'), t('setup.page.looking.lead')),
      el('ul', { class: 'rows' }, row('run', t('setup.page.ha.key')))],

    // Claude or ChatGPT in the browser goes on without an AI app here; what
    // it needs is said under its button, before any sign-in.
    noclient: () => [...head(t('setup.page.noclient.eyebrow'), t('setup.page.noclient.title'), t('setup.page.noclient.lead')),
      el('div', { class: 'actions' }, button(t('setup.page.noclient.web'), 'web')),
      el('p', { class: 'fine', id: 'web-needs' }, `${t('setup.page.noclient.web_needs')} `, chatHelp()),
      el('div', { class: 'actions' },
        link(t('setup.page.link.claude_desktop'), downloads.claude_desktop, 'ghost', 'desktop'),
        link(t('setup.page.link.claude_code'), downloads.claude_code, 'ghost', 'code'),
        button(t('setup.page.button.recheck'), 'recheck', 'ghost'))],

    terminal: () => head(t('setup.page.terminal.eyebrow'), t('setup.page.terminal.title'), t(terminalLeads[state.handover] || 'setup.page.terminal.lead')),

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
      const clientRow = state.web ? row('ok', t('setup.page.clients.key'), t('setup.page.clients.web'))
        : row('ok', t('setup.page.clients.key'), format(clients), {
          side: el('span', { class: 'tag cyan' }, t('setup.page.clients.tag', { count: clients.length })),
        });
      let appRow;
      if (!state.app || asking) appRow = row('todo', t('setup.page.app.key'), t('setup.page.app.unknown'));
      else if (state.app === 'running') appRow = row('ok', t('setup.page.app.key'), state.app_version ? t('setup.page.app.running_version', { version: state.app_version }) : t('setup.page.app.running'));
      else appRow = row('todo', t('setup.page.app.key'), t(state.app === 'old' ? 'setup.page.app.old' : 'setup.page.app.missing'), { side: el('span', { class: 'tag amber' }, t('setup.page.app.next')) });

      const primary = asking ? '' : state.primary;
      // Who signs in, said above the button: a family member may not know.
      const owner = primary ? el('p', { class: 'owner' }, t('setup.page.found.owner')) : null;
      const actions = el('div', { class: 'actions' },
        primary ? button(t(primaryLabels[primary]), 'signin')
          : button(t('setup.page.button.terminal'), 'terminal', asking ? 'ghost' : 'primary'));
      let fine = [];
      if (primary) {
        fine = [primary === 'signin' ? null : t('setup.page.found.fine_install'),
          t('setup.page.found.fine_access', { client: `${location.origin}/ha-nova` })];
        // No "copy the sign-in link": its answer only comes back to this computer.
        // Claude or ChatGPT in the browser: no code, chat apps need the owner's sign-in here.
        if (!state.web) fine.push(codeLink());
      } else if (!asking && state.app) {
        fine = [t('setup.page.found.fine_terminal')];
      }
      if (!haKnown) {
        fine.push(el('button', { id: 'b-demo', class: 'linkish', type: 'button', onclick: () => post('demo'), ...(accepts('demo') ? {} : { disabled: '' }) }, t('setup.page.found.demo')));
      }
      let [title, lead] = haKnown ? ['setup.page.found.title', 'setup.page.found.lead'] : ['setup.page.found.title_ask', 'setup.page.found.lead_ask'];
      if (haKnown && state.typed) [title, lead] = ['setup.page.found.title_typed', 'setup.page.found.lead_typed'];
      if (haKnown && asking) [title, lead] = ['setup.page.found.title_ask', 'setup.page.found.lead_change']; // the address field is open
      if (several) lead = 'setup.page.found.lead_several';
      return [...head(t('setup.page.found.eyebrow'), t(title), t(lead)),
        problemLine(),
        el('ul', { class: 'rows' }, haRow, clientRow, appRow),
        owner,
        actions,
        ...fine.filter(Boolean).map((line) => (typeof line === 'string' ? el('p', { class: 'fine' }, line) : line))];
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
            el('button', { id: 'b-stop', class: 'btn btn-ghost', type: 'button', onclick: () => post('stop') }, t('setup.page.signin.stop'))),
          // While the App installs there is no code yet.
          installs ? null : el('p', { class: 'fine' }, t('setup.page.signin.elsewhere')));
      } else if (state.accepts.length) {
        const links = state.links || {};
        const webFix = state.fix === 'web' || state.fix === 'web_never';
        actions.push(el('div', { class: 'actions' },
          fixLabels[state.fix] ? button(t(fixLabels[state.fix]), 'signin') : null,
          // Chat apps can't work here yet: set it up and sign in again, or an AI app on this computer.
          webFix ? button(t('setup.page.button.local'), 'local', state.fix === 'web' ? 'ghost' : 'primary') : null,
          link(t('setup.page.link.guide'), links.guide, fixLabels[state.fix] ? 'ghost' : 'primary', 'guide'),
          link(t('setup.page.link.app'), links.app, 'ghost', 'app')),
          webFix ? el('p', { class: 'fine' }, state.fix === 'web' ? `${t('setup.page.connect.web_signin')} ` : null, chatHelp()) : null,
          // Not owner: the owner can still make a code on any device.
          state.fix === 'owner' && !state.web ? el('p', { class: 'fine' }, t('setup.page.found.owner_away')) : null,
          state.web ? null : codeLink());
      }
      return [...head(t('setup.page.connect.eyebrow'), t('setup.page.connect.title'), t(installs ? 'setup.page.connect.lead' : 'setup.page.connect.lead_signin'), true),
        problemLine(), el('ul', { class: 'rows' }, items), ...actions];
    },

    done: () => [...head(t('setup.page.done.eyebrow'), t('setup.page.done.title'), t('setup.page.done.lead', { clients: state.clients || [] })),
      el('p', { class: 'fine' }, t('setup.page.done.fine'))],
  };
  // Steps 3 and 4 (steps.js), and a re-run (rerun.js).
  const announce = (text) => { statusEl.textContent = text; };
  const helpers = { t, el, row, head, button, link, format, render, post, accepts, announce, state: () => state };
  const later = window.novaSteps(helpers);
  Object.assign(screens, later, window.novaRerun({ ...helpers, control: later.control }));
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
    const key = JSON.stringify(state) + posting + editing + checking;
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
      if (document.getElementById(focused)) focus(focused);
      else {
        // The clicked control is gone: focus what replaced it (steps.js).
        const next = [...screenEl.querySelectorAll('[data-focus-after]')].find((node) => node.dataset.focusAfter.split(' ').includes(focused));
        if (next) next.focus({ preventScroll: true });
      }
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
    const sent = sentAddress; // a poll begun before the address went out shows the old state
    const seen = posting ? -1 : clicks;
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
    const fresh = await response.json();
    // Older than the last click: the poll after it shows where setup stands.
    // An end screen is never stale, and setup may close right after it is read.
    if (seen !== clicks && !finalScreens.includes(fresh.screen)) return;
    state = fresh;
    if ((state.accepts || []).length || state.screen !== lastScreen) checking = false;
    // Setup checks a sent address, then waits for a click again: the field
    // closes (a wrong one comes back with ask_ha).
    if (state.screen !== 'found' || (sent && sentAddress && (state.accepts || []).length)) editing = sentAddress = false;
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
