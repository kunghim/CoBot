(() => {
  // A compact, readable outline of the page: headings and text as lines,
  // every control the member may act on with a [eN] reference. The numbers
  // belong to this snapshot; a later page or snapshot hands them out anew.
  const MAX_LINES = 400, MAX_NAME = 100, MAX_TEXT = 200;
  const refs = new Map();
  window.__cobotRefs = refs;
  let counter = 0, truncated = false;
  const lines = [];
  const clip = (s, n) => {
    s = (s || '').replace(/\s+/g, ' ').trim();
    return s.length > n ? s.slice(0, n - 1) + '…' : s;
  };
  const push = (depth, text) => {
    if (lines.length >= MAX_LINES) { truncated = true; return; }
    lines.push('  '.repeat(Math.min(depth, 8)) + text);
  };
  const isHidden = (el) => {
    if (el.hasAttribute('hidden') || el.getAttribute('aria-hidden') === 'true') return true;
    const s = getComputedStyle(el);
    return s.display === 'none' || s.visibility === 'hidden' || s.visibility === 'collapse';
  };
  const INTERACTIVE_ROLES = new Set(['button', 'link', 'checkbox', 'radio', 'tab', 'menuitem', 'menuitemcheckbox',
    'menuitemradio', 'switch', 'option', 'combobox', 'textbox', 'searchbox', 'slider', 'spinbutton', 'treeitem', 'file']);
  const INTERACTIVE_SELECTOR = 'a[href],button,input:not([type=hidden]),select,textarea,summary,[role=button],[role=link],' +
    '[role=checkbox],[role=radio],[role=tab],[role=menuitem],[role=switch],[role=option],[role=combobox],[role=textbox],' +
    '[tabindex],[onclick],[contenteditable=""],[contenteditable=true]';
  const inputRole = (el) => {
    switch ((el.type || 'text').toLowerCase()) {
      case 'checkbox': return 'checkbox';
      case 'radio': return 'radio';
      case 'button': case 'submit': case 'reset': case 'image': return 'button';
      case 'range': return 'slider';
      case 'number': return 'spinbutton';
      case 'search': return 'searchbox';
      case 'file': return 'file';
      case 'hidden': return null;
      default: return 'textbox';
    }
  };
  const roleOf = (el) => {
    const explicit = el.getAttribute('role');
    if (explicit) return explicit.split(' ')[0];
    switch (el.tagName) {
      case 'A': return el.hasAttribute('href') ? 'link' : null;
      case 'BUTTON': case 'SUMMARY': return 'button';
      case 'INPUT': return inputRole(el);
      case 'SELECT': return 'combobox';
      case 'TEXTAREA': return 'textbox';
      case 'H1': case 'H2': case 'H3': case 'H4': case 'H5': case 'H6': return 'heading';
      case 'NAV': return 'navigation';
      case 'MAIN': return 'main';
      case 'FORM': return 'form';
      case 'DIALOG': return 'dialog';
      case 'UL': case 'OL': return 'list';
      case 'TABLE': return 'table';
      default: return el.isContentEditable && el.hasAttribute('contenteditable') ? 'textbox' : null;
    }
  };
  const isInteractive = (el, role) => {
    if (role && INTERACTIVE_ROLES.has(role)) return true;
    const ti = el.getAttribute('tabindex');
    if (ti !== null && Number(ti) >= 0 && el.tagName !== 'BODY') return true;
    return el.hasAttribute('onclick');
  };
  const labelText = (el) => {
    const aria = el.getAttribute('aria-label');
    if (aria) return aria;
    const by = el.getAttribute('aria-labelledby');
    if (by) {
      const text = by.split(/\s+/).map((id) => (document.getElementById(id) || {}).textContent || '').join(' ');
      if (text.trim()) return text;
    }
    if (el.labels && el.labels.length) return Array.from(el.labels).map((l) => l.textContent).join(' ');
    return el.getAttribute('alt') || el.getAttribute('title') || el.getAttribute('placeholder') || '';
  };
  const nameOf = (el) => {
    const name = clip(labelText(el), MAX_NAME);
    if (name) return name;
    if (el.tagName === 'INPUT' && ['button', 'submit', 'reset'].includes(el.type)) return clip(el.value, MAX_NAME);
    if (el.tagName === 'SELECT') return '';
    return clip(el.innerText || el.textContent, MAX_NAME);
  };
  const flagsOf = (el) => {
    const out = [];
    if (el.disabled || el.getAttribute('aria-disabled') === 'true') out.push('disabled');
    if (el.checked === true || el.getAttribute('aria-checked') === 'true') out.push('checked');
    if (el.getAttribute('aria-expanded')) out.push('expanded=' + el.getAttribute('aria-expanded'));
    if (el.getAttribute('aria-selected') === 'true') out.push('selected');
    if (el.getAttribute('aria-current')) out.push('current');
    if (el === document.activeElement) out.push('focused');
    if (el.required) out.push('required');
    if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') {
      // A password is never read out: the member does not handle credentials.
      if (el.type === 'password') out.push(el.value ? 'value=(已填，隐藏)' : 'value=(空)');
      else if (!['checkbox', 'radio', 'button', 'submit', 'reset', 'file', 'image'].includes(el.type) && el.value) {
        out.push('value=' + JSON.stringify(clip(el.value, 80)));
      }
    }
    if (el.tagName === 'SELECT') {
      const option = el.selectedOptions && el.selectedOptions[0];
      if (option) out.push('value=' + JSON.stringify(clip(option.textContent, 60)));
    }
    if (el.tagName === 'A' && el.getAttribute('href')) out.push('href=' + clip(el.getAttribute('href'), 100));
    return out;
  };
  const inlineOnly = (el) => {
    if (el.querySelector(INTERACTIVE_SELECTOR)) return false;
    for (const child of el.children) {
      const display = getComputedStyle(child).display;
      if (!(display.startsWith('inline') || display === 'contents')) return false;
    }
    return true;
  };
  const SKIP = new Set(['SCRIPT', 'STYLE', 'NOSCRIPT', 'TEMPLATE', 'HEAD', 'META', 'LINK']);
  const walk = (el, depth) => {
    if (truncated) return;
    const tag = el.tagName;
    if (SKIP.has(tag) || tag.toLowerCase() === 'svg' || isHidden(el)) return;
    const role = roleOf(el);
    if (role === 'heading') {
      const level = /^H[1-6]$/.test(tag) ? tag[1] : (el.getAttribute('aria-level') || '2');
      push(depth, '#'.repeat(Number(level)) + ' ' + clip(el.innerText, MAX_TEXT));
      return;
    }
    if (isInteractive(el, role)) {
      const ref = 'e' + (++counter);
      refs.set(ref, el);
      const name = nameOf(el);
      const flags = flagsOf(el);
      push(depth, '[' + ref + '] ' + (role || tag.toLowerCase()) + (name ? ' ' + JSON.stringify(name) : '') + (flags.length ? ' ' + flags.join(' ') : ''));
      return;
    }
    if (tag === 'IFRAME' || tag === 'FRAME') {
      push(depth, '(iframe ' + clip(el.getAttribute('src') || el.getAttribute('name') || '', 80) + '：内容不在快照中)');
      return;
    }
    if (tag === 'IMG') {
      const alt = el.getAttribute('alt');
      if (alt) push(depth, 'img ' + JSON.stringify(clip(alt, MAX_NAME)));
      return;
    }
    if (inlineOnly(el)) {
      const text = clip(el.innerText, MAX_TEXT);
      if (text) push(depth, text);
      return;
    }
    const landmark = role && ['navigation', 'main', 'form', 'dialog', 'list', 'table'].includes(role);
    if (landmark) push(depth, role);
    const next = landmark ? depth + 1 : depth;
    // Text sitting beside child elements, except a label whose text the
    // control inside it already carries as its name.
    if (tag !== 'LABEL') {
      const own = Array.from(el.childNodes).filter((n) => n.nodeType === 3).map((n) => n.textContent).join(' ');
      const text = clip(own, MAX_TEXT);
      if (text) push(next, text);
    }
    for (const child of el.children) walk(child, next);
    if (el.shadowRoot) for (const child of el.shadowRoot.children) walk(child, next);
  };
  walk(document.body || document.documentElement, 0);
  const scroller = document.scrollingElement;
  const footer = [];
  if (scroller && scroller.scrollHeight > scroller.clientHeight + 4) {
    footer.push('滚动位置 ' + Math.round(scroller.scrollTop) + '/' + Math.round(scroller.scrollHeight - scroller.clientHeight) + 'px');
  }
  if (truncated) footer.push('内容太多，已截断；滚动后再取快照可看到更多');
  return lines.join('\n') + (footer.length ? '\n(' + footer.join('；') + ')' : '');
})()
