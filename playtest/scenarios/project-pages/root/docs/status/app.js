// Atlas status page: a sandboxed project page (opaque origin).
document.querySelector('[data-testid="fixture.page.script_ran"]').textContent = 'yes';

// The viewer must refuse a write from the page: the request carries
// Origin: null and gets a 403; the page can't read the answer either way.
var base = location.pathname.split('/page-raw/')[0];
var result = document.querySelector('[data-testid="fixture.page.post_result"]');
fetch(base + '/issue/ui/route-planner-panel/dispatch', {method: 'POST'})
  .then(function(r) { result.textContent = 'sent ' + r.status; })
  .catch(function() { result.textContent = 'blocked'; });

// Live counts from the viewer frame.
window.addEventListener('message', function(e) {
  if (!e.data || e.data.type !== 'issue-viewer.status') return;
  document.querySelector('[data-testid="fixture.page.status_open"]').textContent = String(e.data.status.open);
});
if (window.parent !== window) window.parent.postMessage({type: 'issue-viewer.status.request'}, '*');
