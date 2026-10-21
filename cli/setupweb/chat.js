// HA NOVA setup page: step 3's chat-app row, "Use HA NOVA from your phone
// too" (spec §11.6). steps.js hands in its helpers and its switch state
// (local); the CLI sends data only, and every text goes in as textContent.
'use strict';

window.novaChat = (h) => {
  const { t, el, row, button, link, render, say, local, toast, line, toggle, focusAfter, focusId, tagOn, flip, problem, mark, done, spinner, muted, needs } = h;

  // Chat apps (spec §11.6): the switch first shows what turning on means,
  // with the restart; only "Turn on and restart" posts, and that click is
  // the consent. Then the restart and Home Assistant loading it, then the
  // connector address to copy and how to add it.
  // Add to Claude: the CLI builds the link (claudeConnectorLink); only one
  // to claude.ai becomes a button, next to the host it adds.
  const addClaude = (href, kind, id) => {
    if (!/^https:\/\/claude\.ai\//.test(href || '')) return null;
    let host = '';
    try { host = decodeURIComponent((/[?&]connectorUrl=([^&]*)/.exec(href) || ['', ''])[1]).replace(/^https:\/\/([^/]+).*$/, '$1'); } catch (_) { /* no host line */ }
    return [link(t('setup.chat.add_claude'), href, kind, id), host ? el('span', { class: 'fine' }, t('setup.chat.adds', { host })) : null];
  };
  const copy = (text) => (navigator.clipboard ? navigator.clipboard.writeText(text) : Promise.reject(new Error('no clipboard')))
    .then(() => toast(t('setup.chat.copied')), () => toast(t('setup.chat.copy_failed')));
  // How to add it, one fold per app (closed); ChatGPT's holds the address to copy.
  const howTo = (app, title, keys, ...first) => {
    const details = el('details', { class: 'how' }, el('summary', { id: `b-chat-how-${app}` }, t(title)),
      ...first, el('ol', {}, keys.map((key) => el('li', {}, t(key)))));
    details.open = Boolean(local.how[app]);
    details.addEventListener('toggle', () => { local.how[app] = details.open; });
    return details;
  };
  // The row after the run ends takes the focus the run row had.
  const refocus = (node) => {
    node.setAttribute('tabindex', '-1');
    node.dataset.focusAfter = 'chat-run b-chat-on b-chat-cancel b-chat-recheck';
    return node;
  };
  // Once on: Add to Claude (never a second primary next to Finish), then how.
  const onParts = (chat) => [
    el('div', { class: 'actions addclaude' }, addClaude(chat.claude_link, 'ghost btn-small', 'chat-claude')),
    howTo('claude', 'setup.chat.claude_title', ['setup.chat.claude_1', 'setup.chat.claude_2', 'setup.chat.claude_3']),
    howTo('chatgpt', 'setup.chat.chatgpt_title', ['setup.chat.chatgpt_1', 'setup.chat.chatgpt_2', 'setup.chat.chatgpt_3'],
      el('div', { class: 'copybar' }, el('code', { id: 'chat-url' }, chat.url),
        el('button', { id: 'b-chat-copy', class: 'btn btn-ghost btn-small', type: 'button', onclick: () => copy(chat.url) }, t('setup.chat.copy'))))];
  // The guide's own-HTTPS-address part: a plain link, not a third button.
  const ownAddress = (href) => {
    const node = link(t('setup.chat.need_own'), href, 'ghost', 'chat-own');
    if (node) node.className = 'linkish';
    return node ? el('p', { class: 'fine' }, node) : null;
  };
  const chatRow = (c) => {
    const chat = c.chat;
    if (!chat) return null;
    // In the browser, chat apps are where the user is; here, the phone is the news.
    const web = h.state().web;
    const key = t(web ? 'setup.chat.key' : 'setup.chat.phone_key');
    const value = t(web ? 'setup.chat.value' : 'setup.chat.phone_value');
    const seen = `${chat.state} ${chat.attempt || 0}`; // a failed sign-in may come back before a poll saw it run
    if (seen !== local.chatSeen) {
      local.chatSeen = seen;
      local.chatFlip = false;
      if (chat.ask) local.chatAsk = true; // Done's sign-in is through: the consent, as on step 3
      if (chat.state === 'on') h.announce(t('setup.chat.on'));
    }
    // Optional here; what a browser user came for (state.web).
    const want = () => (h.state().web ? needs() : muted('◎'));
    const progress = (chat.rows || []).length
      ? el('ul', { class: 'rows sub' }, chat.rows.map((r) => row(r.state, say(r.msg)))) : null;
    switch (chat.state) {
      case 'on':
        return refocus(line(done(chat.problem), key, t(chat.problem ? 'setup.chat.page_on_short' : 'setup.chat.on_next'), [chat.problem ? progress : null,
          problem(chat.problem), ...onParts(chat), el('p', { class: 'fine' }, t('setup.chat.off_hint_page'))], tagOn()));
      case 'run':
        return el('li', { id: 'chat-run', ...focusAfter(['b-chat-on', 'b-chat-switch']) }, spinner(),
          el('div', {}, el('div', { class: 'k' }, key), el('div', { class: 'v', role: 'status' }, t(chat.no_restart ? 'setup.chat.wait_now' : 'setup.chat.wait')), progress), el('span'));
      case 'expired':
        return line(mark('fail', '!'), key, value, problem(chat.problem));
      case 'page_on': // a re-run: no owner sign-in yet, so Done applies the switch
      case 'page_off': {
        const was = chat.state === 'page_on';
        const on = was !== local.chatFlip; // the switch: what Done makes it; the mark: what it is now
        const sw = toggle('b-chat-switch', key, on, flip('chatFlip', was ? 'setup.chat.off_later' : 'setup.chat.on_later'), !h.accepts('finish'));
        const now = was ? done(chat.problem) : muted('◎');
        if (local.chatFlip) { // what Done does: off, or the sign-in and then the question
          return refocus(line(now, key, was ? t('setup.chat.off_later') : value, was ? null : el('div', { class: 'v' }, t('setup.chat.on_later')), sw, 'b-chat-switch-v'));
        }
        if (!was) return refocus(line(now, key, value, [el('div', { class: 'v' }, t('setup.chat.page_off')), problem(chat.problem)], sw, 'b-chat-switch-v')); // what it is, then Off
        return refocus(line(now, key, t(chat.url && !chat.problem ? 'setup.chat.page_on' : 'setup.chat.page_on_short'),
          [problem(chat.problem), ...(chat.url ? onParts(chat) : [])], sw, 'b-chat-switch-v'));
      }
      case 'signin':
        return el('li', {}, spinner(),
          el('div', {}, el('div', { class: 'k' }, key), el('div', { class: 'v', role: 'status' }, t('setup.chat.signin'))), el('span'));
      case 'noaddress': // on, but nothing reaches them: a problem. Off: what they need, and Check again
        if (chat.problem) return line(mark('fail', '!'), key, say(chat.problem));
        return refocus(line(want(), key, value, [
          el('div', { class: 'v' }, t('setup.chat.need_link')),
          el('div', { class: 'pick' }, link(t('setup.chat.open_link'), chat.link_page, 'ghost btn-small', 'chat-link'),
            button(t('setup.page.button.recheck'), 'chat_recheck', 'ghost btn-small', undefined, 'chat-recheck')),
          // What the last Check again found, once setup waits again.
          chat.checked && h.accepts('chat_recheck') ? el('p', { class: 'fine', role: 'status' }, t(chat.checked === 'failed' ? 'setup.chat.check_failed' : 'setup.chat.still_off')) : null,
          ownAddress(chat.guide)]));
      default: {
        const ask = local.chatAsk && h.accepts('chat');
        const consent = ask ? el('div', { class: 'consent', id: 'chat-consent' },
          el('p', {}, chat.phone ? t('setup.chat.consent_phone', { phone: chat.phone }) : t('setup.chat.consent_code')),
          chat.no_restart ? null : el('p', { class: 'restart' }, t('setup.chat.restart')),
          el('div', { class: 'actions' },
            button(t(chat.no_restart ? 'setup.chat.turn_on_now' : 'setup.chat.turn_on'), 'chat', 'primary btn-small', 'on', 'chat-on'),
            el('button', { id: 'b-chat-cancel', class: 'btn btn-ghost btn-small', type: 'button', onclick: () => { local.chatAsk = false; render(true); focusId('b-chat-ask'); } }, t('setup.chat.not_now')))) : null;
        // A button that opens the question; only its "Turn on" acts.
        const open = ask ? null : el('button', { id: 'b-chat-ask', class: 'btn btn-ghost btn-small', type: 'button', 'aria-expanded': 'false', 'aria-controls': 'chat-consent',
          onclick: () => { local.chatAsk = true; render(true); focusId('b-chat-on'); }, ...(h.accepts('chat') ? {} : { disabled: '' }) }, t('setup.chat.turn_on_ask'));
        const status = chat.problem ? mark('fail', '!') : want();
        return refocus(line(status, key, value, [problem(chat.problem), consent], open));
      }
    }
  };

  return { chatRow, addClaude };
};
