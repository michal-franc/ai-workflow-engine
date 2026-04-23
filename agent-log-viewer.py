#!/usr/bin/env python3
"""Parse Claude Code agent logs (rawlog + clilog) and produce an HTML visualization."""

import argparse
import html
import json
import os
import re
import sys
from datetime import datetime
from pathlib import Path

# ANSI escape sequence pattern
ANSI_RE = re.compile(r'''
    \x1b          # ESC
    (?:
        \[[0-9;]*[a-zA-Z]        # CSI sequences  \e[...m
      | \[\?[0-9;]*[a-zA-Z]      # private CSI    \e[?...h
      | \][0-9];[^\x07]*\x07     # OSC sequences  \e]...BEL
      | \[>[0-9;]*[a-zA-Z]       # private CSI >  \e[>...
      | \[>[0-9]*q               # XTVERSION
      | \[>[0-9;]*[a-zA-Z]       # other private
      | \(B                      # charset
      | c                        # RIS
    )
''', re.VERBOSE)

SPINNER_CHARS = set('✶✻✽✢·*')
SPINNER_RE = re.compile(r'^[✶✻✽✢·\*\s]+$')
THINKING_RE = re.compile(r'^\(thinking\)$')
CARAMELIZING_RE = re.compile(r'^.*(?:Caramelizing|Clauding).*$')
STATUS_BAR_RE = re.compile(r'^\[.*(?:Opus|Sonnet|Haiku|Claude).*\]')
UI_CHROME_RE = re.compile(r'^(?:──+|⏵⏵|~\(°°\)~|❯|$)')
SEPARATOR_RE = re.compile(r'^─{10,}$')


def strip_ansi(text: str) -> str:
    # Replace cursor-forward sequences (\e[NC) with spaces
    text = re.sub(r'\x1b\[(\d+)C', lambda m: ' ' * int(m.group(1)), text)
    # Replace single cursor-forward (\e[C) with one space
    text = re.sub(r'\x1b\[C', ' ', text)
    text = ANSI_RE.sub('', text)
    text = text.replace('\r', '')
    return text


def parse_clilog(path: str) -> list[dict]:
    """Parse JSON-lines clilog into structured events."""
    events = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
                events.append(obj)
            except json.JSONDecodeError:
                continue
    return events


def is_noise(line: str) -> bool:
    """Return True if line is spinner/UI noise."""
    stripped = line.strip()
    if not stripped:
        return True
    if SPINNER_RE.match(stripped):
        return True
    if THINKING_RE.match(stripped):
        return True
    if CARAMELIZING_RE.match(stripped):
        return True
    if STATUS_BAR_RE.match(stripped):
        return True
    if UI_CHROME_RE.match(stripped):
        return True
    if SEPARATOR_RE.match(stripped):
        return True
    # Pure spinner char sequences with thinking
    if re.match(r'^[✶✻✽✢·\*\s]*(thinking|Caramelizing|Clauding)[✶✻✽✢·\*\s]*', stripped):
        return True
    # Status line fragments
    if re.match(r'^(accept|edits|on|shift\+tab|to|cycle)\s*$', stripped):
        return True
    if re.match(r'^Snark\s*$', stripped):
        return True
    if stripped in ('k', '=', '%', '>', 'k\\', '\\'):
        return True
    # Token counter fragments
    if re.match(r'^\d+$', stripped):
        return True
    if re.match(r'^[A-Z]:\d', stripped):
        return True
    # ctrl+o hints
    if 'ctrl+o to expand' in stripped and len(stripped) < 40:
        return True
    if 'ctrl+b ctrl+b' in stripped:
        return True
    # Fragments from thinking timer
    if re.match(r'^\d+s\s*$', stripped):
        return True
    if re.match(r'^↓\s*\d', stripped):
        return True
    return False


def extract_user_prompt(lines: list[str]) -> tuple[str, int]:
    """Extract the initial user prompt from rawlog lines."""
    prompt_lines = []
    in_prompt = False
    end_idx = 0

    for i, line in enumerate(lines):
        stripped = line.strip()
        if '[Pastedtext' in stripped or 'Pasted text' in stripped:
            in_prompt = True
            continue
        if in_prompt:
            if 'Caramelizing' in stripped or 'Clauding' in stripped:
                end_idx = i
                break
            if not is_noise(stripped):
                prompt_lines.append(stripped)

    return '\n'.join(prompt_lines), end_idx


def unsquish(text: str) -> str:
    """Re-insert spaces in text that the TUI rendering squished together."""
    # camelCase boundaries: lowercase followed by uppercase
    text = re.sub(r'([a-z])([A-Z])', r'\1 \2', text)
    # Period/comma followed by letter (no space)
    text = re.sub(r'([.,:;])([A-Za-z])', r'\1 \2', text)
    # Em-dash boundaries
    text = re.sub(r'(\w)(—)', r'\1 \2', text)
    text = re.sub(r'(—)(\w)', r'\1 \2', text)
    # Closing paren/quote followed by word
    text = re.sub(r'([)\]"\'`])([A-Za-z])', r'\1 \2', text)
    # Word followed by opening paren/quote (but not tool call patterns)
    text = re.sub(r'([a-z])(\()', r'\1 \2', text)
    # "the" "to" "a" "in" "of" "is" stuck to next word
    text = re.sub(r'\b(the|to|a|in|of|is|and|but|or|for|at|by|on|if|it|its|not|now|let|me|how|my|this|that|with|from|has|have|was|were|will|can|than|then|also|just|into|each|all|new|old|are|be|no|so|do|up|an)\b(?=[A-Z])', r'\1 ', text)
    return text


def parse_rawlog(path: str) -> dict:
    """Parse rawlog into structured conversation events."""
    with open(path) as f:
        raw = f.read()

    clean = strip_ansi(raw)
    lines = clean.split('\n')

    # Extract user prompt
    prompt, prompt_end = extract_user_prompt(lines)

    # Now extract events from the rest using ● markers
    # The rawlog contains duplicates due to TUI redraws, so we deduplicate
    events = []
    seen_events = set()

    # Find all lines starting with ●
    bullet_lines = []
    for i, line in enumerate(lines[prompt_end:], start=prompt_end):
        stripped = line.strip()
        if stripped.startswith('●') and len(stripped) > 1:
            content = stripped[1:].strip()
            if content and not SPINNER_RE.match(content) and content not in ('', '(thinking)'):
                bullet_lines.append((i, content))

    # Also look for ⎿ result markers
    result_lines = []
    for i, line in enumerate(lines[prompt_end:], start=prompt_end):
        stripped = line.strip()
        if stripped.startswith('⎿') and len(stripped) > 3:
            content = stripped[1:].strip()
            if content and 'Running…' not in content and 'Waiting…' not in content and 'Initializing…' not in content:
                result_lines.append((i, content))

    # Classify bullet_lines into tool calls vs text output
    tool_call_re = re.compile(r'^(Bash|Read|Edit|Write|Grep|Glob|Search|Explore|Update|Agent)\s*\(')
    reading_re = re.compile(r'^Reading \d+ file|^Searching for|^Searched for')

    for idx, content in bullet_lines:
        # Skip very short fragments that are noise
        if len(content) < 3:
            continue
        # Skip noise that slipped through
        if re.match(r'^\d+s.*ctrl\+o', content):
            continue
        if 'ctrl+o to expand' in content and len(content) < 50:
            continue

        # Deduplicate — normalize by stripping whitespace and comparing
        dedup_key = re.sub(r'\s+', '', content[:120])
        is_dup = dedup_key in seen_events
        if not is_dup:
            for existing in list(seen_events):
                # Two keys share a long enough prefix — consider duplicate
                min_len = min(len(existing), len(dedup_key))
                common = min_len
                for ci in range(min_len):
                    if existing[ci] != dedup_key[ci]:
                        common = ci
                        break
                if common >= 40:
                    is_dup = True
                    # Keep the longer one
                    if len(dedup_key) > len(existing):
                        seen_events.discard(existing)
                        seen_events.add(dedup_key)
                    break
        if is_dup:
            continue
        seen_events.add(dedup_key)

        if tool_call_re.match(content):
            # Extract tool name and args
            m = re.match(r'^(\w+)\s*\((.+?)(?:\)\s*|$)', content)
            if m:
                raw_args = m.group(2).strip().rstrip(')')
                # Re-insert spaces in squished tool args
                raw_args = unsquish(raw_args)
                events.append({
                    'type': 'tool_call',
                    'tool': m.group(1),
                    'args': raw_args,
                    'raw': content,
                    'line': idx,
                })
            else:
                events.append({
                    'type': 'tool_call',
                    'tool': content.split('(')[0],
                    'args': '',
                    'raw': content,
                    'line': idx,
                })
        elif reading_re.match(content):
            events.append({
                'type': 'tool_info',
                'raw': content,
                'line': idx,
            })
        else:
            # Text output from assistant
            text = unsquish(content)
            events.append({
                'type': 'text',
                'content': text,
                'line': idx,
            })

    # Extract token stats from status bar
    stats_re = re.compile(r'In:([0-9.k]+)\s*Out:([0-9.k]+)\s*\[(\d+)%\]')
    stats = []
    for line in lines:
        m = stats_re.search(line)
        if m:
            stats.append({
                'in': m.group(1),
                'out': m.group(2),
                'pct': m.group(3),
            })

    final_stats = stats[-1] if stats else None

    return {
        'prompt': prompt,
        'events': events,
        'stats': final_stats,
    }


def format_clilog_command(event: dict) -> str:
    """Format a clilog event as a readable command."""
    args = event.get('args', [])
    return 'issue-cli ' + ' '.join(args)


def generate_html(rawlog_data: dict, clilog_events: list[dict], log_name: str) -> str:
    """Generate HTML visualization."""

    prompt_html = html.escape(rawlog_data['prompt'])
    stats = rawlog_data['stats']

    # Build timeline from clilog
    cli_timeline_html = ''
    if clilog_events:
        cli_timeline_html = '<div class="section"><h2>Issue CLI Timeline</h2><div class="timeline">'
        for ev in clilog_events:
            ts = ev.get('ts', '')
            if ts:
                try:
                    dt = datetime.fromisoformat(ts.replace('Z', '+00:00'))
                    ts_display = dt.strftime('%H:%M:%S')
                except:
                    ts_display = ts
            else:
                ts_display = ''

            cmd = format_clilog_command(ev)
            cmd_html = html.escape(cmd)

            # Color-code by command type
            args = ev.get('args', [])
            cmd_type = args[0] if args else ''
            css_class = f'cli-{cmd_type}' if cmd_type else 'cli-other'

            cli_timeline_html += f'''
                <div class="timeline-item {css_class}">
                    <span class="timestamp">{ts_display}</span>
                    <span class="cli-cmd">{cmd_html}</span>
                </div>'''
        cli_timeline_html += '</div></div>'

    # Build conversation flow
    conv_html = ''
    for ev in rawlog_data['events']:
        if ev['type'] == 'tool_call':
            tool = html.escape(ev.get('tool', ''))
            args = html.escape(ev.get('args', ''))
            conv_html += f'''
                <div class="event tool-call">
                    <span class="tool-badge">{tool}</span>
                    <span class="tool-args">{args}</span>
                </div>'''
        elif ev['type'] == 'tool_info':
            conv_html += f'''
                <div class="event tool-info">{html.escape(ev['raw'])}</div>'''
        elif ev['type'] == 'text':
            conv_html += f'''
                <div class="event assistant-text">{html.escape(ev['content'])}</div>'''

    # Stats section
    stats_html = ''
    if stats:
        stats_html = f'''
            <div class="stats-bar">
                <span class="stat">Input: <strong>{stats["in"]}</strong> tokens</span>
                <span class="stat">Output: <strong>{stats["out"]}</strong> tokens</span>
                <span class="stat">Context: <strong>{stats["pct"]}%</strong></span>
            </div>'''

    title = html.escape(log_name.replace('agent-', '').replace('-', ' ').title())

    return f'''<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Agent Log: {title}</title>
<style>
:root {{
    --bg: #0d1117;
    --surface: #161b22;
    --border: #30363d;
    --text: #e6edf3;
    --text-muted: #8b949e;
    --accent: #58a6ff;
    --green: #3fb950;
    --yellow: #d29922;
    --red: #f85149;
    --purple: #bc8cff;
    --orange: #d18616;
    --cyan: #39d353;
}}
* {{ box-sizing: border-box; margin: 0; padding: 0; }}
body {{
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif;
    background: var(--bg);
    color: var(--text);
    line-height: 1.5;
    padding: 2rem;
    max-width: 1100px;
    margin: 0 auto;
}}
h1 {{
    font-size: 1.5rem;
    margin-bottom: 0.5rem;
    color: var(--text);
    border-bottom: 1px solid var(--border);
    padding-bottom: 0.75rem;
}}
h2 {{
    font-size: 1.1rem;
    color: var(--text-muted);
    margin-bottom: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-weight: 600;
}}
.stats-bar {{
    display: flex;
    gap: 1.5rem;
    padding: 0.75rem 1rem;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 6px;
    margin-bottom: 1.5rem;
    font-size: 0.85rem;
}}
.stat {{ color: var(--text-muted); }}
.stat strong {{ color: var(--accent); }}
.section {{
    margin-bottom: 2rem;
}}
/* Prompt */
.prompt {{
    background: var(--surface);
    border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    border-radius: 6px;
    padding: 1rem 1.25rem;
    margin-bottom: 1.5rem;
    white-space: pre-wrap;
    font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
    font-size: 0.8rem;
    max-height: 400px;
    overflow-y: auto;
    color: var(--text-muted);
}}
/* CLI Timeline */
.timeline {{
    display: flex;
    flex-direction: column;
    gap: 2px;
}}
.timeline-item {{
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.4rem 0.75rem;
    border-radius: 4px;
    font-size: 0.82rem;
    font-family: 'SFMono-Regular', Consolas, monospace;
    background: var(--surface);
    border-left: 3px solid var(--border);
}}
.timestamp {{
    color: var(--text-muted);
    min-width: 5.5rem;
    font-size: 0.78rem;
}}
.cli-cmd {{ color: var(--text); }}
.cli-start {{ border-left-color: var(--green); }}
.cli-show {{ border-left-color: var(--accent); }}
.cli-check {{ border-left-color: var(--cyan); }}
.cli-transition {{ border-left-color: var(--yellow); }}
.cli-comment {{ border-left-color: var(--purple); }}
.cli-process {{ border-left-color: var(--text-muted); }}
.cli-append {{ border-left-color: var(--orange); }}
/* Conversation flow */
.conversation {{
    display: flex;
    flex-direction: column;
    gap: 4px;
}}
.event {{
    padding: 0.5rem 0.75rem;
    border-radius: 4px;
    font-size: 0.82rem;
}}
.tool-call {{
    background: var(--surface);
    border: 1px solid var(--border);
    font-family: 'SFMono-Regular', Consolas, monospace;
    display: flex;
    align-items: center;
    gap: 0.5rem;
}}
.tool-badge {{
    display: inline-block;
    background: #1f2937;
    color: var(--accent);
    padding: 0.1rem 0.5rem;
    border-radius: 3px;
    font-weight: 600;
    font-size: 0.75rem;
    white-space: nowrap;
}}
.tool-args {{
    color: var(--text-muted);
    word-break: break-all;
}}
.tool-info {{
    color: var(--text-muted);
    font-style: italic;
    font-size: 0.78rem;
    padding-left: 1rem;
}}
.assistant-text {{
    background: #1c2333;
    border-left: 3px solid var(--purple);
    color: var(--text);
    line-height: 1.6;
}}
/* Collapsible */
details {{
    margin-bottom: 1.5rem;
}}
details summary {{
    cursor: pointer;
    color: var(--accent);
    font-weight: 600;
    font-size: 0.9rem;
    padding: 0.5rem 0;
    user-select: none;
}}
details summary:hover {{ text-decoration: underline; }}
</style>
</head>
<body>
<h1>Agent Log: {title}</h1>
{stats_html}

<div class="section">
    <h2>User Prompt</h2>
    <div class="prompt">{prompt_html}</div>
</div>

{cli_timeline_html}

<div class="section">
    <h2>Conversation Flow</h2>
    <details open>
        <summary>Show {len(rawlog_data['events'])} events</summary>
        <div class="conversation">
            {conv_html}
        </div>
    </details>
</div>

</body>
</html>'''


def find_log_files(log_dir: str) -> tuple[str | None, str | None]:
    """Find rawlog and clilog files in a directory or handle a single .log file."""
    if os.path.isfile(log_dir):
        # Single file (e.g. .log)
        return log_dir, None

    rawlog = None
    clilog = None

    for f in os.listdir(log_dir):
        full = os.path.join(log_dir, f)
        if f == 'rawlog':
            rawlog = full
        elif f.endswith('.clilog'):
            clilog = full

    return rawlog, clilog


def generate_index_html(entries: list[dict]) -> str:
    """Generate an index HTML page listing all processed logs."""
    rows_html = ''
    for e in entries:
        title = html.escape(e['title'])
        filename = html.escape(e['filename'])
        events = e['event_count']
        cli_events = e['cli_count']
        stats = e.get('stats')
        tokens = f"{stats['out']}" if stats else '—'
        context = f"{stats['pct']}%" if stats else '—'

        ts_range = ''
        if e.get('start_ts') and e.get('end_ts'):
            try:
                start = datetime.fromisoformat(e['start_ts'].replace('Z', '+00:00'))
                end = datetime.fromisoformat(e['end_ts'].replace('Z', '+00:00'))
                duration = end - start
                mins = int(duration.total_seconds() / 60)
                ts_range = f'{start.strftime("%H:%M")} — {end.strftime("%H:%M")} ({mins}m)'
            except:
                ts_range = ''

        rows_html += f'''
            <tr>
                <td><a href="{filename}">{title}</a></td>
                <td class="num">{events}</td>
                <td class="num">{cli_events}</td>
                <td class="num">{tokens}</td>
                <td class="num">{context}</td>
                <td class="ts">{ts_range}</td>
            </tr>'''

    return f'''<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Agent Logs</title>
<style>
:root {{
    --bg: #0d1117;
    --surface: #161b22;
    --border: #30363d;
    --text: #e6edf3;
    --text-muted: #8b949e;
    --accent: #58a6ff;
}}
* {{ box-sizing: border-box; margin: 0; padding: 0; }}
body {{
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif;
    background: var(--bg);
    color: var(--text);
    padding: 2rem;
    max-width: 1100px;
    margin: 0 auto;
}}
h1 {{
    font-size: 1.5rem;
    margin-bottom: 1.5rem;
    border-bottom: 1px solid var(--border);
    padding-bottom: 0.75rem;
}}
table {{
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85rem;
}}
th {{
    text-align: left;
    color: var(--text-muted);
    font-weight: 600;
    padding: 0.5rem 0.75rem;
    border-bottom: 2px solid var(--border);
    font-size: 0.78rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
}}
td {{
    padding: 0.6rem 0.75rem;
    border-bottom: 1px solid var(--border);
}}
tr:hover {{ background: var(--surface); }}
a {{ color: var(--accent); text-decoration: none; }}
a:hover {{ text-decoration: underline; }}
.num {{ text-align: center; color: var(--text-muted); font-family: monospace; }}
.ts {{ color: var(--text-muted); font-size: 0.78rem; font-family: monospace; }}
</style>
</head>
<body>
<h1>Agent Logs ({len(entries)})</h1>
<table>
<thead>
<tr>
    <th>Agent Session</th>
    <th>Events</th>
    <th>CLI Calls</th>
    <th>Output Tokens</th>
    <th>Context</th>
    <th>Time</th>
</tr>
</thead>
<tbody>
{rows_html}
</tbody>
</table>
</body>
</html>'''


def process_one(log_dir: str, output_dir: str) -> dict | None:
    """Process a single agent log directory. Returns metadata for the index."""
    log_name = os.path.basename(log_dir)

    rawlog_path, clilog_path = find_log_files(log_dir)
    if not rawlog_path:
        # Check if it's a single .log file
        if log_dir.endswith('.log') and os.path.isfile(log_dir):
            rawlog_path = log_dir
        else:
            return None

    try:
        rawlog_data = parse_rawlog(rawlog_path)
    except Exception as e:
        print(f'  Error parsing {log_name}: {e}', file=sys.stderr)
        return None

    clilog_events = []
    if clilog_path:
        clilog_events = parse_clilog(clilog_path)

    html_content = generate_html(rawlog_data, clilog_events, log_name)
    out_file = f'{log_name}.html'
    out_path = os.path.join(output_dir, out_file)

    with open(out_path, 'w') as f:
        f.write(html_content)

    title = log_name.replace('agent-', '').replace('-', ' ').title()
    start_ts = clilog_events[0].get('ts') if clilog_events else None
    end_ts = clilog_events[-1].get('ts') if clilog_events else None

    return {
        'title': title,
        'filename': out_file,
        'event_count': len(rawlog_data['events']),
        'cli_count': len(clilog_events),
        'stats': rawlog_data.get('stats'),
        'start_ts': start_ts,
        'end_ts': end_ts,
    }


def main():
    parser = argparse.ArgumentParser(description='Parse and visualize Claude Code agent logs')
    parser.add_argument('path', help='Agent log directory, or parent directory containing multiple agent-* dirs')
    parser.add_argument('-o', '--output', help='Output directory for HTML files (default: current dir)', default='.')
    args = parser.parse_args()

    path = args.path.rstrip('/')
    output_dir = args.output

    os.makedirs(output_dir, exist_ok=True)

    # Check if this is a single log dir or a parent with multiple logs
    rawlog_check, _ = find_log_files(path)
    if rawlog_check:
        # Single log directory
        result = process_one(path, output_dir)
        if result:
            print(f'Written {result["filename"]} ({result["event_count"]} events, {result["cli_count"]} CLI calls)')
        else:
            print('Error: could not parse log', file=sys.stderr)
            sys.exit(1)
    else:
        # Parent directory — process all agent-* subdirs
        subdirs = sorted([
            os.path.join(path, d) for d in os.listdir(path)
            if os.path.isdir(os.path.join(path, d)) and d.startswith('agent-')
        ])
        # Also check for .log files
        logfiles = sorted([
            os.path.join(path, f) for f in os.listdir(path)
            if f.endswith('.log') and os.path.isfile(os.path.join(path, f))
        ])

        all_entries = subdirs + logfiles
        if not all_entries:
            print(f'No agent-* directories or .log files found in {path}', file=sys.stderr)
            sys.exit(1)

        entries = []
        for entry in all_entries:
            name = os.path.basename(entry)
            print(f'Processing {name}...')
            result = process_one(entry, output_dir)
            if result:
                entries.append(result)
                print(f'  {result["event_count"]} events, {result["cli_count"]} CLI calls')
            else:
                print(f'  Skipped (no rawlog)')

        # Generate index
        if entries:
            index_html = generate_index_html(entries)
            index_path = os.path.join(output_dir, 'index.html')
            with open(index_path, 'w') as f:
                f.write(index_html)
            print(f'\nGenerated index.html with {len(entries)} logs')


if __name__ == '__main__':
    main()
