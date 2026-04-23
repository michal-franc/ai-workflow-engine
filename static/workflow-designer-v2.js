(function () {
  window.__workflowDesignerV2 = true;

  const INITIAL_WORKFLOW = JSON.parse(document.getElementById('workflow-json').textContent);
  const ISSUE_OPTIONS = parseIssueOptions(document.getElementById('workflow-issues')?.textContent || '[]');
  const INITIAL_YAML = document.getElementById('workflow-yaml').textContent.trim();
  const STORAGE_KEY = 'workflow-designer:' + location.pathname;
  const SAVE_URL = location.pathname;
  const DATA_URL = location.pathname.replace(/\/workflow-designer$/, '/workflow-designer/data');
  const PREVIEW_URL = location.pathname.replace(/\/workflow-designer$/, '/workflow-designer/preview');

  const ACTION_TYPES = [
    { type: 'validate', title: 'Validate', color: '#d97706', desc: 'Run a configured validation rule before the transition completes.' },
    { type: 'append_section', title: 'Append Section', color: '#059669', desc: 'Create or reuse a section and append markdown into it.' },
    { type: 'inject_prompt', title: 'Inject Prompt', color: '#ea580c', desc: 'Add transition-specific guidance for the next agent.' },
    { type: 'require_human_approval', title: 'Human Approval', color: '#db2777', desc: 'Block until the issue is approved for a target status.' },
    { type: 'set_fields', title: 'Set Fields', color: '#7c3aed', desc: 'Update selected frontmatter fields during the transition.' },
  ];

  const VALIDATION_RULES = [
    { value: 'body_not_empty', label: 'Body Not Empty', description: 'Blocks the transition until the issue body has some content.' },
    { value: 'has_checkboxes', label: 'Has Checkboxes', description: 'Requires at least one checkbox anywhere in the issue body.' },
    { value: 'has_assignee', label: 'Has Assignee', description: 'Requires the issue to be claimed before the transition can continue.' },
    { value: 'all_checkboxes_checked', label: 'All Checkboxes Checked', description: 'Requires every checkbox in the issue body to be checked.' },
    { value: 'section_checkboxes_checked', label: 'Section Checkboxes Checked', description: 'Requires all checkboxes inside one named section to be checked.', argLabel: 'Section Name', argPlaceholder: 'Implementation' },
    { value: 'has_test_plan', label: 'Has Test Plan', description: 'Requires a `## Test Plan` section with `### Automated` and `### Manual` subsections.' },
    { value: 'has_comment_prefix', label: 'Has Comment Prefix', description: 'Requires at least one issue comment starting with a specific prefix.', argLabel: 'Comment Prefix', argPlaceholder: 'tests:' },
    { value: 'approved_for', label: 'Approved For', description: 'Requires the issue approval metadata to match a target status.', argLabel: 'Approved Status', argPlaceholder: 'backlog' },
  ];

  const SET_FIELD_OPTIONS = [
    { value: 'assignee', label: 'Assignee', description: 'Assign or clear the issue assignee.' },
    { value: 'approved_for', label: 'Approved For', description: 'Update the human approval target status.' },
    { value: 'priority', label: 'Priority', description: 'Set or clear the issue priority.' },
    { value: 'status', label: 'Status', description: 'Override the resulting issue status.' },
  ];

  const EMPTY_WORKFLOW = normalizeWorkflow(INITIAL_WORKFLOW);

  let state = {
    workflow: EMPTY_WORKFLOW,
    selected: null,
    draftLoaded: false,
    serverSource: document.getElementById('workflow-source-badge')?.textContent || 'built-in default workflow',
    serverTarget: document.getElementById('workflow-target-badge')?.textContent || 'workflow.yaml',
  };

  let exportState = { title: '', text: '', filename: '', mode: 'export' };
  let debugEvents = [];
  let toastTimer = null;

  function freshTesterState() {
    return {
      slug: '',
      to: '',
      transition: '',
      loading: false,
      viabilityLoading: false,
      viableOnly: false,
      viability: [],
      result: null,
      error: '',
    };
  }

  let testerState = freshTesterState();

  function parseIssueOptions(raw) {
    try {
      let parsed = JSON.parse(raw || '[]');
      if (typeof parsed === 'string') {
        parsed = JSON.parse(parsed || '[]');
      }
      if (Array.isArray(parsed)) return parsed;
      if (parsed && Array.isArray(parsed.issues)) return parsed.issues;
      if (parsed && typeof parsed === 'object') return Object.values(parsed);
      return [];
    } catch (error) {
      return [];
    }
  }

  function countIndent(line) {
    const match = line.match(/^ */);
    return match ? match[0].length : 0;
  }

  function yamlQuote(text) {
    return '"' + String(text ?? '').replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"';
  }

  function unquoteYAML(value) {
    const trimmed = String(value ?? '').trim();
    if ((trimmed.startsWith('"') && trimmed.endsWith('"')) || (trimmed.startsWith("'") && trimmed.endsWith("'"))) {
      return trimmed.slice(1, -1).replace(/\\"/g, '"').replace(/\\\\/g, '\\');
    }
    return trimmed;
  }

  function escapeHTML(value) {
    return String(value ?? '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function escapeAttr(value) {
    return escapeHTML(value).replace(/'/g, '&#39;');
  }

  function normalizeStatus(raw) {
    return {
      name: String(raw?.name ?? raw?.Name ?? '').trim(),
      description: String(raw?.description ?? raw?.Description ?? '').trim(),
      prompt: String(raw?.prompt ?? raw?.Prompt ?? '').replace(/\r/g, '').replace(/\n+$/, ''),
    };
  }

  function normalizeAction(raw) {
    return {
      type: String(raw?.type ?? raw?.Type ?? '').trim(),
      rule: String(raw?.rule ?? raw?.Rule ?? '').trim(),
      status: String(raw?.status ?? raw?.Status ?? '').trim(),
      title: String(raw?.title ?? raw?.Title ?? '').trim(),
      body: String(raw?.body ?? raw?.Body ?? '').replace(/\r/g, '').replace(/\n+$/, ''),
      prompt: String(raw?.prompt ?? raw?.Prompt ?? '').replace(/\r/g, '').replace(/\n+$/, ''),
      field: String(raw?.field ?? raw?.Field ?? '').trim(),
      value: String(raw?.value ?? raw?.Value ?? '').trim(),
    };
  }

  function normalizeTransition(raw) {
    return {
      from: String(raw?.from ?? raw?.From ?? '').trim(),
      to: String(raw?.to ?? raw?.To ?? '').trim(),
      actions: (raw?.actions ?? raw?.Actions ?? []).map(normalizeAction).filter(action => action.type),
    };
  }

  function normalizeSystems(raw) {
    const out = {};
    Object.keys(raw || {}).forEach(name => {
      const overlay = raw[name] || {};
      out[name] = {
        statuses: (overlay.statuses ?? overlay.Statuses ?? []).map(normalizeStatus).filter(status => status.name),
        transitions: (overlay.transitions ?? overlay.Transitions ?? []).map(normalizeTransition).filter(transition => transition.from && transition.to),
      };
    });
    return out;
  }

  function normalizeWorkflow(raw) {
    return {
      statuses: (raw?.statuses ?? raw?.Statuses ?? []).map(normalizeStatus).filter(status => status.name),
      transitions: (raw?.transitions ?? raw?.Transitions ?? []).map(normalizeTransition).filter(transition => transition.from && transition.to),
      systems: normalizeSystems(raw?.systems ?? raw?.Systems),
    };
  }

  function cloneWorkflow(workflow) {
    return normalizeWorkflow(JSON.parse(JSON.stringify(workflow || EMPTY_WORKFLOW)));
  }

  function parseValidationRule(rule) {
    const normalized = String(rule || '').trim();
    const idx = normalized.indexOf(': ');
    if (idx === -1) return { type: normalized, arg: '' };
    return { type: normalized.slice(0, idx), arg: normalized.slice(idx + 2) };
  }

  function composeValidationRule(type, arg) {
    const trimmedType = String(type || '').trim();
    const trimmedArg = String(arg || '').trim();
    if (!trimmedType) return '';
    if (trimmedType === 'section_checkboxes_checked' || trimmedType === 'has_comment_prefix' || trimmedType === 'approved_for') {
      return trimmedArg ? `${trimmedType}: ${trimmedArg}` : trimmedType;
    }
    return trimmedType;
  }

  function validationRuleMeta(rule) {
    const parsed = parseValidationRule(rule);
    return VALIDATION_RULES.find(item => item.value === parsed.type) || null;
  }

  function validationRuleSummary(rule) {
    const parsed = parseValidationRule(rule);
    const meta = validationRuleMeta(rule);
    if (!meta) return rule || 'Validation rule';
    return parsed.arg ? `${meta.label}: ${parsed.arg}` : meta.label;
  }

  function setFieldMeta(field) {
    return SET_FIELD_OPTIONS.find(item => item.value === String(field || '').trim()) || null;
  }

  function actionMeta(type) {
    return ACTION_TYPES.find(item => item.type === type) || ACTION_TYPES[0];
  }

  function actionSummary(action) {
    const normalized = normalizeAction(action);
    switch (normalized.type) {
      case 'validate':
        return validationRuleSummary(normalized.rule);
      case 'append_section':
        return normalized.title ? `Append ${normalized.title}` : 'Append section content';
      case 'inject_prompt':
        return normalized.prompt ? normalized.prompt.split('\n')[0].slice(0, 72) : 'Injected guidance';
      case 'require_human_approval':
        return normalized.status ? `Requires approval for ${normalized.status}` : 'Requires human approval';
      case 'set_fields':
        return normalized.field ? `${normalized.field} = ${normalized.value || '""'}` : 'Set frontmatter field';
      default:
        return normalized.type || 'Action';
    }
  }

  function emitBlock(lines, indent, key, value) {
    lines.push(`${indent}${key}: |`);
    value.replace(/\r/g, '').split('\n').forEach(line => {
      lines.push(`${indent}  ${line}`);
    });
  }

  function emitAction(lines, indent, action) {
    const normalized = normalizeAction(action);
    lines.push(`${indent}- type: ${yamlQuote(normalized.type)}`);
    if (normalized.rule) lines.push(`${indent}  rule: ${yamlQuote(normalized.rule)}`);
    if (normalized.status) lines.push(`${indent}  status: ${yamlQuote(normalized.status)}`);
    if (normalized.title) lines.push(`${indent}  title: ${yamlQuote(normalized.title)}`);
    if (normalized.body) emitBlock(lines, `${indent}  `, 'body', normalized.body);
    if (normalized.prompt) emitBlock(lines, `${indent}  `, 'prompt', normalized.prompt);
    if (normalized.field) lines.push(`${indent}  field: ${yamlQuote(normalized.field)}`);
    if (normalized.value !== '') lines.push(`${indent}  value: ${yamlQuote(normalized.value)}`);
    if (normalized.value === '' && normalized.type === 'set_fields') lines.push(`${indent}  value: ""`);
  }

  function workflowToYAML(workflow) {
    const normalized = simplifyWorkflow(workflow);
    const lines = ['statuses:'];
    normalized.statuses.forEach(status => {
      lines.push(`  - name: ${yamlQuote(status.name)}`);
      if (status.description) lines.push(`    description: ${yamlQuote(status.description)}`);
      if (status.prompt) emitBlock(lines, '    ', 'prompt', status.prompt);
    });

    lines.push('');
    lines.push('transitions:');
    if (!normalized.transitions.length) {
      lines.push('  []');
    } else {
      normalized.transitions.forEach(transition => {
        lines.push(`  - from: ${yamlQuote(transition.from)}`);
        lines.push(`    to: ${yamlQuote(transition.to)}`);
        lines.push('    actions:');
        if (!transition.actions.length) {
          lines.push('      []');
        } else {
          transition.actions.forEach(action => emitAction(lines, '      ', action));
        }
      });
    }

    const systemNames = Object.keys(normalized.systems || {});
    if (systemNames.length) {
      lines.push('');
      lines.push('systems:');
      systemNames.forEach(name => {
        const overlay = normalized.systems[name] || { statuses: [], transitions: [] };
        lines.push(`  ${name}:`);
        if (overlay.statuses?.length) {
          lines.push('    statuses:');
          overlay.statuses.forEach(status => {
            lines.push(`      - name: ${yamlQuote(status.name)}`);
            if (status.description) lines.push(`        description: ${yamlQuote(status.description)}`);
            if (status.prompt) emitBlock(lines, '        ', 'prompt', status.prompt);
          });
        }
        lines.push('    transitions:');
        if (!overlay.transitions?.length) {
          lines.push('      []');
        } else {
          overlay.transitions.forEach(transition => {
            lines.push(`      - from: ${yamlQuote(transition.from)}`);
            lines.push(`        to: ${yamlQuote(transition.to)}`);
            lines.push('        actions:');
            if (!transition.actions.length) {
              lines.push('          []');
            } else {
              transition.actions.forEach(action => emitAction(lines, '          ', action));
            }
          });
        }
      });
    }

    return lines.join('\n');
  }

  function parseActions(lines, startIndex, actionIndent) {
    const actions = [];
    let i = startIndex;
    while (i < lines.length) {
      const line = lines[i];
      const trimmed = line.trim();
      const indent = countIndent(line);
      if (!trimmed || trimmed.startsWith('#')) {
        i++;
        continue;
      }
      if (indent < actionIndent) break;
      if (indent === actionIndent && trimmed === '[]') {
        i++;
        break;
      }
      if (indent !== actionIndent || !trimmed.startsWith('- type:')) break;

      const action = normalizeAction({ type: unquoteYAML(trimmed.slice('- type:'.length)) });
      i++;
      while (i < lines.length) {
        const inner = lines[i];
        const innerTrimmed = inner.trim();
        const innerIndent = countIndent(inner);
        if (!innerTrimmed || innerTrimmed.startsWith('#')) {
          i++;
          continue;
        }
        if (innerIndent <= actionIndent) break;
        if (innerIndent !== actionIndent + 2) {
          i++;
          continue;
        }
        const block = innerTrimmed.match(/^([a-z_]+):\s*\|\s*$/);
        const scalar = innerTrimmed.match(/^([a-z_]+):\s*(.*)$/);
        if (block) {
          const field = block[1];
          i++;
          const body = [];
          while (i < lines.length) {
            const bodyLine = lines[i];
            const bodyTrimmed = bodyLine.trim();
            const bodyIndent = countIndent(bodyLine);
            if (!bodyTrimmed && bodyIndent >= actionIndent + 4) {
              body.push('');
              i++;
              continue;
            }
            if (bodyIndent < actionIndent + 4) break;
            body.push(bodyLine.slice(actionIndent + 4));
            i++;
          }
          action[field] = body.join('\n').replace(/\n+$/, '');
          continue;
        }
        if (scalar) action[scalar[1]] = unquoteYAML(scalar[2]);
        i++;
      }
      actions.push(normalizeAction(action));
    }
    return [actions, i];
  }

  function parseTransition(lines, startIndex, transitionIndent, actionIndent) {
    const transition = normalizeTransition({
      from: unquoteYAML(lines[startIndex].trim().slice('- from:'.length)),
      to: '',
      actions: [],
    });
    let i = startIndex + 1;
    while (i < lines.length) {
      const line = lines[i];
      const trimmed = line.trim();
      const indent = countIndent(line);
      if (!trimmed || trimmed.startsWith('#')) {
        i++;
        continue;
      }
      if (indent < transitionIndent + 2) break;
      if (indent === transitionIndent && trimmed.startsWith('- from:')) break;
      if (indent === transitionIndent + 2 && trimmed.startsWith('to:')) {
        transition.to = unquoteYAML(trimmed.slice('to:'.length));
        i++;
        continue;
      }
      if (indent === transitionIndent + 2 && trimmed === 'actions:') {
        const parsed = parseActions(lines, i + 1, actionIndent);
        transition.actions = parsed[0];
        i = parsed[1];
        continue;
      }
      i++;
    }
    return [transition, i];
  }

  function parseYAMLWorkflow(text) {
    const lines = text.replace(/\r/g, '').split('\n');
    const workflow = { statuses: [], transitions: [], systems: {} };
    let section = '';
    let i = 0;
    while (i < lines.length) {
      const line = lines[i];
      const trimmed = line.trim();
      const indent = countIndent(line);
      if (!trimmed || trimmed.startsWith('#')) {
        i++;
        continue;
      }
      if (indent === 0) {
        if (trimmed === 'statuses:') {
          section = 'statuses';
          i++;
          continue;
        }
        if (trimmed === 'transitions:') {
          section = 'transitions';
          i++;
          continue;
        }
        if (trimmed === 'systems:') {
          section = 'systems';
          i++;
          continue;
        }
      }

      if (section === 'statuses' && indent === 2 && trimmed.startsWith('- name:')) {
        const status = normalizeStatus({ name: unquoteYAML(trimmed.slice('- name:'.length)) });
        i++;
        while (i < lines.length) {
          const inner = lines[i];
          const innerTrimmed = inner.trim();
          const innerIndent = countIndent(inner);
          if (!innerTrimmed || innerTrimmed.startsWith('#')) {
            i++;
            continue;
          }
          if (innerIndent <= 2) break;
          if (innerIndent === 4 && innerTrimmed.startsWith('description:')) {
            status.description = unquoteYAML(innerTrimmed.slice('description:'.length));
            i++;
            continue;
          }
          if (innerIndent === 4 && innerTrimmed === 'prompt: |') {
            i++;
            const body = [];
            while (i < lines.length) {
              const bodyLine = lines[i];
              const bodyTrimmed = bodyLine.trim();
              const bodyIndent = countIndent(bodyLine);
              if (!bodyTrimmed && bodyIndent >= 6) {
                body.push('');
                i++;
                continue;
              }
              if (bodyIndent < 6) break;
              body.push(bodyLine.slice(6));
              i++;
            }
            status.prompt = body.join('\n').replace(/\n+$/, '');
            continue;
          }
          i++;
        }
        workflow.statuses.push(status);
        continue;
      }

      if (section === 'transitions' && indent === 2 && trimmed.startsWith('- from:')) {
        const parsed = parseTransition(lines, i, 2, 6);
        workflow.transitions.push(parsed[0]);
        i = parsed[1];
        continue;
      }

      if (section === 'systems' && indent === 2 && trimmed.endsWith(':')) {
        const systemName = trimmed.slice(0, -1);
        const overlay = { statuses: [], transitions: [] };
        i++;
        while (i < lines.length) {
          const inner = lines[i];
          const innerTrimmed = inner.trim();
          const innerIndent = countIndent(inner);
          if (!innerTrimmed || innerTrimmed.startsWith('#')) {
            i++;
            continue;
          }
          if (innerIndent <= 2) break;
          if (innerIndent === 4 && innerTrimmed === 'statuses:') {
            i++;
            while (i < lines.length) {
              const statusLine = lines[i];
              const statusTrimmed = statusLine.trim();
              const statusIndent = countIndent(statusLine);
              if (!statusTrimmed || statusTrimmed.startsWith('#')) {
                i++;
                continue;
              }
              if (statusIndent < 6) break;
              if (statusIndent === 6 && statusTrimmed.startsWith('- name:')) {
                const status = normalizeStatus({ name: unquoteYAML(statusTrimmed.slice('- name:'.length)) });
                i++;
                while (i < lines.length) {
                  const descLine = lines[i];
                  const descTrimmed = descLine.trim();
                  const descIndent = countIndent(descLine);
                  if (!descTrimmed || descTrimmed.startsWith('#')) {
                    i++;
                    continue;
                  }
                  if (descIndent <= 6) break;
                  if (descIndent === 8 && descTrimmed.startsWith('description:')) {
                    status.description = unquoteYAML(descTrimmed.slice('description:'.length));
                    i++;
                    continue;
                  }
                  if (descIndent === 8 && descTrimmed === 'prompt: |') {
                    i++;
                    const body = [];
                    while (i < lines.length) {
                      const bodyLine = lines[i];
                      const bodyTrimmed = bodyLine.trim();
                      const bodyIndent = countIndent(bodyLine);
                      if (!bodyTrimmed && bodyIndent >= 10) {
                        body.push('');
                        i++;
                        continue;
                      }
                      if (bodyIndent < 10) break;
                      body.push(bodyLine.slice(10));
                      i++;
                    }
                    status.prompt = body.join('\n').replace(/\n+$/, '');
                    continue;
                  }
                  i++;
                }
                overlay.statuses.push(status);
                continue;
              }
              break;
            }
            continue;
          }
          if (innerIndent === 4 && innerTrimmed === 'transitions:') {
            i++;
            while (i < lines.length) {
              const transitionLine = lines[i];
              const transitionTrimmed = transitionLine.trim();
              const transitionIndent = countIndent(transitionLine);
              if (!transitionTrimmed || transitionTrimmed.startsWith('#')) {
                i++;
                continue;
              }
              if (transitionIndent < 6) break;
              if (transitionIndent === 6 && transitionTrimmed.startsWith('- from:')) {
                const parsed = parseTransition(lines, i, 6, 10);
                overlay.transitions.push(parsed[0]);
                i = parsed[1];
                continue;
              }
              break;
            }
            continue;
          }
          i++;
        }
        workflow.systems[systemName] = overlay;
        continue;
      }
      i++;
    }
    return normalizeWorkflow(workflow);
  }

  function statusIndexMap(statuses) {
    const map = new Map();
    statuses.forEach((status, index) => {
      map.set(status.name, index);
    });
    return map;
  }

  function isAdjacent(statuses, from, to) {
    const index = statusIndexMap(statuses);
    return index.has(from) && index.has(to) && index.get(to) === index.get(from) + 1;
  }

  function simplifyWorkflow(workflow) {
    const normalized = cloneWorkflow(workflow);
    normalized.transitions = normalized.transitions.filter(transition => isAdjacent(normalized.statuses, transition.from, transition.to));
    Object.keys(normalized.systems).forEach(name => {
      normalized.systems[name].transitions = (normalized.systems[name].transitions || []).filter(transition => isAdjacent(normalized.statuses, transition.from, transition.to));
      normalized.systems[name].statuses = (normalized.systems[name].statuses || []).map(normalizeStatus).filter(status => status.name);
    });
    return normalized;
  }

  function transitionPairs() {
    const out = [];
    for (let i = 0; i < state.workflow.statuses.length - 1; i++) {
      out.push({
        from: state.workflow.statuses[i].name,
        to: state.workflow.statuses[i + 1].name,
        index: i,
      });
    }
    return out;
  }

  function findTransition(list, from, to) {
    return (list || []).find(transition => transition.from === from && transition.to === to) || null;
  }

  function ensureSystem(name) {
    const trimmed = String(name || '').trim();
    if (!trimmed) return null;
    if (!state.workflow.systems[trimmed]) {
      state.workflow.systems[trimmed] = { statuses: [], transitions: [] };
    }
    return state.workflow.systems[trimmed];
  }

  function ensureTransition(systemName, from, to) {
    const list = systemName ? ensureSystem(systemName).transitions : state.workflow.transitions;
    let transition = findTransition(list, from, to);
    if (!transition) {
      transition = normalizeTransition({ from, to, actions: [] });
      list.push(transition);
    }
    return transition;
  }

  function pruneEmptyTransitions() {
    state.workflow.transitions = state.workflow.transitions.filter(transition => transition.actions.length);
    Object.keys(state.workflow.systems).forEach(name => {
      state.workflow.systems[name].transitions = (state.workflow.systems[name].transitions || []).filter(transition => transition.actions.length);
    });
  }

  function defaultAction(type, transition) {
    switch (type) {
      case 'validate':
        return normalizeAction({ type, rule: VALIDATION_RULES[0].value });
      case 'append_section':
        return normalizeAction({ type, title: 'New Section', body: '- [ ] Describe what should be added here' });
      case 'inject_prompt':
        return normalizeAction({ type, prompt: `Guidance for ${transition.from} -> ${transition.to}` });
      case 'require_human_approval':
        return normalizeAction({ type, status: transition.to });
      case 'set_fields':
        return normalizeAction({ type, field: SET_FIELD_OPTIONS[0].value, value: '' });
      default:
        return normalizeAction({ type });
    }
  }

  function statusCount() {
    return state.workflow.statuses.length;
  }

  function baseTransitionCount() {
    return transitionPairs().length;
  }

  function actionCount() {
    let total = state.workflow.transitions.reduce((sum, transition) => sum + transition.actions.length, 0);
    Object.keys(state.workflow.systems).forEach(name => {
      total += (state.workflow.systems[name].transitions || []).reduce((sum, transition) => sum + transition.actions.length, 0);
    });
    return total;
  }

  function logEvent(kind, message) {
    debugEvents.unshift({ kind, message, at: new Date().toISOString() });
    debugEvents = debugEvents.slice(0, 10);
  }

  function showToast(message, isError) {
    let toast = document.getElementById('designer-toast');
    if (!toast) {
      toast = document.createElement('div');
      toast.id = 'designer-toast';
      toast.className = 'designer-toast';
      document.body.appendChild(toast);
    }
    toast.textContent = message;
    toast.className = 'designer-toast show' + (isError ? ' error' : '');
    window.clearTimeout(toastTimer);
    toastTimer = window.setTimeout(() => {
      toast.className = 'designer-toast' + (isError ? ' error' : '');
    }, 2800);
  }

  function persistState() {
    pruneEmptyTransitions();
    localStorage.setItem(STORAGE_KEY, JSON.stringify({
      version: 3,
      workflow: simplifyWorkflow(state.workflow),
      selected: state.selected,
    }));
    state.draftLoaded = true;
    updateBadges();
  }

  function loadState() {
    state.workflow = simplifyWorkflow(EMPTY_WORKFLOW);
    state.selected = statusCount() ? { kind: 'status', statusIndex: 0 } : null;
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) {
      logEvent('load', 'Loaded workflow from server data embedded in the page');
      return;
    }
    try {
      const parsed = JSON.parse(raw);
      if (parsed?.workflow) {
        state.workflow = simplifyWorkflow(parsed.workflow);
        state.selected = parsed.selected || state.selected;
        state.draftLoaded = true;
        logEvent('load', 'Loaded workflow draft from local browser storage');
      }
    } catch (error) {
      logEvent('warn', `Failed to read local draft: ${error.message}`);
    }
    sanitizeSelection();
  }

  function clearLocalDraft() {
    localStorage.removeItem(STORAGE_KEY);
    state.draftLoaded = false;
    logEvent('draft', 'Cleared local workflow draft');
    showToast('Local draft cleared', false);
    updateBadges();
  }

  function sanitizeSelection() {
    const pairs = transitionPairs();
    if (!state.selected) {
      state.selected = statusCount() ? { kind: 'status', statusIndex: 0 } : null;
      return;
    }
    if (state.selected.kind === 'status') {
      if (state.selected.statusIndex < 0 || state.selected.statusIndex >= state.workflow.statuses.length) {
        state.selected = statusCount() ? { kind: 'status', statusIndex: 0 } : null;
      }
      return;
    }
    const pair = pairs.find(item => item.from === state.selected.from && item.to === state.selected.to);
    if (!pair) {
      state.selected = statusCount() ? { kind: 'status', statusIndex: 0 } : null;
      return;
    }
    const transition = getSelectedTransition(true);
    if (!transition) {
      state.selected = { kind: 'transition', system: state.selected.system || '', from: pair.from, to: pair.to };
      return;
    }
    if (state.selected.kind === 'action') {
      if (state.selected.actionIndex < 0 || state.selected.actionIndex >= transition.actions.length) {
        state.selected = { kind: 'transition', system: state.selected.system || '', from: pair.from, to: pair.to };
      }
    }
  }

  function getSelectedTransition(allowEmpty) {
    if (!state.selected || (state.selected.kind !== 'transition' && state.selected.kind !== 'action')) return null;
    const systemName = state.selected.system || '';
    const list = systemName ? ensureSystem(systemName).transitions : state.workflow.transitions;
    const found = findTransition(list, state.selected.from, state.selected.to);
    if (found || !allowEmpty) return found;
    return normalizeTransition({ from: state.selected.from, to: state.selected.to, actions: [] });
  }

  function selectStatus(statusIndex) {
    state.selected = { kind: 'status', statusIndex };
    render();
  }

  function selectTransition(from, to, systemName) {
    state.selected = { kind: 'transition', system: systemName || '', from, to };
    render();
  }

  function selectAction(from, to, actionIndex, systemName) {
    state.selected = { kind: 'action', system: systemName || '', from, to, actionIndex };
    render();
  }

  function updateBadges() {
    const countBadge = document.getElementById('status-count-badge');
    if (countBadge) {
      countBadge.textContent = `${statusCount()} statuses / ${baseTransitionCount()} transitions / ${actionCount()} actions`;
    }
    const draftBadge = document.getElementById('draft-badge');
    if (draftBadge) {
      draftBadge.textContent = state.draftLoaded ? 'Draft autosaved locally' : 'Using server workflow';
    }
  }

  function readinessChecks() {
    const items = [];
    const names = state.workflow.statuses.map(status => status.name);
    const duplicateNames = names.filter((name, index) => name && names.indexOf(name) !== index);
    if (duplicateNames.length) {
      items.push({ level: 'error', title: 'Duplicate statuses', desc: `Status names must be unique. Duplicates: ${duplicateNames.join(', ')}` });
    }
      const missingDescriptions = state.workflow.statuses.filter(status => !status.description).map(status => status.name);
    if (missingDescriptions.length) {
      items.push({ level: 'warn', title: 'Missing descriptions', desc: `Statuses without descriptions: ${missingDescriptions.join(', ')}` });
    }
    const missingPrompts = state.workflow.statuses.filter(status => !status.prompt).map(status => status.name);
    if (missingPrompts.length) {
      items.push({ level: 'warn', title: 'Missing status prompts', desc: `Statuses without baseline guidance: ${missingPrompts.join(', ')}` });
    }
    const nonAdjacentBase = state.workflow.transitions.filter(transition => !isAdjacent(state.workflow.statuses, transition.from, transition.to));
    if (nonAdjacentBase.length) {
      items.push({ level: 'warn', title: 'Unsupported base transitions', desc: 'Only adjacent status transitions are editable in this designer. Non-adjacent base transitions will be dropped on save.' });
    }
    Object.keys(state.workflow.systems).forEach(name => {
      const overlay = state.workflow.systems[name] || { transitions: [], statuses: [] };
      if ((overlay.statuses || []).length) {
        items.push({ level: 'warn', title: `${name} overlay statuses`, desc: 'Subsystem overlay statuses are preserved in YAML but not edited visually here.' });
      }
      const invalid = (overlay.transitions || []).filter(transition => !isAdjacent(state.workflow.statuses, transition.from, transition.to));
      if (invalid.length) {
        items.push({ level: 'warn', title: `${name} overlay transitions`, desc: 'Non-adjacent subsystem transitions will be dropped on save.' });
      }
    });
    if (!items.length) {
      items.push({ level: 'good', title: 'Workflow shape looks clean', desc: 'Ordered statuses and adjacent transition actions are ready to save.' });
    }
    return items;
  }

  function renderPalette() {
    const root = document.getElementById('palette-root');
    if (!root) return;
    const canAdd = !!(state.selected && (state.selected.kind === 'transition' || state.selected.kind === 'action'));
    root.innerHTML = ACTION_TYPES.map(action => `
      <button class="palette-item" data-add-action="${escapeAttr(action.type)}" ${canAdd ? '' : 'disabled'}>
        <div class="palette-icon" style="background:${action.color}">${escapeHTML(action.title.slice(0, 1))}</div>
        <div>
          <div class="palette-item-title">${escapeHTML(action.title)}</div>
          <div class="palette-item-desc">${escapeHTML(action.desc)}</div>
        </div>
      </button>
    `).join('');
  }

  function renderSystems() {
    const root = document.getElementById('starter-root');
    if (!root) return;
    const systemNames = Object.keys(state.workflow.systems).sort();
    root.innerHTML = `
      <button class="starter-item" id="add-system-btn">
        <div class="palette-icon" style="background:#2563eb">+</div>
        <div>
          <div class="starter-item-title">Add Subsystem</div>
          <div class="starter-item-desc">Create an overlay with extra transition actions for one subsystem.</div>
        </div>
      </button>
      ${systemNames.map(name => {
        const overlay = state.workflow.systems[name] || { transitions: [] };
        const count = (overlay.transitions || []).reduce((sum, transition) => sum + transition.actions.length, 0);
        return `
          <button class="starter-item" data-select-system="${escapeAttr(name)}">
            <div class="palette-icon" style="background:#0f766e">${escapeHTML(name.slice(0, 1).toUpperCase())}</div>
            <div>
              <div class="starter-item-title">${escapeHTML(name)}</div>
              <div class="starter-item-desc">${count} overlay actions across ${(overlay.transitions || []).length} transitions</div>
            </div>
          </button>
        `;
      }).join('')}
    `;
  }

  function renderReadiness() {
    const root = document.getElementById('readiness-root');
    if (!root) return;
    const checks = readinessChecks();
    const worst = checks.some(item => item.level === 'error') ? 'error' : (checks.some(item => item.level === 'warn') ? 'warn' : 'good');
    const pillText = worst === 'error' ? 'Needs cleanup' : (worst === 'warn' ? 'Mostly ready' : 'Ready to save');
    root.innerHTML = `
      <div class="readiness-pill ${worst}">${escapeHTML(pillText)}</div>
      <div class="readiness-list">
        ${checks.map(item => `
          <div class="readiness-item ${item.level}">
            <div class="palette-item-title">${escapeHTML(item.title)}</div>
            <div class="readiness-item-desc">${escapeHTML(item.desc)}</div>
          </div>
        `).join('')}
      </div>
    `;
  }

  function renderDebug() {
    const root = document.getElementById('debug-root');
    if (!root) return;
    const serverBaseActions = state.workflow.transitions.reduce((sum, transition) => sum + transition.actions.length, 0);
    const overlayCount = Object.keys(state.workflow.systems).length;
    root.innerHTML = `
      <div class="readiness-list">
        <div class="readiness-item good">
          <div class="palette-item-title">Server workflow source</div>
          <div class="readiness-item-desc">${escapeHTML(state.serverSource)}</div>
        </div>
        <div class="readiness-item good">
          <div class="palette-item-title">Save target</div>
          <div class="readiness-item-desc">${escapeHTML(state.serverTarget)}</div>
        </div>
        <div class="readiness-item ${state.draftLoaded ? 'warn' : 'good'}">
          <div class="palette-item-title">Draft state</div>
          <div class="readiness-item-desc">${state.draftLoaded ? 'Local draft currently overrides the server-loaded workflow.' : 'Using the server-loaded workflow with no local override.'}</div>
        </div>
        <div class="readiness-item good">
          <div class="palette-item-title">Current workflow</div>
          <div class="readiness-item-desc">${statusCount()} statuses, ${baseTransitionCount()} adjacent transitions, ${serverBaseActions} base actions, ${overlayCount} subsystem overlays.</div>
        </div>
        ${debugEvents.map(item => `
          <div class="readiness-item warn">
            <div class="palette-item-title">${escapeHTML(item.kind.toUpperCase())}</div>
            <div class="readiness-item-desc">${escapeHTML(item.message)}<br>${escapeHTML(item.at)}</div>
          </div>
        `).join('')}
      </div>
    `;
  }

  function statusTransitionOptions(selected) {
    return transitionPairs().map(pair => {
      const value = `${pair.from}|${pair.to}`;
      return `<option value="${escapeAttr(value)}" ${value === selected ? 'selected' : ''}>${escapeHTML(pair.from)} -> ${escapeHTML(pair.to)}</option>`;
    }).join('');
  }

  function issueOptionLabel(issue) {
    const parts = [issue.slug];
    if (issue.status) parts.push(issue.status);
    if (issue.system) parts.push(issue.system);
    return parts.join(' | ');
  }

  function testerTransitionParts() {
    const [from, to] = String(testerState.transition || '').split('|');
    return { from: from || '', to: to || testerState.to || '' };
  }

  function candidateIssues() {
    const { from } = testerTransitionParts();
    if (!from) return ISSUE_OPTIONS.slice();
    return ISSUE_OPTIONS.filter(issue => String(issue.status || '').trim() === from);
  }

  function visibleTesterIssues() {
    const candidates = candidateIssues();
    if (!testerState.viableOnly || !testerState.viability.length) return candidates;
    const ready = new Set(testerState.viability.filter(item => item.allowed).map(item => item.slug));
    return candidates.filter(issue => ready.has(issue.slug));
  }

  function viabilitySummary(slug) {
    const found = testerState.viability.find(item => item.slug === slug);
    if (!found) return '';
    return found.allowed ? 'ready' : `blocked: ${found.reason}`;
  }

  function renderTester() {
    const root = document.getElementById('tester-root');
    if (!root) return;
    const candidates = candidateIssues();
    const visibleIssues = visibleTesterIssues();
    const issueList = visibleIssues.map(issue => {
      const summary = viabilitySummary(issue.slug);
      const label = summary ? `${issueOptionLabel(issue)} | ${summary}` : issueOptionLabel(issue);
      return `<option value="${escapeAttr(issue.slug)}">${escapeHTML(label)}</option>`;
    }).join('');
    const preview = testerState.result?.preview;
    const issue = testerState.result?.issue;
    const { from, to } = testerTransitionParts();
    root.innerHTML = `
      <div class="readiness-list">
        <div class="readiness-item good">
          <label class="inspector-label">Target Transition</label>
          <select class="inspector-select" id="tester-transition-select">
            <option value="">Choose target transition</option>
            ${statusTransitionOptions(testerState.transition)}
          </select>
          <div class="inspector-help">${from ? `${candidates.length} candidate issues currently in ${from}.` : 'Choose a transition first to filter matching issues.'}</div>
          <label class="inspector-label">Issue Slug</label>
          <input class="inspector-input" id="tester-slug-input" list="workflow-issue-options" value="${escapeAttr(testerState.slug)}" placeholder="combat/example-issue">
          <datalist id="workflow-issue-options">${issueList}</datalist>
          <div class="inspector-chip-row">
            <button class="designer-btn" id="run-viability-btn" ${!to || testerState.viabilityLoading ? 'disabled' : ''}>${testerState.viabilityLoading ? 'Checking...' : 'Check Viable Issues'}</button>
            <button class="designer-btn ${testerState.viableOnly ? 'active' : ''}" id="toggle-viable-btn" ${!testerState.viability.length ? 'disabled' : ''}>${testerState.viableOnly ? 'Showing Viable Only' : 'Show Viable Only'}</button>
          </div>
          <button class="designer-btn primary" id="run-preview-btn" ${testerState.loading ? 'disabled' : ''}>${testerState.loading ? 'Running...' : 'Run Preview'}</button>
        </div>
        ${testerState.transition ? `
          <div class="readiness-item ${testerState.viability.length ? 'good' : 'warn'}">
            <div class="palette-item-title">Candidate issues</div>
            <div class="readiness-item-desc">
              ${candidates.length ? candidates.map(issue => {
                const summary = viabilitySummary(issue.slug);
                return `${issue.slug}${summary ? ` (${summary})` : ''}`;
              }).join('<br>') : `No issues are currently in ${escapeHTML(from)}.`}
            </div>
          </div>
        ` : ''}
        ${testerState.error ? `
          <div class="readiness-item error">
            <div class="palette-item-title">Preview error</div>
            <div class="readiness-item-desc">${escapeHTML(testerState.error)}</div>
          </div>
        ` : ''}
        ${preview ? `
          <div class="readiness-item ${preview.allowed ? 'good' : 'error'}">
            <div class="palette-item-title">${escapeHTML(issue.title || issue.slug)}</div>
            <div class="readiness-item-desc">
              Current: ${escapeHTML(issue.status || '')}${issue.system ? ` | System: ${escapeHTML(issue.system)}` : ''}<br>
              Target: ${escapeHTML(preview.to)}<br>
              Result: ${preview.allowed ? 'Allowed' : 'Blocked'}
            </div>
          </div>
          ${preview.steps.map(step => `
            <div class="readiness-item ${step.outcome === 'failed' ? 'error' : (step.outcome === 'passed' ? 'good' : 'warn')}">
              <div class="palette-item-title">${escapeHTML(step.summary)}</div>
              <div class="readiness-item-desc">${escapeHTML(step.message || step.outcome)}</div>
            </div>
          `).join('')}
          <div class="readiness-item ${preview.allowed ? 'good' : 'warn'}">
            <div class="palette-item-title">Transition effects</div>
            <div class="readiness-item-desc">
              Prompts: ${(preview.result.injected_prompts || []).length}<br>
              Body changed: ${preview.result.body_changed ? 'yes' : 'no'}<br>
              Approval cleared: ${preview.result.cleared_approval ? 'yes' : 'no'}
            </div>
          </div>
        ` : `
          <div class="readiness-item warn">
            <div class="palette-item-title">Dry-run tester</div>
            <div class="readiness-item-desc">Pick a real issue and target transition to preview the same validation and transition engine used by the CLI.</div>
          </div>
        `}
      </div>
    `;
  }

  function selectionMatches(kind, from, to, systemName, actionIndex) {
    if (!state.selected || state.selected.kind !== kind) return false;
    return (state.selected.system || '') === (systemName || '')
      && state.selected.from === from
      && state.selected.to === to
      && (kind !== 'action' || state.selected.actionIndex === actionIndex);
  }

  function transitionCardHTML(pair, systemName) {
    const transition = findTransition(systemName ? ensureSystem(systemName).transitions : state.workflow.transitions, pair.from, pair.to) || normalizeTransition({ from: pair.from, to: pair.to, actions: [] });
    const selected = selectionMatches('transition', pair.from, pair.to, systemName) || selectionMatches('action', pair.from, pair.to, systemName);
    const scopeLabel = systemName || 'Base';
    return `
      <div class="transition-stack ${selected ? 'selected' : ''}" data-select-transition="${escapeAttr(pair.from)}|${escapeAttr(pair.to)}|${escapeAttr(systemName || '')}">
        <div class="transition-stack-header">
          <div>
            <div class="transition-stack-label">${escapeHTML(scopeLabel)} transition</div>
            <div class="transition-stack-title">${escapeHTML(pair.from)} -> ${escapeHTML(pair.to)}</div>
          </div>
          <button class="designer-btn transition-add-btn" data-add-action-here="${escapeAttr(pair.from)}|${escapeAttr(pair.to)}|${escapeAttr(systemName || '')}">+ Action</button>
        </div>
        <div class="transition-actions">
          ${transition.actions.length ? transition.actions.map((action, index) => {
            const meta = actionMeta(action.type);
            return `
              <button class="action-block ${selectionMatches('action', pair.from, pair.to, systemName, index) ? 'selected' : ''}" data-select-action="${escapeAttr(pair.from)}|${escapeAttr(pair.to)}|${index}|${escapeAttr(systemName || '')}">
                <span class="workflow-chip" style="background:${meta.color}">${escapeHTML(meta.title)}</span>
                <div class="action-block-title">${escapeHTML(actionSummary(action))}</div>
              </button>
            `;
          }).join('') : `<div class="transition-empty">No actions configured yet.</div>`}
        </div>
      </div>
    `;
  }

  function renderCanvas() {
    const scene = document.getElementById('canvas-scene');
    if (!scene) return;
    const empty = document.getElementById('canvas-empty');
    if (empty) empty.style.display = statusCount() ? 'none' : '';
    const hint = document.getElementById('canvas-hint');
    if (hint) hint.style.display = statusCount() ? '' : 'none';
    const edgeLayer = document.getElementById('edge-layer');
    if (edgeLayer) edgeLayer.style.display = 'none';
    const minimap = document.getElementById('minimap');
    if (minimap) minimap.style.display = 'none';

    if (!statusCount()) {
      scene.innerHTML = '';
      return;
    }

    const pairs = transitionPairs();
    const baseFlow = [];
    state.workflow.statuses.forEach((status, index) => {
      const selected = state.selected?.kind === 'status' && state.selected.statusIndex === index;
      baseFlow.push(`
        <button class="status-card-linear ${selected ? 'selected' : ''}" data-select-status="${index}">
          <div class="status-card-kicker">Status</div>
          <div class="status-card-title">${escapeHTML(status.name)}</div>
          <div class="status-card-desc">${escapeHTML(status.description || 'No description yet')}</div>
          ${status.prompt ? `<div class="status-card-prompt"><div class="status-card-prompt-label">Status Prompt</div><div class="status-card-prompt-text">${escapeHTML(status.prompt)}</div></div>` : ''}
        </button>
      `);
      if (index < pairs.length) baseFlow.push(transitionCardHTML(pairs[index], ''));
    });

    const systemsHTML = Object.keys(state.workflow.systems).sort().map(name => {
      const overlay = state.workflow.systems[name] || { transitions: [] };
      return `
        <section class="overlay-group">
          <div class="overlay-group-header">
            <div>
              <div class="transition-stack-label">Subsystem overlay</div>
              <div class="overlay-group-title">${escapeHTML(name)}</div>
            </div>
            <div class="overlay-group-actions">
              <button class="designer-btn" data-jump-system="${escapeAttr(name)}">Focus</button>
              <button class="designer-btn" data-delete-system="${escapeAttr(name)}">Delete</button>
            </div>
          </div>
          <div class="workflow-linear">
            ${state.workflow.statuses.map((status, index) => {
              const parts = [`
                <div class="status-spacer">
                  <div class="status-spacer-title">${escapeHTML(status.name)}</div>
                </div>
              `];
              if (index < pairs.length) parts.push(transitionCardHTML(pairs[index], name));
              return parts.join('');
            }).join('')}
          </div>
        </section>
      `;
    }).join('');

    scene.innerHTML = `
      <div class="workflow-canvas-flow">
        <section class="workflow-lane-section">
          <div class="overlay-group-header">
            <div>
              <div class="transition-stack-label">Base workflow</div>
              <div class="overlay-group-title">Ordered lifecycle</div>
            </div>
          </div>
          <div class="workflow-linear">${baseFlow.join('')}</div>
        </section>
        ${systemsHTML ? `<section class="workflow-overlays">${systemsHTML}</section>` : ''}
      </div>
    `;
  }

  function inspectorStatus() {
    const status = state.workflow.statuses[state.selected.statusIndex];
    if (!status) return `<div class="inspector-empty">Select a status or transition.</div>`;
    return `
      <div class="inspector-section">
        <div class="inspector-chip-row">
          <span class="inspector-mini-chip">Status</span>
          <span class="inspector-mini-chip">${escapeHTML(status.name || 'unnamed')}</span>
        </div>
        <label class="inspector-label">Status Name</label>
        <input class="inspector-input" id="status-name-input" value="${escapeAttr(status.name)}" placeholder="in progress">
        <label class="inspector-label">Description</label>
        <textarea class="inspector-textarea" id="status-description-input" placeholder="Explain what being in this status means.">${escapeHTML(status.description)}</textarea>
        <label class="inspector-label">Status Prompt</label>
        <textarea class="inspector-textarea" id="status-prompt-input" placeholder="Baseline guidance for agents working in or entering this status.">${escapeHTML(status.prompt || '')}</textarea>
        <div class="inspector-help">This guidance is shown whenever an agent starts work in this status or transitions into it. Transition inject prompts stay additive.</div>
      </div>
      <div class="inspector-section">
        <div class="inspector-row">
          <button class="designer-btn" id="move-status-left-btn" ${state.selected.statusIndex === 0 ? 'disabled' : ''}>Move Left</button>
          <button class="designer-btn" id="move-status-right-btn" ${state.selected.statusIndex === state.workflow.statuses.length - 1 ? 'disabled' : ''}>Move Right</button>
        </div>
        <div class="inspector-help">Transitions are implicit between adjacent statuses in this order.</div>
        <button class="designer-btn" id="duplicate-status-btn">Duplicate Status</button>
        <button class="designer-btn" id="delete-status-btn" ${state.workflow.statuses.length <= 1 ? 'disabled' : ''}>Delete Status</button>
      </div>
    `;
  }

  function inspectorTransition() {
    const transition = getSelectedTransition(true);
    const systemName = state.selected.system || '';
    const actionButtons = ACTION_TYPES.map(action => `<button class="designer-btn" data-add-action="${escapeAttr(action.type)}">${escapeHTML(action.title)}</button>`).join('');
    return `
      <div class="inspector-section">
        <div class="inspector-chip-row">
          <span class="inspector-mini-chip">${escapeHTML(systemName || 'Base')}</span>
          <span class="inspector-mini-chip">${escapeHTML(transition.from)} -> ${escapeHTML(transition.to)}</span>
        </div>
        <div class="inspector-help">This transition owns an ordered list of actions. Actions run top to bottom.</div>
      </div>
      <div class="inspector-section">
        <label class="inspector-label">Add Action</label>
        <div class="inspector-chip-row">${actionButtons}</div>
      </div>
      <div class="inspector-section">
        <label class="inspector-label">Action Order</label>
        <div class="readiness-list">
          ${transition.actions.length ? transition.actions.map((action, index) => `
            <button class="palette-item" data-select-action="${escapeAttr(transition.from)}|${escapeAttr(transition.to)}|${index}|${escapeAttr(systemName)}">
              <div class="palette-icon" style="background:${actionMeta(action.type).color}">${escapeHTML(actionMeta(action.type).title.slice(0, 1))}</div>
              <div>
                <div class="palette-item-title">${escapeHTML(index + 1 + '. ' + actionMeta(action.type).title)}</div>
                <div class="palette-item-desc">${escapeHTML(actionSummary(action))}</div>
              </div>
            </button>
          `).join('') : `<div class="inspector-empty">No actions yet. Add one from the buttons above.</div>`}
        </div>
      </div>
    `;
  }

  function statusOptions(selected) {
    return state.workflow.statuses.map(status => `<option value="${escapeAttr(status.name)}" ${status.name === selected ? 'selected' : ''}>${escapeHTML(status.name)}</option>`).join('');
  }

  function inspectorAction() {
    const transition = getSelectedTransition(true);
    const action = transition.actions[state.selected.actionIndex];
    if (!action) return inspectorTransition();
    const meta = actionMeta(action.type);
    let fields = '';
    if (action.type === 'validate') {
      const parsed = parseValidationRule(action.rule);
      const ruleMeta = validationRuleMeta(action.rule);
      fields = `
        <label class="inspector-label">Validation Rule</label>
        <select class="inspector-select" id="action-validation-type">
          ${VALIDATION_RULES.map(rule => `<option value="${escapeAttr(rule.value)}" ${rule.value === parsed.type ? 'selected' : ''}>${escapeHTML(rule.label)}</option>`).join('')}
        </select>
        <div class="inspector-help">${escapeHTML(ruleMeta?.description || 'Choose a built-in validation rule.')}</div>
        ${(ruleMeta && ruleMeta.argLabel) ? `
          <label class="inspector-label">${escapeHTML(ruleMeta.argLabel)}</label>
          <input class="inspector-input" id="action-validation-arg" value="${escapeAttr(parsed.arg)}" placeholder="${escapeAttr(ruleMeta.argPlaceholder || '')}">
        ` : ''}
      `;
    } else if (action.type === 'append_section') {
      fields = `
        <label class="inspector-label">Section Title</label>
        <input class="inspector-input" id="action-title-input" value="${escapeAttr(action.title)}" placeholder="Implementation">
        <div class="inspector-help">The section will be created if missing, or reused if it already exists.</div>
        <label class="inspector-label">Section Body</label>
        <textarea class="inspector-textarea" id="action-body-input" placeholder="- [ ] What should be added">${escapeHTML(action.body)}</textarea>
      `;
    } else if (action.type === 'inject_prompt') {
      fields = `
        <label class="inspector-label">Injected Prompt</label>
        <textarea class="inspector-textarea" id="action-prompt-input" placeholder="Explain what the next agent should pay attention to.">${escapeHTML(action.prompt)}</textarea>
      `;
    } else if (action.type === 'require_human_approval') {
      fields = `
        <label class="inspector-label">Approved Status</label>
        <select class="inspector-select" id="action-approval-status">
          ${statusOptions(action.status)}
        </select>
        <div class="inspector-help">This reuses the existing issue approval metadata instead of creating a separate approval state.</div>
      `;
    } else if (action.type === 'set_fields') {
      const fieldMeta = setFieldMeta(action.field);
      fields = `
        <label class="inspector-label">Allowed Field</label>
        <select class="inspector-select" id="action-field-select">
          ${SET_FIELD_OPTIONS.map(option => `<option value="${escapeAttr(option.value)}" ${option.value === action.field ? 'selected' : ''}>${escapeHTML(option.label)}</option>`).join('')}
        </select>
        <div class="inspector-help">${escapeHTML(fieldMeta?.description || 'Select one of the supported frontmatter fields.')}</div>
        <label class="inspector-label">Value</label>
        <input class="inspector-input" id="action-field-value" value="${escapeAttr(action.value)}" placeholder="Leave empty to clear the field">
      `;
    }
    return `
      <div class="inspector-section">
        <div class="inspector-chip-row">
          <span class="inspector-mini-chip">${escapeHTML(state.selected.system || 'Base')}</span>
          <span class="inspector-mini-chip">${escapeHTML(transition.from)} -> ${escapeHTML(transition.to)}</span>
          <span class="inspector-mini-chip">${escapeHTML(meta.title)}</span>
        </div>
        <div class="inspector-help">Action ${state.selected.actionIndex + 1} of ${transition.actions.length}. Actions run in this exact order.</div>
        ${fields}
      </div>
      <div class="inspector-section">
        <div class="inspector-row">
          <button class="designer-btn" id="move-action-up-btn" ${state.selected.actionIndex === 0 ? 'disabled' : ''}>Move Up</button>
          <button class="designer-btn" id="move-action-down-btn" ${state.selected.actionIndex === transition.actions.length - 1 ? 'disabled' : ''}>Move Down</button>
        </div>
        <button class="designer-btn" id="delete-action-btn">Delete Action</button>
      </div>
    `;
  }

  function renderInspector() {
    const root = document.getElementById('inspector-root');
    if (!root) return;
    if (!state.selected) {
      root.innerHTML = `<div class="inspector-empty">Add a status to start building the workflow.</div>`;
      return;
    }
    if (state.selected.kind === 'status') {
      root.innerHTML = inspectorStatus();
      return;
    }
    if (state.selected.kind === 'transition') {
      root.innerHTML = inspectorTransition();
      return;
    }
    root.innerHTML = inspectorAction();
  }

  function render() {
    sanitizeSelection();
    updateBadges();
    renderPalette();
    renderSystems();
    renderReadiness();
    renderTester();
    renderDebug();
    renderCanvas();
    renderInspector();
  }

  function currentTransitionTarget() {
    if (!state.selected) return null;
    if (state.selected.kind === 'transition' || state.selected.kind === 'action') {
      return { from: state.selected.from, to: state.selected.to, system: state.selected.system || '' };
    }
    return null;
  }

  function addAction(type, target) {
    const context = target || currentTransitionTarget();
    if (!context) {
      showToast('Select a transition first', true);
      return;
    }
    const transition = ensureTransition(context.system, context.from, context.to);
    transition.actions.push(defaultAction(type, transition));
    state.selected = {
      kind: 'action',
      system: context.system || '',
      from: context.from,
      to: context.to,
      actionIndex: transition.actions.length - 1,
    };
    logEvent('edit', `Added ${type} to ${context.system || 'base'} ${context.from} -> ${context.to}`);
    persistState();
    render();
  }

  function addStatus() {
    const nextIndex = state.workflow.statuses.length + 1;
    state.workflow.statuses.push(normalizeStatus({ name: `new-status-${nextIndex}`, description: 'Describe this status', prompt: 'Add baseline guidance for this status.' }));
    state.selected = { kind: 'status', statusIndex: state.workflow.statuses.length - 1 };
    logEvent('edit', `Added status new-status-${nextIndex}`);
    persistState();
    render();
  }

  function duplicateStatus() {
    if (!state.selected || state.selected.kind !== 'status') return;
    const current = state.workflow.statuses[state.selected.statusIndex];
    const copy = normalizeStatus({
      name: `${current.name}-copy`,
      description: current.description,
      prompt: current.prompt,
    });
    state.workflow.statuses.splice(state.selected.statusIndex + 1, 0, copy);
    state.selected = { kind: 'status', statusIndex: state.selected.statusIndex + 1 };
    logEvent('edit', `Duplicated status ${current.name}`);
    persistState();
    render();
  }

  function moveStatus(direction) {
    if (!state.selected || state.selected.kind !== 'status') return;
    const from = state.selected.statusIndex;
    const to = from + direction;
    if (to < 0 || to >= state.workflow.statuses.length) return;
    const [item] = state.workflow.statuses.splice(from, 1);
    state.workflow.statuses.splice(to, 0, item);
    state.selected = { kind: 'status', statusIndex: to };
    logEvent('edit', `Moved status ${item.name}`);
    persistState();
    render();
  }

  function deleteStatus() {
    if (!state.selected || state.selected.kind !== 'status' || state.workflow.statuses.length <= 1) return;
    const removed = state.workflow.statuses.splice(state.selected.statusIndex, 1)[0];
    state.workflow.transitions = state.workflow.transitions.filter(transition => transition.from !== removed.name && transition.to !== removed.name);
    Object.keys(state.workflow.systems).forEach(name => {
      state.workflow.systems[name].transitions = state.workflow.systems[name].transitions.filter(transition => transition.from !== removed.name && transition.to !== removed.name);
    });
    state.selected = { kind: 'status', statusIndex: Math.max(0, state.selected.statusIndex - 1) };
    logEvent('edit', `Deleted status ${removed.name}`);
    persistState();
    render();
  }

  function moveAction(direction) {
    if (!state.selected || state.selected.kind !== 'action') return;
    const transition = getSelectedTransition(false);
    if (!transition) return;
    const from = state.selected.actionIndex;
    const to = from + direction;
    if (to < 0 || to >= transition.actions.length) return;
    const [item] = transition.actions.splice(from, 1);
    transition.actions.splice(to, 0, item);
    state.selected.actionIndex = to;
    logEvent('edit', `Moved action in ${state.selected.system || 'base'} ${state.selected.from} -> ${state.selected.to}`);
    persistState();
    render();
  }

  function deleteAction() {
    if (!state.selected || state.selected.kind !== 'action') return;
    const transition = getSelectedTransition(false);
    if (!transition) return;
    transition.actions.splice(state.selected.actionIndex, 1);
    if (transition.actions.length) {
      state.selected.actionIndex = Math.max(0, state.selected.actionIndex - 1);
    } else {
      state.selected = { kind: 'transition', system: state.selected.system || '', from: state.selected.from, to: state.selected.to };
    }
    pruneEmptyTransitions();
    logEvent('edit', `Deleted action from ${state.selected.system || 'base'} ${state.selected.from} -> ${state.selected.to}`);
    persistState();
    render();
  }

  function addSystem() {
    const name = window.prompt('Subsystem name');
    if (!name) return;
    const trimmed = name.trim();
    if (!trimmed) return;
    if (state.workflow.systems[trimmed]) {
      showToast(`Subsystem ${trimmed} already exists`, true);
      return;
    }
    state.workflow.systems[trimmed] = { statuses: [], transitions: [] };
    const pair = transitionPairs()[0];
    if (pair) state.selected = { kind: 'transition', system: trimmed, from: pair.from, to: pair.to };
    logEvent('edit', `Added subsystem overlay ${trimmed}`);
    persistState();
    render();
  }

  function deleteSystem(name) {
    if (!state.workflow.systems[name]) return;
    delete state.workflow.systems[name];
    if (state.selected?.system === name) {
      state.selected = statusCount() ? { kind: 'status', statusIndex: 0 } : null;
    }
    logEvent('edit', `Deleted subsystem overlay ${name}`);
    persistState();
    render();
  }

  function focusTransition(systemName) {
    const pair = transitionPairs().find(item => findTransition(systemName ? state.workflow.systems[systemName].transitions : state.workflow.transitions, item.from, item.to));
    if (pair) {
      selectTransition(pair.from, pair.to, systemName);
      requestAnimationFrame(focusSelection);
    }
  }

  function focusSelection() {
    const element = document.querySelector('.status-card-linear.selected, .transition-stack.selected, .action-block.selected');
    element?.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'center' });
  }

  async function reloadFromServer() {
    try {
      const response = await fetch(DATA_URL, { headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error(`Server returned ${response.status}`);
      const payload = await response.json();
      state.workflow = simplifyWorkflow(payload.workflow || EMPTY_WORKFLOW);
      state.serverSource = payload.source || state.serverSource;
      state.serverTarget = payload.target || state.serverTarget;
      state.selected = statusCount() ? { kind: 'status', statusIndex: 0 } : null;
      localStorage.removeItem(STORAGE_KEY);
      state.draftLoaded = false;
      testerState = freshTesterState();
      exportState = { title: '', text: '', filename: '', mode: 'export' };
      debugEvents = [];
      logEvent('load', `Reloaded workflow from server file source: ${state.serverSource}`);
      showToast('Reloaded project workflow from server', false);
      render();
    } catch (error) {
      logEvent('error', `Server reload failed: ${error.message}`);
      showToast(error.message, true);
    }
  }

  async function saveWorkflow() {
    const yaml = workflowToYAML(state.workflow);
    try {
      const response = await fetch(SAVE_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ yaml }),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(payload.error || `Save failed with ${response.status}`);
      state.serverTarget = payload.path || state.serverTarget;
      logEvent('save', `Saved workflow to ${payload.path || 'workflow.yaml'}`);
      showToast(`Workflow saved to ${payload.path || 'workflow.yaml'}`, false);
      renderDebug();
      updateBadges();
    } catch (error) {
      logEvent('error', `Save failed: ${error.message}`);
      showToast(error.message, true);
    }
  }

  async function runPreview() {
    const slug = document.getElementById('tester-slug-input')?.value.trim() || testerState.slug;
    const transitionValue = document.getElementById('tester-transition-select')?.value || '';
    const to = transitionValue.split('|')[1] || testerState.to;
    testerState.slug = slug;
    testerState.to = to;
    testerState.transition = transitionValue;
    testerState.error = '';
    testerState.result = null;
    if (!slug || !to) {
      testerState.error = 'Pick both an issue slug and target transition.';
      renderTester();
      return;
    }
    testerState.loading = true;
    renderTester();
    try {
      const response = await fetch(PREVIEW_URL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ slug, to }),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(payload.error || `Preview failed with ${response.status}`);
      testerState.result = payload;
      logEvent('preview', `Previewed ${slug} -> ${to}`);
    } catch (error) {
      testerState.error = error.message;
      logEvent('error', `Preview failed: ${error.message}`);
    } finally {
      testerState.loading = false;
      renderTester();
      renderDebug();
    }
  }

  async function runViabilityCheck() {
    const transitionValue = document.getElementById('tester-transition-select')?.value || testerState.transition;
    const parts = transitionValue.split('|');
    const to = parts[1] || testerState.to;
    testerState.transition = transitionValue;
    testerState.to = to;
    testerState.error = '';
    testerState.viability = [];
    const candidates = candidateIssues();
    if (!to) {
      testerState.error = 'Pick a target transition first.';
      renderTester();
      return;
    }
    if (!candidates.length) {
      testerState.error = `No candidate issues found for ${parts[0] || 'this transition'}.`;
      renderTester();
      return;
    }
    testerState.viabilityLoading = true;
    renderTester();
    try {
      const results = await Promise.all(candidates.map(async issue => {
        const response = await fetch(PREVIEW_URL, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ slug: issue.slug, to }),
        });
        const payload = await response.json().catch(() => ({}));
        if (!response.ok) {
          return { slug: issue.slug, allowed: false, reason: payload.error || `preview failed with ${response.status}` };
        }
        const preview = payload.preview || {};
        return {
          slug: issue.slug,
          allowed: !!preview.allowed,
          reason: preview.allowed ? '' : (preview.validation_error || 'blocked'),
        };
      }));
      testerState.viability = results;
      logEvent('preview', `Checked viability for ${results.length} candidate issues toward ${to}`);
      const firstReady = results.find(item => item.allowed);
      if (testerState.viableOnly && firstReady && (!testerState.slug || !results.some(item => item.slug === testerState.slug && item.allowed))) {
        testerState.slug = firstReady.slug;
      }
    } catch (error) {
      testerState.error = error.message;
      logEvent('error', `Viability check failed: ${error.message}`);
    } finally {
      testerState.viabilityLoading = false;
      renderTester();
      renderDebug();
    }
  }

  function importWorkflowText(text) {
    const trimmed = text.trim();
    if (!trimmed) throw new Error('Import text is empty');
    let parsed;
    if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
      parsed = JSON.parse(trimmed);
    } else {
      parsed = parseYAMLWorkflow(trimmed);
    }
    const workflow = simplifyWorkflow(parsed.workflow || parsed);
    if (!workflow.statuses.length) throw new Error('Imported workflow did not contain any statuses');
    state.workflow = workflow;
    state.selected = { kind: 'status', statusIndex: 0 };
    state.draftLoaded = true;
    persistState();
    logEvent('import', `Imported workflow with ${workflow.statuses.length} statuses`);
    showToast(`Imported workflow: ${workflow.statuses.length} statuses`, false);
    render();
  }

  function showModal(title, text, filename, mode) {
    exportState = { title, text, filename, mode };
    document.getElementById('modal-title').textContent = title;
    document.getElementById('modal-text').value = text;
    document.getElementById('export-modal').classList.add('open');
    document.getElementById('pick-file-btn').style.display = mode === 'import' ? '' : 'none';
    document.getElementById('download-modal-btn').style.display = mode === 'export' ? '' : 'none';
    document.getElementById('copy-modal-btn').textContent = mode === 'import' ? 'Apply Import' : 'Copy';
  }

  function closeModal() {
    document.getElementById('export-modal').classList.remove('open');
  }

  function applyInspectorChanges() {
    if (!state.selected) return;
    if (state.selected.kind === 'status') {
      const status = state.workflow.statuses[state.selected.statusIndex];
      const oldName = status.name;
      const nextName = document.getElementById('status-name-input')?.value.trim() || status.name;
      status.name = nextName;
      status.description = document.getElementById('status-description-input')?.value.trim() || '';
      status.prompt = document.getElementById('status-prompt-input')?.value.replace(/\r/g, '').trim() || '';
      if (nextName !== oldName) {
        state.workflow.transitions.forEach(transition => {
          if (transition.from === oldName) transition.from = nextName;
          if (transition.to === oldName) transition.to = nextName;
        });
        Object.keys(state.workflow.systems).forEach(name => {
          state.workflow.systems[name].transitions.forEach(transition => {
            if (transition.from === oldName) transition.from = nextName;
            if (transition.to === oldName) transition.to = nextName;
          });
        });
      }
    }
    if (state.selected.kind === 'action') {
      const transition = getSelectedTransition(false);
      if (!transition) return;
      const action = transition.actions[state.selected.actionIndex];
      if (!action) return;
      if (action.type === 'validate') {
        const type = document.getElementById('action-validation-type')?.value || '';
        const arg = document.getElementById('action-validation-arg')?.value || '';
        action.rule = composeValidationRule(type, arg);
      }
      if (action.type === 'append_section') {
        action.title = document.getElementById('action-title-input')?.value.trim() || '';
        action.body = document.getElementById('action-body-input')?.value.replace(/\r/g, '').trim();
      }
      if (action.type === 'inject_prompt') {
        action.prompt = document.getElementById('action-prompt-input')?.value.replace(/\r/g, '').trim();
      }
      if (action.type === 'require_human_approval') {
        action.status = document.getElementById('action-approval-status')?.value || '';
      }
      if (action.type === 'set_fields') {
        action.field = document.getElementById('action-field-select')?.value || '';
        action.value = document.getElementById('action-field-value')?.value ?? '';
      }
    }
    persistState();
    render();
  }

  function bindEvents() {
    document.getElementById('save-workflow-btn').addEventListener('click', saveWorkflow);
    document.getElementById('reset-btn').addEventListener('click', reloadFromServer);
    document.getElementById('import-btn').addEventListener('click', () => showModal('Import Workflow', INITIAL_YAML || workflowToYAML(state.workflow), 'workflow.yaml', 'import'));
    document.getElementById('export-json-btn').addEventListener('click', () => showModal('Export Workflow JSON', JSON.stringify(simplifyWorkflow(state.workflow), null, 2), 'workflow.json', 'export'));
    document.getElementById('export-yaml-btn').addEventListener('click', () => showModal('Export Workflow YAML', workflowToYAML(state.workflow), 'workflow.yaml', 'export'));
    document.getElementById('add-status-btn').addEventListener('click', addStatus);
    document.getElementById('auto-layout-btn').addEventListener('click', () => {
      document.getElementById('canvas-wrap').scrollTo({ left: 0, top: 0, behavior: 'smooth' });
      showToast('View reset to the start of the workflow', false);
    });
    document.getElementById('focus-selection-btn').addEventListener('click', focusSelection);
    document.getElementById('clear-local-btn').addEventListener('click', () => {
      clearLocalDraft();
      render();
    });
    document.getElementById('close-modal-btn').addEventListener('click', closeModal);
    document.getElementById('pick-file-btn').addEventListener('click', () => document.getElementById('workflow-file-input').click());
    document.getElementById('workflow-file-input').addEventListener('change', async event => {
      const file = event.target.files?.[0];
      if (!file) return;
      const text = await file.text();
      document.getElementById('modal-text').value = text;
    });
    document.getElementById('copy-modal-btn').addEventListener('click', async () => {
      const text = document.getElementById('modal-text').value;
      if (exportState.mode === 'import') {
        try {
          importWorkflowText(text);
          closeModal();
        } catch (error) {
          logEvent('error', `Import failed: ${error.message}`);
          showToast(error.message, true);
        }
        return;
      }
      await navigator.clipboard.writeText(text);
      showToast('Copied to clipboard', false);
    });
    document.getElementById('download-modal-btn').addEventListener('click', () => {
      const blob = new Blob([document.getElementById('modal-text').value], { type: 'text/plain;charset=utf-8' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = exportState.filename;
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(url);
    });

    document.getElementById('palette-root').addEventListener('click', event => {
      const button = event.target.closest('[data-add-action]');
      if (!button) return;
      addAction(button.getAttribute('data-add-action'));
    });

    document.getElementById('starter-root').addEventListener('click', event => {
      if (event.target.closest('#add-system-btn')) {
        addSystem();
        return;
      }
      const systemButton = event.target.closest('[data-select-system]');
      if (systemButton) {
        focusTransition(systemButton.getAttribute('data-select-system'));
      }
    });

    document.getElementById('tester-root').addEventListener('click', event => {
      if (event.target.closest('#run-preview-btn')) {
        runPreview();
        return;
      }
      if (event.target.closest('#run-viability-btn')) {
        runViabilityCheck();
        return;
      }
      if (event.target.closest('#toggle-viable-btn')) {
        testerState.viableOnly = !testerState.viableOnly;
        const visible = visibleTesterIssues();
        if (testerState.viableOnly && visible.length && !visible.some(issue => issue.slug === testerState.slug)) {
          testerState.slug = visible[0].slug;
        }
        renderTester();
      }
    });

    document.getElementById('tester-root').addEventListener('change', event => {
      if (event.target.id === 'tester-slug-input') {
        testerState.slug = event.target.value.trim();
      }
      if (event.target.id === 'tester-transition-select') {
        testerState.transition = event.target.value;
        testerState.to = (event.target.value.split('|')[1] || '').trim();
        testerState.viability = [];
        testerState.viableOnly = false;
        const visible = visibleTesterIssues();
        if (visible.length && !visible.some(issue => issue.slug === testerState.slug)) {
          testerState.slug = visible[0].slug;
        }
        renderTester();
      }
    });

    document.getElementById('canvas-scene').addEventListener('click', event => {
      const statusButton = event.target.closest('[data-select-status]');
      if (statusButton) {
        selectStatus(Number(statusButton.getAttribute('data-select-status')));
        return;
      }
      const quickAdd = event.target.closest('[data-add-action-here]');
      if (quickAdd) {
        const [from, to, systemName] = quickAdd.getAttribute('data-add-action-here').split('|');
        selectTransition(from, to, systemName);
        addAction('validate', { from, to, system: systemName });
        return;
      }
      const actionButton = event.target.closest('[data-select-action]');
      if (actionButton) {
        const [from, to, actionIndex, systemName] = actionButton.getAttribute('data-select-action').split('|');
        selectAction(from, to, Number(actionIndex), systemName);
        return;
      }
      const transitionButton = event.target.closest('[data-select-transition]');
      if (transitionButton) {
        const [from, to, systemName] = transitionButton.getAttribute('data-select-transition').split('|');
        selectTransition(from, to, systemName);
        return;
      }
      const deleteSystemButton = event.target.closest('[data-delete-system]');
      if (deleteSystemButton) {
        deleteSystem(deleteSystemButton.getAttribute('data-delete-system'));
        return;
      }
      const jumpButton = event.target.closest('[data-jump-system]');
      if (jumpButton) {
        focusTransition(jumpButton.getAttribute('data-jump-system'));
      }
    });

    document.getElementById('inspector-root').addEventListener('click', event => {
      if (event.target.closest('#move-status-left-btn')) return moveStatus(-1);
      if (event.target.closest('#move-status-right-btn')) return moveStatus(1);
      if (event.target.closest('#duplicate-status-btn')) return duplicateStatus();
      if (event.target.closest('#delete-status-btn')) return deleteStatus();
      if (event.target.closest('#move-action-up-btn')) return moveAction(-1);
      if (event.target.closest('#move-action-down-btn')) return moveAction(1);
      if (event.target.closest('#delete-action-btn')) return deleteAction();
      const actionAdd = event.target.closest('[data-add-action]');
      if (actionAdd) return addAction(actionAdd.getAttribute('data-add-action'));
      const actionPick = event.target.closest('[data-select-action]');
      if (actionPick) {
        const [from, to, actionIndex, systemName] = actionPick.getAttribute('data-select-action').split('|');
        selectAction(from, to, Number(actionIndex), systemName);
      }
    });

    document.getElementById('inspector-root').addEventListener('change', applyInspectorChanges);
  }

  loadState();
  bindEvents();
  render();
})();
