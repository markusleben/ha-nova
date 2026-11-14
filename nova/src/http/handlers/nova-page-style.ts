// The NOVA pages' look, shared by the console (nova-page-view.ts) and the
// approval view (nova-approve.ts). Colours are Home Assistant's own default
// theme tokens; prefers-color-scheme follows the dark/light preference (HA
// injects no theme CSS into ingress frames).

export const PAGE_CSS = `
  :root {
    --bg: #f5f5f7; --card: #ffffff; --text: #212121; --muted: #6f6f6f; --field: #f0f0f2;
    --border: rgba(0,0,0,.10); --primary: #03a9f4; --primary-soft: rgba(3,169,244,.10); --on-primary: #ffffff;
    --danger: #db4437; --ok: #2e7d32; --shadow: 0 1px 3px rgba(0,0,0,.08);
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #111111; --card: #1c1c1c; --text: #e1e1e1; --muted: #9b9b9b; --field: #262626;
      --border: rgba(225,225,225,.12); --primary-soft: rgba(3,169,244,.16); --danger: #e57373; --ok: #81c784; --shadow: none;
    }
  }
  * { box-sizing: border-box; }
  [hidden] { display: none !important; }
  body { font-family: Roboto, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    background: var(--bg); color: var(--text); margin: 0; padding: 1.5rem 1rem 2rem;
    -webkit-font-smoothing: antialiased; line-height: 1.5; }
  main { max-width: 38rem; margin: 0 auto; }
  h1 { display: flex; align-items: center; gap: .55rem; font-size: 1.5rem; font-weight: 500; margin: 0 0 .25rem; }
  .logo { width: 1.7rem; height: 1.7rem; color: var(--primary); }
  .intro { color: var(--muted); margin: 0 0 1.25rem; }
  section { background: var(--card); border: 1px solid var(--border); border-radius: 12px;
    box-shadow: var(--shadow); padding: 1rem 1.25rem; margin: 0 0 1rem; }
  section h2 { font-size: .8rem; text-transform: uppercase; letter-spacing: .04em;
    color: var(--muted); margin: 0 0 .5rem; font-weight: 600; }
  section h3 { font-size: 1rem; font-weight: 500; margin: 1.25rem 0 .35rem; }
  p { margin: .35rem 0; }
  a { color: var(--primary); }
  .muted { color: var(--muted); }
  .meta { font-size: .85rem; }
  .ok { color: var(--primary); }
  .error { border-color: var(--danger); }
  .error h2, .error p, p.error { color: var(--danger); }
  .waiting { font-style: italic; animation: pulse 1.6s ease-in-out infinite; }
  @keyframes pulse { 0%, 100% { opacity: .55; } 50% { opacity: 1; } }
  /* One click selects the whole code or address for copying. */
  .code { font-size: 2.25rem; letter-spacing: .3rem; font-weight: 700; color: var(--primary); margin: .5rem 0; user-select: all; -webkit-user-select: all; cursor: pointer; }
  .address { font-family: ui-monospace, monospace; font-size: .85rem; overflow-wrap: anywhere; user-select: all; -webkit-user-select: all; cursor: pointer; }
  .field { display: block; background: var(--field); border: 1px solid var(--border); border-radius: 8px; padding: .55rem .7rem; margin: .25rem 0; }
  .label { font-size: .85rem; font-weight: 500; margin: 1rem 0 0; }
  form { display: inline; margin: 0; }
  button, .linkbtn { display: inline-flex; align-items: center; justify-content: center; min-height: 2.75rem;
    font: inherit; font-weight: 500; padding: .45rem 1.1rem; border-radius: 10px; text-decoration: none; cursor: pointer; }
  button { border: 1px solid var(--primary); background: var(--primary); color: var(--on-primary); }
  button.secondary, .linkbtn { background: transparent; color: var(--text); border: 1px solid var(--border); }
  button.danger-solid { background: var(--danger); border-color: var(--danger); color: var(--on-primary); }
  .linkbtn.danger { color: var(--danger); }
  .linkbtn.primary { background: var(--primary); color: var(--on-primary); border-color: var(--primary); }
  .linkbtn.quiet { border-color: transparent; padding-left: .5rem; padding-right: .5rem; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; margin: .6rem 0 0; }
  .pill { display: inline-block; font-size: .8rem; font-weight: 500; border-radius: 999px; padding: .05rem .6rem;
    background: var(--primary-soft); color: var(--primary); white-space: nowrap; }
  .pill.good { color: var(--ok); background: transparent; border: 1px solid currentColor; }
  .pill.bad { color: var(--danger); background: transparent; border: 1px solid currentColor; }
  .badge { display: inline-block; font-size: .7rem; text-transform: uppercase; letter-spacing: .04em;
    border: 1px solid var(--primary); color: var(--primary); border-radius: 6px;
    padding: 0 .35rem; margin-left: .45rem; vertical-align: middle; }
  .confirm { border-color: var(--danger); }
  .confirm input[type="text"] { font: inherit; padding: .45rem .7rem; border-radius: 10px;
    border: 1px solid var(--border); background: var(--bg); color: var(--text); width: 7rem; }
  .alert { border: 2px solid var(--primary); background: linear-gradient(var(--primary-soft), var(--primary-soft)), var(--card); }
  .alert h2 { color: var(--primary); }
  .count { display: inline-block; min-width: 1.4rem; text-align: center; border-radius: 999px; background: var(--primary); color: var(--on-primary); padding: 0 .4rem; margin-left: .25rem; }
  .lead { font-size: 1.05rem; font-weight: 500; }
  .ask-head { display: flex; justify-content: space-between; align-items: center; gap: .5rem; }
  .timer { font-variant-numeric: tabular-nums; font-weight: 600; color: var(--primary); }
  .what { font-size: 1.3rem; font-weight: 500; line-height: 1.35; margin: .35rem 0 .5rem; }
  ol.what { padding-left: 1.4rem; font-size: 1.1rem; }
  .what li { display: list-item; border: none; padding: .2rem 0; }
  .detail { display: block; font-weight: 400; margin-top: .15rem; }
  .ask .actions button { flex: 1 1 9rem; min-height: 2.9rem; }
  .bar { height: 4px; border-radius: 2px; background: var(--border); overflow: hidden; margin-top: .9rem; }
  .bar span { display: block; height: 100%; background: var(--primary); transform-origin: left; animation: drain linear forwards; }
  @keyframes drain { from { transform: scaleX(var(--from, 1)); } to { transform: scaleX(0); } }
  ul { list-style: none; padding: 0; margin: 0; }
  li { display: flex; align-items: center; justify-content: space-between; gap: .75rem 1rem; flex-wrap: wrap;
    padding: .65rem 0; border-top: 1px solid var(--border); }
  li:first-child { border-top: none; }
  .device, .person { display: flex; flex-direction: column; min-width: 0; flex: 1 1 14rem; }
  .facts li { padding: .45rem 0; }
  .clients { display: grid; grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr)); gap: .75rem; margin: .75rem 0 0; }
  .client { border: 1px solid var(--border); border-radius: 10px; padding: .75rem .9rem; }
  .client h3 { margin: 0 0 .25rem; }
  details { font-size: .9rem; border-top: 1px solid var(--border); margin-top: .6rem; }
  summary { cursor: pointer; color: var(--primary); font-weight: 500; padding: .6rem 0; }
  :focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }
  details[open] > summary { margin-bottom: .25rem; }
  details ol { margin: .25rem 0; padding-left: 1.25rem; }
  li details { flex-basis: 100%; border-top: none; margin-top: 0; }
  li details summary { padding-top: 0; }
  select { font: inherit; min-height: 2.75rem; padding: .35rem .5rem; border-radius: 10px; border: 1px solid var(--border); background: var(--bg); color: var(--text); }
  input[type=checkbox], input[type=radio] { width: 1.1rem; height: 1.1rem; vertical-align: -.15rem; accent-color: var(--primary); }
  code { background: var(--border); padding: .05rem .35rem; border-radius: 5px; font-size: .9em; }
  footer { color: var(--muted); font-size: .8rem; margin-top: 1.5rem; text-align: center; }
  @media (max-width: 30rem) {
    body { padding: 1rem .75rem 1.5rem; }
    section { padding: .9rem 1rem; }
    li > select { margin-left: 1.6rem; }
  }
`;
