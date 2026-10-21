// Playtest overlay: the owner's judgement steps on a live board. The runner
// injects this (with checks.js) into every page of its Chrome window; the
// board itself has no overlay code. It talks to the runner through
// window.__ptSend(msg), which the runner exposes; {type:'hello'} returns the
// current state.
//
// Keys: Alt+P pass, Alt+F fail (note), Alt+S skip, Alt+R replay the step,
// Alt+N opinion on the element under the mouse, F10 hide/show the dock.
(function () {
  if (window.top !== window || window.__ptOverlay) return;

  var CYAN = '#22d3ee';
  var css = [
    '#__pt{position:fixed;inset:0;pointer-events:none;z-index:2147483646;font:13px/1.45 system-ui,-apple-system,"Segoe UI",sans-serif;color:#e6f6fa}',
    '#__pt *{box-sizing:border-box}',
    '#__pt .dock{position:fixed;top:12px;bottom:12px;width:340px;background:rgba(8,20,26,.96);border:1px solid ' + CYAN + ';border-radius:8px;box-shadow:0 8px 32px rgba(0,0,0,.5);display:flex;flex-direction:column;pointer-events:auto;overflow:hidden}',
    '#__pt .dock.right{right:12px}#__pt .dock.left{left:12px}',
    '#__pt .hd{padding:10px 12px;border-bottom:1px solid rgba(34,211,238,.35)}',
    '#__pt .hd b{color:' + CYAN + ';letter-spacing:.08em}',
    '#__pt .sub{color:#8fb7c2;font-size:12px}',
    '#__pt .body{flex:1;overflow:auto;padding:8px 12px}',
    '#__pt .steps{list-style:none;margin:0 0 10px;padding:0}',
    '#__pt .steps li{display:flex;gap:6px;align-items:baseline;padding:3px 4px;border-radius:4px;cursor:default}',
    '#__pt .steps li.you{cursor:pointer}#__pt .steps li.you:hover{background:rgba(34,211,238,.12)}',
    '#__pt .steps li.cur{background:rgba(34,211,238,.2)}',
    '#__pt .tag{font-size:10px;padding:0 4px;border-radius:3px;border:1px solid #4b6b75;color:#8fb7c2}',
    '#__pt .tag.you{border-color:' + CYAN + ';color:' + CYAN + '}',
    '#__pt .r{margin-left:auto;font-weight:600}',
    '#__pt .pass{color:#4ade80}#__pt .fail{color:#f87171}#__pt .skip{color:#facc15}#__pt .pending{color:#8fb7c2}',
    '#__pt h4{margin:10px 0 2px;font-size:11px;letter-spacing:.1em;color:' + CYAN + '}',
    '#__pt .txt{white-space:pre-wrap}',
    '#__pt .chk{display:flex;gap:6px;font-family:ui-monospace,monospace;font-size:11.5px}',
    '#__pt .btns{display:flex;gap:6px;padding:10px 12px;border-top:1px solid rgba(34,211,238,.35);flex-wrap:wrap}',
    '#__pt button{background:transparent;color:#e6f6fa;border:1px solid ' + CYAN + ';border-radius:5px;padding:5px 9px;font:inherit;cursor:pointer}',
    '#__pt button:hover{background:rgba(34,211,238,.18)}',
    '#__pt button.p{border-color:#4ade80}#__pt button.f{border-color:#f87171}',
    '#__pt textarea{width:100%;min-height:64px;background:#03151b;color:#e6f6fa;border:1px solid ' + CYAN + ';border-radius:5px;padding:6px;font:inherit}',
    '#__pt .form{padding:8px 12px;border-top:1px solid rgba(34,211,238,.35)}',
    '#__pt .hl{position:fixed;border:2px solid ' + CYAN + ';border-radius:4px;box-shadow:0 0 0 3px rgba(34,211,238,.25);animation:__ptpulse 1.4s ease-in-out infinite}',
    '#__pt .chip{position:fixed;background:' + CYAN + ';color:#03151b;font-weight:700;font-size:11px;border-radius:9px;padding:0 6px;line-height:18px}',
    '#__pt .arrow{position:fixed;background:' + CYAN + ';color:#03151b;font-weight:700;font-size:12px;border-radius:4px;padding:2px 6px}',
    '#__pt .card{position:fixed;max-width:320px;background:rgba(8,20,26,.97);border:1px solid ' + CYAN + ';border-radius:6px;padding:8px 10px;box-shadow:0 6px 20px rgba(0,0,0,.5)}',
    '#__pt .busy{position:fixed;left:12px;bottom:12px;padding:6px 10px;color:' + CYAN + ';background:rgba(8,20,26,.9);border:1px solid ' + CYAN + ';border-radius:6px;pointer-events:none}',
    '#__pt .err{color:#f87171;white-space:pre-wrap;font-family:ui-monospace,monospace;font-size:11.5px}',
    '@keyframes __ptpulse{0%,100%{box-shadow:0 0 0 2px rgba(34,211,238,.15)}50%{box-shadow:0 0 0 6px rgba(34,211,238,.45)}}'
  ].join('\n');

  var state = null, root, dock, layer, form = null, hidden = false, mouse = { x: -1, y: -1 }, live = [];

  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text != null) e.textContent = text;
    return e;
  }

  function send(msg) {
    if (typeof window.__ptSend !== 'function') return Promise.reject(new Error('runner not connected'));
    return window.__ptSend(msg);
  }

  function init() {
    var style = el('style');
    style.textContent = css;
    document.head.appendChild(style);
    root = el('div');
    root.id = '__pt';
    layer = el('div');
    root.appendChild(layer);
    document.body.appendChild(root);
    document.addEventListener('mousemove', function (e) { mouse.x = e.clientX; mouse.y = e.clientY; hoverCard(); }, true);
    document.addEventListener('keydown', onKey, true);
    window.addEventListener('scroll', drawHighlights, true);
    window.addEventListener('resize', drawHighlights);
    setInterval(tick, 500);
    refresh();
  }

  function refresh() {
    return send({ type: 'hello' }).then(function (s) { state = s; render(); }).catch(function () {
      setTimeout(refresh, 500);
    });
  }

  function cur() { return state && state.current >= 0 ? state.step : null; }

  function render() {
    if (dock) dock.remove();
    layer.innerHTML = '';
    if (!state || hidden) return;
    if (state.busy) {
      // pointer-events:none, so the runner's own clicks during setup are
      // never covered by the overlay.
      dock = el('div', 'busy', 'Playtest: setting up the step…');
      root.appendChild(dock);
      return;
    }
    dock = el('div', 'dock ' + dockSide());
    var hd = el('div', 'hd');
    hd.appendChild(el('b', null, 'PLAYTEST'));
    hd.appendChild(document.createTextNode(' · ' + state.session));
    hd.appendChild(el('div', 'sub', state.title || ''));
    hd.appendChild(el('div', 'sub', state.progress || ''));
    dock.appendChild(hd);

    var body = el('div', 'body');
    var ul = el('ul', 'steps');
    state.steps.forEach(function (s, i) {
      var li = el('li', (s.you ? 'you' : '') + (state.step && state.step.id === s.id ? ' cur' : ''));
      li.appendChild(el('span', 'tag' + (s.you ? ' you' : ''), s.you ? 'YOU' : 'AUTO'));
      li.appendChild(el('span', null, s.id + ' ' + s.title));
      var r = s.judged || s.auto || '';
      li.appendChild(el('span', 'r ' + r, r));
      if (s.you) li.onclick = function () { send({ type: 'goto', id: s.id }); };
      ul.appendChild(li);
    });
    body.appendChild(ul);

    var st = cur();
    if (state.error) body.appendChild(el('div', 'err', state.error));
    if (st) {
      [['DO', st.do], ['EXPECT', st.expect], ['WHY', st.why]].forEach(function (p) {
        if (!p[1]) return;
        body.appendChild(el('h4', null, p[0]));
        body.appendChild(el('div', 'txt', p[1]));
      });
      if ((st.checks || []).length || (st.runner_checks || []).length) {
        body.appendChild(el('h4', null, 'CHECKS'));
        live = [];
        (st.checks || []).forEach(function (c) {
          var row = el('div', 'chk');
          var mark = el('span', 'pending', '·');
          row.appendChild(mark);
          row.appendChild(el('span', null, c.label));
          body.appendChild(row);
          live.push({ c: c, mark: mark });
        });
        (st.runner_checks || []).forEach(function (l) {
          var row = el('div', 'chk');
          row.appendChild(el('span', 'pending', '⏵'));
          row.appendChild(el('span', null, l + '  (runner, on Pass)'));
          body.appendChild(row);
        });
      }
    } else if (state.done) {
      body.appendChild(el('h4', null, 'DONE'));
      body.appendChild(el('div', 'txt', 'Every judgement step has a verdict. Results: ' + (state.results || '') + '\nClose this window (or Alt+Q) to finish, then run make playtest-report.'));
    }
    dock.appendChild(body);

    if (form) dock.appendChild(form.node);
    var btns = el('div', 'btns');
    function b(label, cls, fn) { var x = el('button', cls, label); x.onclick = fn; btns.appendChild(x); }
    if (st) {
      b('Pass  Alt+P', 'p', function () { verdict('pass'); });
      b('Fail  Alt+F', 'f', function () { askNote('fail'); });
      b('Skip  Alt+S', '', function () { verdict('skip'); });
      b('Replay  Alt+R', '', function () { send({ type: 'replay' }); });
    }
    b('Quit  Alt+Q', '', function () { send({ type: 'quit' }); });
    dock.appendChild(btns);
    root.appendChild(dock);
    drawHighlights();
  }

  // dockSide keeps the dock away from the step's targets.
  function dockSide() {
    var st = cur();
    if (!st || !window.__pt) return 'right';
    var right = 0, n = 0;
    (st.targets || []).forEach(function (t) {
      var e = __pt.first(t.id);
      if (!e) return;
      var r = e.getBoundingClientRect();
      n++;
      if (r.left + r.width / 2 > innerWidth * 0.6) right++;
    });
    return n && right * 2 > n ? 'left' : 'right';
  }

  function verdict(status, note) {
    form = null;
    send({ type: status, note: note || '' });
  }

  function askNote(kind, target) {
    var node = el('div', 'form');
    node.appendChild(el('div', 'sub', kind === 'fail' ? 'What went wrong? (Ctrl+Enter saves, Esc cancels)' : 'Opinion on ' + target + ' (Ctrl+Enter saves, Esc cancels)'));
    var ta = el('textarea');
    node.appendChild(ta);
    ta.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
        e.preventDefault();
        var note = ta.value.trim();
        form = null;
        if (kind === 'fail') verdict('fail', note);
        else send({ type: 'opinion', target: target, note: note }).then(render);
      } else if (e.key === 'Escape') {
        form = null;
        render();
      }
      e.stopPropagation();
    });
    form = { node: node };
    render();
    setTimeout(function () { ta.focus(); }, 0);
  }

  function underMouse() {
    var e = document.elementFromPoint(mouse.x, mouse.y);
    if (!e || root.contains(e)) return null;
    var t = e.closest('[data-testid]');
    return t ? t.getAttribute('data-testid') : __pt.cssPath(e);
  }

  function onKey(e) {
    if (e.key === 'F10') { hidden = !hidden; render(); e.preventDefault(); return; }
    if (!e.altKey) return;
    var k = e.key.toLowerCase(), st = cur();
    var acts = {
      p: function () { if (st) verdict('pass'); },
      f: function () { if (st) askNote('fail'); },
      s: function () { if (st) verdict('skip'); },
      r: function () { if (st) send({ type: 'replay' }); },
      n: function () { var t = underMouse(); if (t) askNote('opinion', t); },
      q: function () { send({ type: 'quit' }); }
    };
    if (acts[k]) { e.preventDefault(); e.stopPropagation(); acts[k](); }
  }

  // --- highlights -------------------------------------------------------

  function drawHighlights() {
    if (!layer) return;
    layer.innerHTML = '';
    var st = cur();
    if (!st || hidden || !window.__pt) return;
    (st.targets || []).forEach(function (t, i) {
      var els = __pt.find(t.id).filter(__pt.visible).slice(0, 8);
      if (!els.length) {
        var a = el('div', 'arrow', (i + 1) + ' ' + t.id + ' — not on this page');
        a.style.left = '12px'; a.style.top = (12 + 26 * i) + 'px';
        layer.appendChild(a);
        return;
      }
      els.forEach(function (e) {
        var r = e.getBoundingClientRect();
        var off = r.bottom < 0 ? '↑' : r.top > innerHeight ? '↓' : r.right < 0 ? '←' : r.left > innerWidth ? '→' : '';
        if (off) {
          var a = el('div', 'arrow', off + ' ' + (i + 1));
          a.style.left = Math.min(Math.max(r.left, 8), innerWidth - 60) + 'px';
          a.style.top = Math.min(Math.max(r.top, 8), innerHeight - 30) + 'px';
          layer.appendChild(a);
          return;
        }
        var h = el('div', 'hl');
        h.style.left = (r.left - 3) + 'px'; h.style.top = (r.top - 3) + 'px';
        h.style.width = (r.width + 6) + 'px'; h.style.height = (r.height + 6) + 'px';
        layer.appendChild(h);
        var c = el('div', 'chip', String(i + 1));
        c.style.left = (r.left - 10) + 'px'; c.style.top = (r.top - 12) + 'px';
        layer.appendChild(c);
      });
    });
    hoverCard();
  }

  var card = null;
  function hoverCard() {
    if (card) { card.remove(); card = null; }
    var st = cur();
    if (!st || hidden || !window.__pt) return;
    (st.targets || []).some(function (t, i) {
      return __pt.find(t.id).filter(__pt.visible).some(function (e) {
        var r = e.getBoundingClientRect();
        if (mouse.x < r.left || mouse.x > r.right || mouse.y < r.top || mouse.y > r.bottom) return false;
        card = el('div', 'card');
        card.appendChild(el('b', null, (i + 1) + ' · ' + t.id));
        if (t.note) card.appendChild(el('div', 'txt', t.note));
        if (st.expect) { card.appendChild(el('h4', null, 'EXPECT')); card.appendChild(el('div', 'txt', st.expect)); }
        if (st.why) { card.appendChild(el('h4', null, 'WHY')); card.appendChild(el('div', 'txt', st.why)); }
        card.style.left = Math.min(mouse.x + 16, innerWidth - 340) + 'px';
        card.style.top = Math.min(mouse.y + 16, innerHeight - 160) + 'px';
        root.appendChild(card);
        return true;
      });
    });
  }

  function tick() {
    if (!state || state.busy || !window.__pt) return;
    live.forEach(function (l) {
      var r = __pt.check(l.c, state.snaps || {});
      l.mark.textContent = r.pass ? '✓' : '✗';
      l.mark.className = r.pass ? 'pass' : 'fail';
      l.mark.title = 'actual: ' + r.actual;
    });
    drawHighlights();
  }

  window.__ptOverlay = {
    refresh: refresh,
    hide: function () { if (root) root.style.display = 'none'; },
    show: function () { if (root) root.style.display = ''; }
  };

  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
