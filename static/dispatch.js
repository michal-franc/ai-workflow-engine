// Shown in the dispatch dialogs when the project is headless (terminal: none,
// set or detected): the command the human runs to reach the agent.
function showAttachCommand(stepsEl, cmd) {
    var box = document.createElement('div');
    box.className = 'dispatch-attach';
    var label = document.createElement('span');
    label.textContent = 'No terminal opened. Attach with:';
    var code = document.createElement('code');
    code.textContent = cmd;
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'dispatch-attach-copy';
    btn.textContent = 'Copy';
    btn.addEventListener('click', function() {
        var done = function() { btn.textContent = 'Copied'; };
        var select = function() {
            var range = document.createRange();
            range.selectNodeContents(code);
            var sel = window.getSelection();
            sel.removeAllRanges();
            sel.addRange(range);
            btn.textContent = 'Selected: press Ctrl+C';
        };
        if (navigator.clipboard && navigator.clipboard.writeText) {
            navigator.clipboard.writeText(cmd).then(done, select);
        } else {
            select();
        }
    });
    box.appendChild(label);
    box.appendChild(code);
    box.appendChild(btn);
    stepsEl.appendChild(box);
}
