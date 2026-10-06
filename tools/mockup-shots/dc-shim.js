// Minimal offline renderer for Claude Design canvas artboards (*.dc.html).
// Supports what static mockups use: {{path}} holes, <sc-for>, <sc-if>,
// <helmet>, and a DCLogic class with renderVals(). Event handlers are
// ignored; this exists only to take screenshots. shots.py rewrites
// <sc-for>/<sc-if> into <template> before loading so tables parse correctly.
(function () {
  window.DCLogic = class {
    constructor(props) { this.props = props || {}; this.state = {}; }
    setState(p) { Object.assign(this.state, p); }
    forceUpdate() {}
  };

  const HOLE = /\{\{\s*([\w.$]+)\s*\}\}/g;
  const WHOLE = /^\{\{\s*([\w.$]+)\s*\}\}$/;

  function lookup(path, scope) {
    if (path === 'true') return true;
    if (path === 'false') return false;
    if (/^-?\d+(\.\d+)?$/.test(path)) return Number(path);
    return path.split('.').reduce((o, k) => (o == null ? undefined : o[k]), scope);
  }

  function interp(text, scope) {
    return text.replace(HOLE, (_, p) => {
      const v = lookup(p, scope);
      return v == null ? '' : String(v);
    });
  }

  function renderNodes(nodes, scope, parent) {
    nodes.forEach((n) => renderNode(n, scope, parent));
  }

  function renderNode(node, scope, parent) {
    if (node.nodeType === Node.TEXT_NODE) {
      parent.appendChild(document.createTextNode(interp(node.textContent, scope)));
      return;
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    if (node.tagName === 'TEMPLATE' && node.dataset.sc) {
      const kids = Array.from(node.content.childNodes);
      if (node.dataset.sc === 'for') {
        const list = lookup((node.getAttribute('list').match(WHOLE) || [])[1], scope) || [];
        const as = node.getAttribute('as') || 'item';
        list.forEach((item, i) => renderNodes(kids, Object.assign({}, scope, { [as]: item, $index: i }), parent));
      } else if (node.dataset.sc === 'if') {
        const m = node.getAttribute('value').match(WHOLE);
        if (m && lookup(m[1], scope)) renderNodes(kids, scope, parent);
      }
      return;
    }
    const el = document.createElement(node.tagName.toLowerCase());
    for (const a of Array.from(node.attributes)) {
      if (/^on[A-Z]/.test(a.name) || /^on[a-z]/.test(a.name)) continue;
      const m = a.value.match(WHOLE);
      if (m) {
        const v = lookup(m[1], scope);
        if (v === false || v == null || typeof v === 'function') continue;
        el.setAttribute(a.name, v === true ? '' : String(v));
      } else {
        el.setAttribute(a.name, interp(a.value, scope));
      }
    }
    const src = node.tagName === 'TEMPLATE' ? node.content.childNodes : node.childNodes;
    renderNodes(Array.from(src), scope, el);
    parent.appendChild(el);
  }

  document.addEventListener('DOMContentLoaded', () => {
    const root = document.querySelector('x-dc');
    const script = document.querySelector('script[data-dc-script]');
    if (!root || !script) return;
    const Component = new Function(script.textContent + '\n;return Component;')();
    const vals = new Component({}).renderVals() || {};
    const helmet = root.querySelector('helmet');
    if (helmet) Array.from(helmet.childNodes).forEach((c) => document.head.appendChild(c));
    const out = document.createElement('div');
    renderNodes(Array.from(root.childNodes).filter((c) => c !== helmet), vals, out);
    root.replaceWith(out);
  });
})();
