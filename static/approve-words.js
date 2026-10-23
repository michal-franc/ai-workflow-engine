// The words box under approve buttons (templates/_approve_words.html):
// approveWords.html builds it for pages that render in JS, take reads it for
// the approve request, clear empties it once the words are saved.
window.approveWords = (function() {
    function box(where) { return document.getElementById('approve-words-' + where); }
    function esc(s) {
        return String(s).replace(/[&<>"']/g, function(c) {
            return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
        });
    }
    return {
        html: function(where) {
            var id = 'approve-words-' + esc(where);
            return '<div class="approve-words"><label for="' + id + '">Your words (optional)</label>'
                + '<textarea id="' + id + '" data-testid="approve_words.' + esc(where) + '" rows="2"'
                + ' placeholder="Why you approve. Saved with the approval and sent to the agent."></textarea></div>';
        },
        take: function(where) {
            var el = box(where);
            return el ? el.value.trim() : '';
        },
        clear: function(where) {
            var el = box(where);
            if (el) el.value = '';
        }
    };
})();
