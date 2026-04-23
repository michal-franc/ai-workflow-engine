// Apply saved theme immediately to prevent flash
(function() {
    var t = localStorage.getItem('theme') || 'dark';
    document.documentElement.setAttribute('data-theme', t);
})();

function initThemePicker() {
    var picker = document.querySelector('.theme-picker');
    if (!picker) return;
    var current = localStorage.getItem('theme') || 'dark';

    picker.querySelectorAll('.theme-btn').forEach(function(btn) {
        if (btn.dataset.theme === current) btn.classList.add('active');
        btn.addEventListener('click', function() {
            var theme = this.dataset.theme;
            document.documentElement.setAttribute('data-theme', theme);
            localStorage.setItem('theme', theme);
            picker.querySelectorAll('.theme-btn').forEach(function(b) { b.classList.remove('active'); });
            this.classList.add('active');
        });
    });
}

document.addEventListener('DOMContentLoaded', initThemePicker);
