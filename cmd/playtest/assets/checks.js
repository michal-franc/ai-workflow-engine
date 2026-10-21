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
