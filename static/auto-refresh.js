(function() {
    var match = location.pathname.match(/^(\/p\/[^/]+)/);
    if (!match) return;
    var prefix = match[1];
    var hashUrl = prefix + '/hash';
    var issuesUrl = prefix + '/issues.json';
    var lastHash = null;
    var interval = 3000;

    // Color maps matching server-side template functions
    var statusColors = {
        'idea': '#8b5cf6', 'in design': '#3b82f6', 'backlog': '#64748b',
        'in progress': '#eab308', 'testing': '#f97316', 'human-testing': '#ec4899',
        'documentation': '#14b8a6', 'shipping': '#0ea5e9', 'done': '#22c55e'
    };
    var statusTextColors = { 'in progress': '#000000', 'testing': '#000000', 'human-testing': '#000000' };
    var priorityColors = { 'low': '#6b7280', 'medium': '#3b82f6', 'high': '#f97316', 'critical': '#ef4444' };
    var assigneeColorPalette = ['#f97316','#3b82f6','#22c55e','#a855f7','#ef4444','#eab308','#14b8a6','#ec4899','#6366f1','#84cc16'];

    function statusColor(s) { return statusColors[s] || '#6b7280'; }
    function statusTextColor(s) { return statusTextColors[s] || '#ffffff'; }
    function priorityColor(p) { return priorityColors[p] || '#6b7280'; }
    function assigneeColor(name) {
        if (!name) return '';
        var h = 0;
        for (var i = 0; i < name.length; i++) h = h * 31 + name.charCodeAt(i);
        if (h < 0) h = -h;
        return assigneeColorPalette[h % assigneeColorPalette.length];
    }

    function renderProjectActiveBots(issues) {
        var summary = document.getElementById('project-active-bots');
        if (!summary) return;

        var seen = {};
        var count = 0;
        issues.forEach(function(iss) {
            (iss.active_sessions || []).forEach(function(session) {
                if (!seen[session.name]) {
                    seen[session.name] = true;
                    count++;
                }
            });
        });

        summary.dataset.count = String(count);
        summary.textContent = count + ' active bot' + (count === 1 ? '' : 's');
    }

    function renderActiveChip(container, className, sessions) {
        var chip = container.querySelector('.' + className);
        if (sessions && sessions.length) {
            if (!chip) {
                chip = document.createElement('span');
                chip.className = className;
                container.appendChild(chip);
            }
            chip.textContent = sessions.length + ' agent active' + (sessions.length === 1 ? '' : 's');
        } else if (chip) {
            chip.remove();
        }
    }

    function detectView() {
        var path = location.pathname.replace(prefix, '');
        if (path === '/board') return 'board';
        if (path.indexOf('/issue/') === 0) return 'detail';
        return 'list';
    }

    function updateBoard(issues) {
        renderProjectActiveBots(issues);
        var issueMap = {};
        issues.forEach(function(iss) { issueMap[iss.slug] = iss; });

        // Track which slugs exist on server
        var serverSlugs = {};
        issues.forEach(function(iss) { serverSlugs[iss.slug] = true; });

        // Track which slugs exist in DOM
        var domSlugs = {};
        document.querySelectorAll('.board-card-wrapper').forEach(function(w) {
            domSlugs[w.dataset.slug] = true;
        });

        // If new issues appeared or issues were deleted, we need a full reload
        // since we can't render new card HTML client-side
        var needsReload = false;
        for (var slug in serverSlugs) {
            if (!domSlugs[slug]) { needsReload = true; break; }
        }
        for (var slug in domSlugs) {
            if (!serverSlugs[slug]) {
                // Issue deleted — just remove the card
                var del = document.querySelector('.board-card-wrapper[data-slug="' + slug + '"]');
                if (del) del.remove();
            }
        }
        if (needsReload) { location.reload(); return; }

        // Move cards between columns if status changed
        document.querySelectorAll('.board-card-wrapper').forEach(function(wrapper) {
            var slug = wrapper.dataset.slug;
            var iss = issueMap[slug];
            if (!iss) return;

            var currentCol = wrapper.closest('.board-column');
            if (!currentCol) return;
            var currentStatus = currentCol.dataset.status;

            if (currentStatus !== iss.status) {
                // Find target column
                var targetCol = document.querySelector('.board-column[data-status="' + iss.status + '"]');
                if (targetCol) {
                    var targetCards = targetCol.querySelector('.board-column-cards');
                    targetCards.appendChild(wrapper);
                }
            }

            // Update assignee styling on the card
            var card = wrapper.querySelector('.board-card');
            if (!card) return;

            if (iss.assignee) {
                card.classList.add('board-card-assigned');
                card.style.borderLeft = '3px solid ' + assigneeColor(iss.assignee);
            } else {
                card.classList.remove('board-card-assigned');
                card.style.borderLeft = '';
            }

            // Update assignee text
            var assigneeEl = card.querySelector('.board-card-assignee');
            if (iss.assignee) {
                if (!assigneeEl) {
                    assigneeEl = document.createElement('div');
                    assigneeEl.className = 'board-card-assignee';
                    card.appendChild(assigneeEl);
                }
                assigneeEl.innerHTML = '<span class="assignee-dot" style="background:' + assigneeColor(iss.assignee) + '"></span> ' + iss.assignee;
            } else if (assigneeEl) {
                assigneeEl.remove();
            }

            // Update status dot color
            var dot = card.querySelector('.board-card-dot');
            if (dot) dot.style.borderColor = statusColor(iss.status);

            renderActiveChip(card, 'board-agent-active', iss.active_sessions);
        });

        // Update all column counts
        document.querySelectorAll('.board-column').forEach(function(col) {
            var count = col.querySelectorAll('.board-card-wrapper').length;
            var countEl = col.querySelector('.board-column-count');
            if (countEl) countEl.textContent = count;
        });

        // Update total count in header
        var totalEl = document.querySelector('.count');
        if (totalEl) totalEl.textContent = issues.length;
    }

    function updateList(issues) {
        renderProjectActiveBots(issues);
        var issueMap = {};
        issues.forEach(function(iss) { issueMap[iss.slug] = iss; });

        document.querySelectorAll('.issue-row').forEach(function(row) {
            var link = row.querySelector('.issue-title');
            if (!link) return;
            var href = link.getAttribute('href');
            // Extract slug from href: prefix/issue/<slug>
            var slugMatch = href.match(/\/issue\/(.+?)(?:\?|$)/);
            if (!slugMatch) return;
            var slug = slugMatch[1];
            var iss = issueMap[slug];
            if (!iss) return;

            // Update status badge
            var badge = row.querySelector('.badge');
            if (badge) {
                badge.textContent = iss.status;
                badge.style.background = statusColor(iss.status);
                badge.style.color = statusTextColor(iss.status);
            }

            // Update priority
            var prioSpan = row.querySelector('.priority-dot');
            if (prioSpan && iss.priority) {
                prioSpan.style.background = priorityColor(iss.priority);
                var prioText = prioSpan.parentElement.querySelector('span:last-child');
                if (prioText) prioText.textContent = iss.priority;
            }

            // Update assignee
            var assigneeTag = row.querySelector('.assignee-tag');
            var rightDiv = row.querySelector('.issue-right');
            if (iss.assignee) {
                if (!assigneeTag) {
                    assigneeTag = document.createElement('span');
                    assigneeTag.className = 'assignee-tag';
                    rightDiv.insertBefore(assigneeTag, rightDiv.firstChild);
                }
                var ac = assigneeColor(iss.assignee);
                assigneeTag.style.borderColor = ac;
                assigneeTag.innerHTML = '<span class="assignee-dot" style="background:' + ac + '"></span> ' + iss.assignee;
            } else if (assigneeTag) {
                assigneeTag.remove();
            }

            var titleRow = row.querySelector('.issue-title-row');
            if (titleRow) renderActiveChip(titleRow, 'agent-active-chip', iss.active_sessions);
        });
    }

    function updateDetail(issues) {
        renderProjectActiveBots(issues);
        // Find current issue slug from URL
        var path = location.pathname.replace(prefix, '');
        var slugMatch = path.match(/^\/issue\/(.+)/);
        if (!slugMatch) return;
        var slug = slugMatch[1];

        var iss = null;
        for (var i = 0; i < issues.length; i++) {
            if (issues[i].slug === slug) { iss = issues[i]; break; }
        }
        if (!iss) return;

        // Update status badge in sidebar
        var badges = document.querySelectorAll('.badge');
        badges.forEach(function(badge) {
            // Find the status badge (in the sidebar meta section)
            var parent = badge.closest('.detail-meta-value, .issue-title-row, .detail-sidebar');
            if (parent) {
                badge.textContent = iss.status;
                badge.style.background = statusColor(iss.status);
                badge.style.color = statusTextColor(iss.status);
            }
        });

        var section = document.getElementById('active-agent-section');
        var list = document.getElementById('active-session-list');
        if (section && list) {
            if (iss.active_sessions && iss.active_sessions.length) {
                section.style.display = '';
                list.innerHTML = '';
                iss.active_sessions.forEach(function(session) {
                    var item = document.createElement('div');
                    item.className = 'active-session-item';

                    var name = document.createElement('div');
                    name.className = 'active-session-name';
                    name.textContent = session.name;
                    item.appendChild(name);

                    if (session.start_time) {
                        var time = document.createElement('div');
                        time.className = 'active-session-time';
                        time.textContent = session.start_time;
                        item.appendChild(time);
                    }

                    list.appendChild(item);
                });
            } else {
                section.style.display = 'none';
                list.innerHTML = '';
            }
        }
    }

    function poll() {
        fetch(hashUrl).then(function(res) {
            return res.json();
        }).then(function(data) {
            if (lastHash === null) {
                lastHash = data.hash;
            } else if (data.hash !== lastHash) {
                lastHash = data.hash;
                // Fetch fresh issue data and update DOM in place
                fetch(issuesUrl).then(function(res) {
                    return res.json();
                }).then(function(issues) {
                    var view = detectView();
                    if (view === 'board') updateBoard(issues);
                    else if (view === 'list') updateList(issues);
                    else if (view === 'detail') location.reload();
                }).catch(function() {});
            }
        }).catch(function() {});
    }

    setInterval(poll, interval);
    poll();
})();
