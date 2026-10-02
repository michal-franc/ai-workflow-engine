// Fills the "Inbox" nav badge with the number of items waiting on a human.
(function() {
    var match = location.pathname.match(/^(\/p\/[^/]+)/);
    if (!match) return;
    var url = match[1] + '/inbox.json';
    function update() {
        fetch(url).then(function(r) { return r.json(); }).then(function(data) {
            document.querySelectorAll('[data-inbox-count]').forEach(function(el) {
                el.textContent = data.count > 0 ? String(data.count) : '';
                el.title = data.count > 0 ? data.count + ' waiting · oldest ' + data.oldest : '';
            });
        }).catch(function() {});
    }
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', update);
    } else {
        update();
    }
    setInterval(update, 30000);
})();
