// Playtest DOM checks. One implementation, used by ux-test (the runner
// evaluates it through chromedp) and by the overlay (live checks), so both
// modes give the same answer. Targets are data-testid ids or patterns:
// `*` is any one segment, `#` an index.
(function () {
  if (window.__pt) return;

  function patternRe(p) {
    var parts = String(p).split('.').map(function (s) {
      if (s === '*') return '[^.]+';
      if (s === '#') return '[0-9]+';
      return s.replace(/[\\^$+?()[\]{}|\/-]/g, '\\$&').replace(/\./g, '\\.');
    });
    return new RegExp('^' + parts.join('\\.') + '$');
  }

  function find(pattern, within) {
    var root = document;
    if (within) {
      var w = first(within);
      if (!w) return [];
      root = w;
    }
    var re = patternRe(pattern);
    return Array.prototype.filter.call(root.querySelectorAll('[data-testid]'), function (el) {
      return re.test(el.getAttribute('data-testid'));
    });
  }

  function visible(el) {
    if (!el || !el.isConnected) return false;
    if (el.checkVisibility && !el.checkVisibility({ opacityProperty: true, visibilityProperty: true })) return false;
    var r = el.getBoundingClientRect();
    return r.width > 0 && r.height > 0;
  }

  // first returns the first visible match, else the first match.
  function first(pattern, within) {
    var els = find(pattern, within);
    for (var i = 0; i < els.length; i++) if (visible(els[i])) return els[i];
    return els[0] || null;
  }

  function norm(s) { return String(s == null ? '' : s).replace(/\s+/g, ' ').trim(); }

  function text(el) {
    if (!el) return null;
    if (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA') return norm(el.value);
    return norm(visible(el) ? el.innerText : el.textContent);
  }

  function disabled(el) {
    return !!(el.disabled || el.getAttribute('aria-disabled') === 'true' || el.closest('fieldset[disabled]'));
  }

  function selected(el) {
    if (el.checked || el.selected) return true;
    if (el.getAttribute('aria-selected') === 'true' || el.getAttribute('aria-pressed') === 'true') return true;
    return el.classList.contains('active') || el.classList.contains('selected') || el.classList.contains('approved');
  }

  function describe(el) {
    if (!el) return 'nothing';
    var id = el.closest('[data-testid]');
    if (id) return id.getAttribute('data-testid');
    var s = el.tagName.toLowerCase();
    if (el.id) s += '#' + el.id;
    if (el.className && typeof el.className === 'string') s += '.' + el.className.trim().split(/\s+/).join('.');
    return s;
  }

  function cssPath(el) {
    var parts = [];
    while (el && el.nodeType === 1 && parts.length < 6) {
      var id = el.getAttribute('data-testid');
      if (id) { parts.unshift('[data-testid="' + id + '"]'); break; }
      var s = el.tagName.toLowerCase();
      if (el.id) { parts.unshift(s + '#' + el.id); break; }
      var p = el.parentElement;
      if (p) {
        var same = Array.prototype.filter.call(p.children, function (c) { return c.tagName === el.tagName; });
        if (same.length > 1) s += ':nth-of-type(' + (same.indexOf(el) + 1) + ')';
      }
      parts.unshift(s);
      el = p;
    }
    return parts.join(' > ');
  }

  // box is a rect as "x L..R y T..B" for shape check failures.
  function box(r) {
    return 'x ' + Math.round(r.left) + '..' + Math.round(r.right) + ' y ' + Math.round(r.top) + '..' + Math.round(r.bottom);
  }

  function res(pass, actual, detail) { return { pass: !!pass, actual: actual == null ? '' : String(actual), detail: detail || '' }; }

  function cmpCount(n, a) {
    if (a.equals != null && n !== Number(a.equals)) return false;
    if (a.min != null && n < Number(a.min)) return false;
    if (a.max != null && n > Number(a.max)) return false;
    return true;
  }

  // check evaluates one DOM check. snapshots maps a snapshot name to the
  // text it stored (see the snapshot action).
  function check(c, snapshots) {
    var a = c.arg || {};
    var el = a.target ? first(a.target) : null;
    var vis = el && visible(el);
    switch (c.kind) {
      case 'exists': return res(!!el, el ? 'present' : 'missing');
      case 'absent': return res(!vis, !el ? 'missing' : (vis ? 'visible' : 'hidden'));
      case 'visible': return res(vis, !el ? 'missing' : (vis ? 'visible' : 'hidden'));
      case 'hidden': return res(el && !vis, !el ? 'missing' : (vis ? 'visible' : 'hidden'));
      case 'enabled': return res(vis && !disabled(el), !el ? 'missing' : (!vis ? 'hidden' : (disabled(el) ? 'disabled' : 'enabled')));
      case 'disabled': return res(el && disabled(el), !el ? 'missing' : (disabled(el) ? 'disabled' : 'enabled'));
      case 'selected': return res(el && selected(el), !el ? 'missing' : (selected(el) ? 'selected' : 'not selected'));
      case 'text_equals': return res(el && text(el) === norm(a.text), el ? text(el) : 'missing');
      case 'text_contains': return res(el && text(el).indexOf(norm(a.text)) >= 0, el ? text(el) : 'missing');
      case 'value_equals': return res(el && String(el.value) === String(a.value), el ? el.value : 'missing');
      case 'count': {
        var n = find(a.pattern, a.within).filter(visible).length;
        return res(cmpCount(n, a), n);
      }
      case 'url': {
        var u = location.pathname + location.search;
        var ok = (a.equals == null || u === a.equals) &&
          (a.contains == null || u.indexOf(a.contains) >= 0) &&
          (a.matches == null || new RegExp(a.matches).test(u));
        return res(ok, u);
      }
      case 'changed':
      case 'unchanged': {
        var snap = snapshots && snapshots[a.since];
        if (snap == null) return res(false, '', 'no snapshot named ' + a.since);
        var now = el ? text(el) : null;
        return res(c.kind === 'changed' ? now !== snap : now === snap, now == null ? 'missing' : now, 'was: ' + snap);
      }
      case 'beside':
      case 'below': {
        var ea = first(a.a), eb = first(a.b);
        if (!ea || !eb || !visible(ea) || !visible(eb)) {
          return res(false, (!ea ? a.a + ' missing' : !visible(ea) ? a.a + ' hidden' : !eb ? a.b + ' missing' : a.b + ' hidden'));
        }
        var ra = ea.getBoundingClientRect(), rb = eb.getBoundingClientRect();
        var both = a.a + ' ' + box(ra) + ', ' + a.b + ' ' + box(rb);
        if (c.kind === 'below') return res(rb.top >= ra.bottom - 1, both);
        var ok = rb.left >= ra.right - 1 && Math.min(ra.bottom, rb.bottom) - Math.max(ra.top, rb.top) > 0;
        if (ok && a.gap_max != null && rb.left - ra.right > Number(a.gap_max)) ok = false;
        return res(ok, both);
      }
      case 'css': {
        if (!el) return res(false, 'missing');
        var cv = getComputedStyle(el).getPropertyValue(a.property).trim();
        var cok = a.equals != null ? cv === String(a.equals) :
          a['in'] != null ? [].concat(a['in']).map(String).indexOf(cv) >= 0 :
          cv !== String(a.not);
        return res(cok, cv);
      }
      case 'size': {
        if (!el) return res(false, 'missing');
        var sr = el.getBoundingClientRect();
        var sok = (a.min_width == null || sr.width >= Number(a.min_width)) &&
          (a.max_width == null || sr.width <= Number(a.max_width)) &&
          (a.min_height == null || sr.height >= Number(a.min_height)) &&
          (a.max_height == null || sr.height <= Number(a.max_height));
        return res(sok, Math.round(sr.width) + 'x' + Math.round(sr.height));
      }
    }
    return res(false, '', 'not a DOM check: ' + c.kind);
  }

  // --- layout rules ---------------------------------------------------

  function ignored(el, ignore) {
    for (var n = el; n; n = n.parentElement) {
      var id = n.getAttribute && n.getAttribute('data-testid');
      if (id) for (var i = 0; i < ignore.length; i++) if (patternRe(ignore[i]).test(id)) return true;
    }
    return false;
  }

  function inScroller(el) {
    for (var n = el.parentElement; n && n !== document.body && n !== document.documentElement; n = n.parentElement) {
      var s = getComputedStyle(n);
      if ((s.overflowX === 'auto' || s.overflowX === 'scroll') && n.scrollWidth > n.clientWidth) return true;
    }
    return false;
  }

  function floating(el) {
    for (var n = el; n && n !== document.body; n = n.parentElement) {
      var p = getComputedStyle(n).position;
      if (p === 'fixed' || p === 'absolute' || p === 'sticky') return true;
    }
    return false;
  }

  function ownText(el) {
    for (var i = 0; i < el.childNodes.length; i++) {
      if (el.childNodes[i].nodeType === 3 && el.childNodes[i].textContent.trim()) return true;
    }
    return false;
  }

  // scan calls onTop for each character of a selector that is outside (),
  // [] and quotes.
  function scan(s, onTop) {
    var depth = 0, quote = '';
    for (var i = 0; i < s.length; i++) {
      var ch = s[i];
      if (quote) { if (ch === '\\') i++; else if (ch === quote) quote = ''; continue; }
      if (ch === '"' || ch === "'") quote = ch;
      else if (ch === '\\') i++;
      else if (ch === '(' || ch === '[') depth++;
      else if (ch === ')' || ch === ']') depth--;
      else if (depth === 0) onTop(ch, i);
    }
  }

  function selectorList(text) {
    var out = [], start = 0;
    scan(text, function (ch, i) { if (ch === ',') { out.push(text.slice(start, i).trim()); start = i + 1; } });
    out.push(text.slice(start).trim());
    return out;
  }

  // compounds returns each compound selector of sel, stripped of pseudos,
  // with the selector up to and including it:
  // ".a .b > .c:hover" → own ".c", upto ".a .b > .c".
  function compounds(sel) {
    var parts = [], comb = ' ', start = 0;
    scan(sel + ' ', function (ch, i) {
      if (!/[\s>+~]/.test(ch)) return;
      if (i > start) { parts.push({ comb: comb, own: stripPseudo(sel.slice(start, i)).trim() || '*' }); comb = ' '; }
      if (!/\s/.test(ch)) comb = ' ' + ch + ' ';
      start = i + 1;
    });
    var upto = '';
    return parts.map(function (p, i) {
      upto += (i ? p.comb : '') + p.own;
      return { own: p.own, upto: upto };
    });
  }

  // stripPseudo drops pseudo-classes and pseudo-elements (and their
  // arguments): state such as :hover shouldn't decide whether a class is
  // styled, and :not(.x) doesn't style .x.
  function stripPseudo(sel) {
    var out = '', depth = 0, skip = false, quote = '';
    for (var i = 0; i < sel.length; i++) {
      var ch = sel[i];
      if (quote) { if (!skip) out += ch; if (ch === quote) quote = ''; continue; }
      if (skip) {
        if (ch === '(') depth++;
        else if (ch === ')') { depth--; if (depth === 0) { skip = false; continue; } }
        if (depth > 0) continue;
        if (/[\w-]/.test(ch) || (ch === ':' && sel[i - 1] === ':')) continue;
        skip = false;
      }
      if (ch === ':' && !inAttr(out)) { skip = true; depth = 0; continue; }
      if (ch === '"' || ch === "'") quote = ch;
      out += ch;
    }
    return out;
  }

  function inAttr(s) { return s.lastIndexOf('[') > s.lastIndexOf(']'); }

  // activeClassRules maps each class to the selectors of style rules that
  // apply at this viewport and name it, as {upto, own}: the selector cut
  // after the compound that names the class. @media and @supports blocks
  // count only when they match; other grouping rules (@layer, @container)
  // count; @keyframes and @font-face don't.
  function activeClassRules() {
    var idx = Object.create(null);
    function add(rule) {
      selectorList(rule.selectorText).forEach(function (sel) {
        compounds(sel).forEach(function (c) {
          var re = /\.(-?[_a-zA-Z][\w-]*)/g, m;
          while ((m = re.exec(c.own))) (idx[m[1]] = idx[m[1]] || []).push(c.upto);
        });
      });
    }
    function walk(list) {
      for (var i = 0; i < list.length; i++) {
        var r = list[i];
        if (r.selectorText != null) { add(r); continue; }
        if (!r.cssRules || (typeof CSSKeyframesRule !== 'undefined' && r instanceof CSSKeyframesRule)) continue;
        if (typeof CSSMediaRule !== 'undefined' && r instanceof CSSMediaRule) {
          if (!matchMedia(r.conditionText || r.media.mediaText).matches) continue;
        } else if (typeof CSSSupportsRule !== 'undefined' && r instanceof CSSSupportsRule) {
          if (!CSS.supports(r.conditionText)) continue;
        }
        walk(r.cssRules);
      }
    }
    Array.prototype.forEach.call(document.styleSheets, function (sh) {
      if (sh.disabled) return;
      if (sh.media && sh.media.mediaText && !matchMedia(sh.media.mediaText).matches) return;
      var rs;
      try { rs = sh.cssRules; } catch (e) { return; }
      walk(rs);
    });
    return idx;
  }

  function styled(el, cls, idx) {
    var sels = idx[cls];
    if (!sels) return false;
    for (var i = 0; i < sels.length; i++) {
      try { if (el.matches(sels[i])) return true; } catch (e) { /* selector the engine rejects */ }
    }
    return false;
  }

  // stylesCap bounds styles_applied's report: a page with no stylesheet
  // would otherwise list every class on it.
  var stylesCap = 25;

  function layout(rules, ignore) {
    ignore = ignore || [];
    var out = [];
    var vw = document.documentElement.clientWidth;
    var all = Array.prototype.filter.call(document.querySelectorAll('[data-testid]'), function (el) {
      return !ignored(el, ignore);
    });
    var shown = all.filter(visible);
    function v(rule, el, detail) { out.push({ rule: rule, id: el.getAttribute('data-testid'), detail: detail }); }
    rules.forEach(function (rule) {
      if (rule === 'inside_viewport') {
        if (document.documentElement.scrollWidth > vw + 1) {
          // Name the outermost element that sticks out, so a layout_ignore on
          // a known-bad area also waives the page scrolling because of it.
          var culprit = null;
          Array.prototype.some.call(document.body.querySelectorAll('*'), function (el) {
            if (ignored(el, ignore) || !visible(el) || inScroller(el)) return false;
            var r = el.getBoundingClientRect();
            if (r.right > vw + 1 || r.left < -1) { culprit = el; return true; }
            return false;
          });
          if (culprit) {
            out.push({ rule: rule, id: '(page)', detail: 'page scrolls sideways: ' + document.documentElement.scrollWidth + 'px wide in a ' + vw + 'px viewport, first because of ' + describe(culprit) + ' (' + cssPath(culprit) + ')' });
          }
        }
        shown.forEach(function (el) {
          if (inScroller(el)) return;
          var r = el.getBoundingClientRect();
          if (r.left < -1 || r.right > vw + 1) v(rule, el, 'x ' + Math.round(r.left) + '..' + Math.round(r.right) + ' outside 0..' + vw);
        });
      } else if (rule === 'no_clipping') {
        shown.forEach(function (el) {
          if (!ownText(el)) return;
          var s = getComputedStyle(el);
          if (s.textOverflow === 'ellipsis') return;
          var clipsSelf = (s.overflowX === 'hidden' || s.overflowX === 'clip') && el.scrollWidth > el.clientWidth + 1;
          if (clipsSelf) { v(rule, el, 'text ' + el.scrollWidth + 'px wide in a ' + el.clientWidth + 'px box'); return; }
          var p = el.parentElement;
          if (p) {
            var ps = getComputedStyle(p);
            if (ps.overflowX === 'hidden' || ps.overflowX === 'clip') {
              var r = el.getBoundingClientRect(), pr = p.getBoundingClientRect();
              if (r.right > pr.right + 1 || r.left < pr.left - 1) v(rule, el, 'cut by its parent at x ' + Math.round(pr.left) + '..' + Math.round(pr.right));
            }
          }
        });
      } else if (rule === 'no_overlap') {
        var groups = new Map();
        shown.forEach(function (el) {
          if (floating(el)) return;
          var parent = el.parentElement ? el.parentElement.closest('[data-testid]') : null;
          var key = parent || document.body;
          if (!groups.has(key)) groups.set(key, []);
          groups.get(key).push(el);
        });
        groups.forEach(function (els) {
          for (var i = 0; i < els.length; i++) for (var j = i + 1; j < els.length; j++) {
            if (els[i].contains(els[j]) || els[j].contains(els[i])) continue;
            var a = els[i].getBoundingClientRect(), b = els[j].getBoundingClientRect();
            var w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
            var h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
            if (w > 2 && h > 2) v(rule, els[i], 'overlaps ' + els[j].getAttribute('data-testid') + ' by ' + Math.round(w) + 'x' + Math.round(h) + 'px');
          }
        });
      } else if (rule === 'no_zero_size_text') {
        all.forEach(function (el) {
          if (!el.getClientRects().length || !norm(el.textContent)) return;
          var s = getComputedStyle(el);
          if (s.visibility === 'hidden' || s.display === 'none') return;
          var r = el.getBoundingClientRect();
          if (r.width === 0 || r.height === 0) v(rule, el, 'text in a ' + Math.round(r.width) + 'x' + Math.round(r.height) + ' box');
        });
      } else if (rule === 'styles_applied') {
        var idx = activeClassRules(), n = 0, more = 0;
        var at = innerWidth + 'x' + innerHeight;
        shown.forEach(function (el) {
          Array.prototype.forEach.call(el.classList, function (cls) {
            if (cls.indexOf('js-') === 0 || styled(el, cls, idx)) return;
            if (n++ < stylesCap) v(rule, el, 'class "' + cls + '" has no CSS rule active at ' + at + ' (add a rule, rename a JS-only hook to js-' + cls + ', or layout_ignore the id)');
            else more++;
          });
        });
        if (more) out.push({ rule: rule, id: '(page)', detail: more + ' more classes have no CSS rule active at ' + at });
      }
    });
    return out;
  }

  // --- action helpers -------------------------------------------------

  // point scrolls the target into view and returns its centre, refusing
  // when it is missing, hidden, disabled or covered by another element.
  function point(target) {
    var el = first(target);
    if (!el) return { ok: false, reason: target + ' not found' };
    el.scrollIntoView({ block: 'center', inline: 'center' });
    if (!visible(el)) return { ok: false, reason: target + ' is hidden' };
    if (disabled(el)) return { ok: false, reason: target + ' is disabled' };
    var r = el.getBoundingClientRect();
    var x = r.left + r.width / 2, y = r.top + r.height / 2;
    var hit = document.elementFromPoint(x, y);
    if (hit && hit !== el && !el.contains(hit) && !hit.contains(el)) {
      return { ok: false, reason: target + ' is covered by ' + describe(hit) };
    }
    return { ok: true, x: x, y: y };
  }

  function focus(target, clear) {
    var el = first(target);
    if (!el) return { ok: false, reason: target + ' not found' };
    if (!visible(el)) return { ok: false, reason: target + ' is hidden' };
    if (disabled(el)) return { ok: false, reason: target + ' is disabled' };
    el.scrollIntoView({ block: 'center' });
    el.focus();
    if (clear && 'value' in el) {
      el.value = '';
      el.dispatchEvent(new Event('input', { bubbles: true }));
    }
    return { ok: document.activeElement === el, reason: document.activeElement === el ? '' : target + ' did not take focus' };
  }

  function select(target, value) {
    var el = first(target);
    if (!el) return { ok: false, reason: target + ' not found' };
    if (!visible(el) || disabled(el)) return { ok: false, reason: target + ' is not usable' };
    var opts = el.options ? Array.prototype.map.call(el.options, function (o) { return o.value; }) : null;
    if (opts && opts.indexOf(String(value)) < 0) return { ok: false, reason: target + ' has no option ' + JSON.stringify(value) + ' (options: ' + opts.join(', ') + ')' };
    el.value = String(value);
    el.dispatchEvent(new Event('input', { bubbles: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
    return { ok: true };
  }

  // drag fires the HTML5 drag-and-drop sequence from one target onto
  // another with a shared DataTransfer (the board's cards use HTML5 DnD,
  // which CDP mouse events don't start).
  function drag(src, dst) {
    var a = first(src), b = first(dst);
    if (!a) return { ok: false, reason: src + ' not found' };
    if (!b) return { ok: false, reason: dst + ' not found' };
    if (!visible(a) || !visible(b)) return { ok: false, reason: (visible(a) ? dst : src) + ' is hidden' };
    var dt = new DataTransfer();
    function fire(el, type) {
      var r = el.getBoundingClientRect();
      el.dispatchEvent(new DragEvent(type, { bubbles: true, cancelable: true, dataTransfer: dt, clientX: r.left + r.width / 2, clientY: r.top + r.height / 2 }));
    }
    fire(a, 'dragstart');
    fire(b, 'dragenter');
    fire(b, 'dragover');
    fire(b, 'drop');
    fire(a, 'dragend');
    return { ok: true };
  }

  function targets() {
    return Array.prototype.map.call(document.querySelectorAll('[data-testid]'), function (el) {
      var r = el.getBoundingClientRect();
      return { id: el.getAttribute('data-testid'), rect: [Math.round(r.left), Math.round(r.top), Math.round(r.width), Math.round(r.height)], visible: visible(el), enabled: !disabled(el), text: text(el).slice(0, 80) };
    });
  }

  window.__pt = {
    patternRe: patternRe, find: find, first: first, visible: visible, text: text,
    check: check, layout: layout, point: point, focus: focus, select: select,
    drag: drag, targets: targets, describe: describe, cssPath: cssPath
  };
})();
