// Stats page (templates/stats.html). The page works without this file:
// every option is a link or a GET form, every chart has its table in a
// <details>. This adds three conveniences: a Table button that swaps a
// chart for its table, the group-by select applying on change, and the
// last options remembered per project (localStorage, which may throw).
(function () {
    var page = document.querySelector('.stats-page');
    if (!page) return;
    page.classList.add('stats-js');

    var key = 'stats:' + (page.getAttribute('data-project') || '');
    var params = new URLSearchParams(location.search);
    params.delete('drill');
    try {
        if (!location.search) {
            var saved = localStorage.getItem(key);
            if (saved) {
                location.replace(location.pathname + '?' + saved + location.hash);
                return;
            }
        } else if (!params.has('days')) {
            localStorage.setItem(key, params.toString());
        }
        var note = page.querySelector('.stats-saved');
        if (note && localStorage.getItem(key)) note.hidden = false;
    } catch (e) {
        // No storage (private window, blocked site data): options stay in
        // the URL only.
    }

    page.querySelectorAll('.js-stats-table-btn').forEach(function (btn) {
        var card = btn.closest('.stats-card');
        var table = document.getElementById(btn.getAttribute('aria-controls'));
        if (!card || !table) return;
        btn.hidden = false;
        btn.addEventListener('click', function () {
            var on = !card.classList.contains('show-table');
            card.classList.toggle('show-table', on);
            table.open = on;
            btn.setAttribute('aria-pressed', on ? 'true' : 'false');
        });
    });

    var group = page.querySelector('.stats-group select');
    if (group) {
        group.addEventListener('change', function () { group.form.submit(); });
    }
})();
