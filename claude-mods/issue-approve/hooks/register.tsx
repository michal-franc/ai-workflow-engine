import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Approvals, HandoffStep, PlaytestLauncher, PlaytestStep } from '../types'

// Approve buttons above the prompt of a dispatched agent's session: the same
// approvals the AI Workflow Engine board's Human Approval box offers for the
// session's issue. Dispatch exports ISSUE_VIEWER_PROJECT and
// ISSUE_VIEWER_ISSUE_SLUG; without a slug (a lead, a hand-started session) the
// mod shows nothing. The board's address is the one issue-cli uses (viewer_url
// in projects.yaml, else the running viewer's), read from
// `issue-cli projects --json`.
//
// The available approvals come from GET …/issue/<slug>/approvals.json, which
// lists the approval-gated transitions out of the issue's current status (the
// workflow with its type and system overlays). A press POSTs …/approve like the
// board's checkbox: it toggles, and the board nudges the agent's tmux window.

const approvals = atom({ plugin: 'issue-approve', key: 'approvals' } as const, null)
const sending = atom({ plugin: 'issue-approve', key: 'sending' } as const, '')
const playtestLauncher = atom({ plugin: 'issue-approve', key: 'playtestLauncher' } as const, '' as PlaytestLauncher)
const hasReport = atom({ plugin: 'issue-approve', key: 'hasReport' } as const, false)
const playtest = atom({ plugin: 'issue-approve', key: 'playtest' } as const, 'idle' as PlaytestStep)
const hasChecks = atom({ plugin: 'issue-approve', key: 'hasChecks' } as const, false)
const handoff = atom({ plugin: 'issue-approve', key: 'handoff' } as const, 'idle' as HandoffStep)

const REFRESH_MS = 15000

// refresh reads the issue's approvals from the board into state; base is
// http://host/p/<project>/issue/<slug>.
async function refresh($: EngineInterface, base: string) {
  if (!base) return
  let next: Approvals | null = null
  try {
    const { ok, text } = await $.http.fetch(`${base}/approvals.json`)
    if (ok) next = JSON.parse(text) as Approvals
  } catch {
    next = null
  }
  await update($, approvals, () => next)
}

const DEFAULT_URL = 'http://localhost:8080'

// viewerURL asks issue-cli where the board is, so the mod and the CLI never
// disagree: the project's viewer_url from `issue-cli projects --json` (the
// active entry, else the first), else the default.
async function viewerURL($: EngineInterface, project: string): Promise<string> {
  try {
    const argv = ['issue-cli', ...(project ? ['--project', project] : []), 'projects', '--json']
    const r = await $.process.run(argv, { timeoutMs: 10000 })
    if (r.exitCode === 0) {
      const entries = JSON.parse(r.stdout) as { viewer_url?: string; active?: boolean }[]
      const entry = entries.find(p => p.active) ?? entries[0]
      if (entry?.viewer_url) return entry.viewer_url.replace(/\/+$/, '')
    }
  } catch {
    // issue-cli missing or its config unreadable: the default board
  }
  return DEFAULT_URL
}

// projectFor asks the board which project's work directory holds cwd: the
// fallback for agents dispatched before ISSUE_VIEWER_PROJECT was exported.
async function projectFor($: EngineInterface, url: string, cwd: string): Promise<string> {
  try {
    const { ok, text } = await $.http.fetch(`${url}/project-for?cwd=${encodeURIComponent(cwd)}`)
    if (ok) return (JSON.parse(text) as { project?: string }).project ?? ''
  } catch {
    // board down: no buttons this session until a reload
  }
  return ''
}

// readyForPlaytest: the issue waits on the owner's playtest (human-testing,
// or a project's own "playtest…" status), or sits at testing behind an owner
// gate: a tweak's path has no human-testing, the owner checks it at testing.
function readyForPlaytest(a: Approvals): boolean {
  const status = a.status.toLowerCase()
  return /human-testing|playtest/.test(status) || (status === 'testing' && a.approvals.length > 0)
}

// The two ways ▶ Playtest opens the owner's playtest, probed once at start: the
// project's own scripts/playtest-window <slug> (RogueTech), else a tmux window
// the mod opens on `make playtest ISSUE=<slug>`. Never a child of the agent's
// process: a game started that way is a window the owner can neither see nor
// stop (D-2).
async function probePlaytest($: EngineInterface, slug: string) {
  let launcher: PlaytestLauncher = ''
  let report = false
  try {
    if ((await $.process.run(['test', '-x', WINDOW_SCRIPT], { timeoutMs: 10000 })).exitCode === 0) launcher = 'script'
    else if ((await $.process.run(['make', '-n', 'playtest', `ISSUE=${slug}`], { timeoutMs: 10000 })).exitCode === 0) launcher = 'make'
    if (launcher) report = (await $.process.run(['make', '-n', 'playtest-report', `ISSUE=${slug}`], { timeoutMs: 10000 })).exitCode === 0
  } catch {
    launcher = ''
  }
  await update($, playtestLauncher, () => launcher)
  await update($, hasReport, () => report)
}

const WINDOW_SCRIPT = 'scripts/playtest-window'

// openPlaytestWindow opens `make playtest ISSUE=<slug>` in a new detached
// window of the agent's tmux session, in its worktree, and returns the
// window's <session>:<name>. The window waits for Enter when the playtest ends.
async function openPlaytestWindow($: EngineInterface, slug: string, cwd: string): Promise<{ ok: boolean; text: string }> {
  const pane = (await $.env.get('TMUX_PANE')) ?? ''
  if (!(await $.env.get('TMUX'))) return { ok: false, text: `not inside tmux; run make playtest ISSUE=${slug} in a terminal` }
  const s = await $.process.run(['tmux', 'display-message', '-p', ...(pane ? ['-t', pane] : []), '#S'], { timeoutMs: 10000 })
  if (s.exitCode !== 0) return { ok: false, text: s.stderr.trim() || 'tmux has no session here' }
  const name = `playtest-${slug.split('/').pop()}`.slice(0, 40)
  const script = `make playtest ISSUE='${slug}'; echo; echo 'Playtest closed.'; read -r -p 'Enter closes this window. ' _`
  const w = await $.process.run(
    ['tmux', 'new-window', '-d', '-P', '-F', '#{session_name}:#{window_name}', '-t', `${s.stdout.trim()}:`, '-n', name, '-c', cwd, 'bash', '-c', script],
    { timeoutMs: 10000 },
  )
  return w.exitCode === 0 ? { ok: true, text: w.stdout.trim() } : { ok: false, text: w.stderr.trim() || `tmux exited ${w.exitCode}` }
}

// runPlaytest opens the owner's playtest in a visible tmux window and returns
// at once: the owner judges the steps there, then presses Report.
async function runPlaytest($: EngineInterface, slug: string, cwd: string) {
  await update($, playtest, () => 'opening')
  let r: { ok: boolean; text: string }
  try {
    if ((await read($, playtestLauncher)) === 'script') {
      const x = await $.process.run([WINDOW_SCRIPT, slug], { timeoutMs: 15000 })
      r = { ok: x.exitCode === 0, text: (x.exitCode === 0 ? x.stdout : x.stderr).trim().split('\n').pop() ?? '' }
    } else {
      r = await openPlaytestWindow($, slug, cwd)
    }
  } catch (err) {
    r = { ok: false, text: String(err) }
  }
  const report = await read($, hasReport)
  if (r.ok) {
    $.ui.toast(`Playtest opened in tmux window ${r.text}${report ? ': judge the steps there, then press Report' : ''}`)
    await update($, playtest, () => (report ? 'ran' : 'idle'))
  } else {
    $.ui.toast(`Playtest did not open: ${r.text}`)
    await update($, playtest, () => 'idle')
  }
}

// runReport runs `make playtest-report ISSUE=<slug> APPLY=1`: it ticks the
// Manual boxes that passed and comments failures and notes on the issue.
async function runReport($: EngineInterface, slug: string, base: string) {
  await update($, playtest, () => 'reporting')
  try {
    const r = await $.process.run(['make', 'playtest-report', `ISSUE=${slug}`, 'APPLY=1'], { timeoutMs: 120000 })
    const last = (r.stdout + r.stderr).trim().split('\n').pop() ?? ''
    $.ui.toast(r.exitCode === 0 ? `Playtest report applied. ${last}` : `playtest-report failed (${r.exitCode}): ${last}`)
  } catch (err) {
    $.ui.toast(`playtest-report failed: ${String(err)}`)
  }
  await update($, playtest, () => 'idle')
  await refresh($, base)
}

// probeChecks asks issue-cli whether the issue has guided check scripts
// (scripts/checks/<name>.sh naming it): `check-script --list` exits 0 when it does.
async function probeChecks($: EngineInterface, project: string, slug: string) {
  let found = false
  try {
    const r = await $.process.run(['issue-cli', '--project', project, 'check-script', slug, '--list'], { timeoutMs: 10000 })
    found = r.exitCode === 0
  } catch {
    found = false
  }
  await update($, hasChecks, () => found)
}

// runChecks asks the board to start the owner's run of the check scripts: an
// interactive script needs a terminal, so the board opens tmux window
// check-script-<slug> in the worktree (or selects it in the shared session).
async function runChecks($: EngineInterface, base: string) {
  await update($, sending, () => 'checks')
  try {
    const res = await $.http.fetch(`${base}/check-script`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' })
    const d = JSON.parse(res.text || '{}') as { status?: string; session?: string; switch_cmd?: string; steps?: { name: string; status: string; detail?: string }[] }
    const headless = (d.steps ?? []).some(s => s.name.startsWith('Terminal: headless'))
    const bad = (d.steps ?? []).find(s => s.status === 'error')
    if (d.status === 'no-scripts') $.ui.toast('No check script names this issue')
    else if (!res.ok || d.status === 'error') $.ui.toast(`Run checks failed (${res.status}): ${bad ? bad.name : res.text.slice(0, 80)}`)
    else if (headless) $.ui.toast(`Started ${d.session}. No terminal configured; inside tmux: ${d.switch_cmd}`)
    else $.ui.toast(`${d.status === 'already-running' ? 'Brought back' : 'Started'} ${d.session}: answer the checks there`)
  } catch {
    $.ui.toast('Run checks failed: the board is not reachable')
  }
  await update($, sending, () => '')
}

// A project lead's session (ISSUE_VIEWER_LEAD_SCOPE set, no issue slug) does
// not end on the first /exit: the lead writes its handoff note first. The
// owner typed /exit, so the turn this starts is the human's (compliance: no
// session is prompted unattended). The next /exit leaves. Ctrl+C, Ctrl+D or a
// killed window can't be caught: session.end only records the missed handoff,
// and the project's next lead session is told to write it at its first turn.
const HANDOFF_PROMPT =
  'The owner typed /exit. Before this session ends, write your handoff note now, as your role brief says: ' +
  "what is live, what waits on whom, the decisions made, and a ## Next section with the ordered next steps. " +
  "Back up the project's private tracker if it has one. Then tell the owner it is done, so they can /exit again."

const missedKey = (project: string) => `missedHandoff:${project}`

function missedNote(at: string): string {
  return (
    `\n\n[issue-approve] The last lead session of this project ended at ${at} without its handoff note ` +
    '(it was closed without /exit). Write the handoff note from the board and the previous note before other work.'
  )
}

// press toggles one approval, like the board's checkbox, then re-reads.
async function press($: EngineInterface, base: string, status: string, approved: boolean) {
  await update($, sending, () => status)
  try {
    const res = await $.http.fetch(`${base}/approve`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ status }),
    })
    $.ui.toast(
      res.ok
        ? approved
          ? `Approval for ${status} removed`
          : `Approved for ${status}`
        : `Approve failed (${res.status}): ${res.text.slice(0, 80)}`,
    )
  } catch {
    $.ui.toast('Approve failed: the board is not reachable')
  }
  await update($, sending, () => '')
  await refresh($, base)
}

export const register: Register = on => {
  // http://host/p/<project>/issue/<slug>; '' outside a dispatched agent.
  let base = ''
  let slug = ''
  let project = ''
  let cwd = ''
  // A lead session: the project lead, not a worker.
  let lead = false
  // The missed-handoff note for the lead's first prompt, '' when none.
  let pendingNote = ''

  on('session.start', async ($, e, next) => {
    slug = (await $.env.get('ISSUE_VIEWER_ISSUE_SLUG')) ?? ''
    project = (await $.env.get('ISSUE_VIEWER_PROJECT')) ?? ''
    cwd = e.cwd
    lead = !slug && !!project && !!(await $.env.get('ISSUE_VIEWER_LEAD_SCOPE'))
    if (lead) {
      const missed = (await $.store.get(missedKey(project))) as { at?: string } | undefined
      if (missed?.at) pendingNote = missedNote(missed.at)
    }
    const url = slug ? await viewerURL($, project) : ''
    if (slug && !project) project = await projectFor($, url, e.cwd)
    if (slug && project) {
      base = `${url}/p/${project}/issue/${slug}`
      await refresh($, base)
      await probePlaytest($, slug)
      await probeChecks($, project, slug)
      $.clock.every(REFRESH_MS, () => {
        void refresh($, base)
      })
    }
    return next(e)
  })

  on('command.run', { command: 'exit' }, async ($, e, next) => {
    if (!lead || (await read($, handoff)) !== 'idle') return next(e)
    await update($, handoff, () => 'writing')
    // Not from inside this hook: the submit would wait on the command it holds.
    $.clock.after(0, () => {
      void $.prompt.submit({ text: HANDOFF_PROMPT })
    })
    return { text: 'Writing the handoff note first; /exit again when the lead says it is done.' }
  })

  on('prompt.submit', async ($, e, next) => {
    if (!lead || !pendingNote) return next(e)
    const note = pendingNote
    pendingNote = ''
    await $.store.delete(missedKey(project))
    return next({ ...e, text: e.text + note })
  })

  on('session.end', async ($, e, next) => {
    if (lead && e.reason !== 'clear' && e.reason !== 'resume') {
      if ((await read($, handoff)) === 'written') await $.store.delete(missedKey(project))
      else await $.store.set(missedKey(project), { at: new Date().toISOString().slice(0, 16).replace('T', ' ') })
    }
    return next(e)
  })

  // A turn often moves the issue (a transition, a new gate) or writes a check
  // script: look again.
  on('turn.complete', async ($, e, next) => {
    if (lead && (await read($, handoff)) === 'writing') await update($, handoff, () => 'written')
    await refresh($, base)
    if (base) await probeChecks($, project, slug)
    return next(e)
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const a = await read($, approvals)
    const step = await read($, playtest)
    const canPlaytest = !!a && (await read($, playtestLauncher)) !== '' && (readyForPlaytest(a) || step !== 'idle')
    const canChecks = !!a && (await read($, hasChecks)) && readyForPlaytest(a)
    if (e.props.hasSurvey || !a || (a.approvals.length === 0 && !canPlaytest && !canChecks)) {
      return next(e)
    }
    const busy = await read($, sending)
    const { Box, Button, Text } = $.ui.resolve(e)

    return (
      <Box>
        <Text dimColor>Human approval · {a.status}: </Text>
        {canPlaytest && (step === 'idle' || step === 'ran') && (
          <Button key="playtest" variant={step === 'ran' ? undefined : 'primary'} onPress={() => runPlaytest($, slug, cwd)}>
            ▶ Playtest
          </Button>
        )}
        {step === 'opening' && <Text>⏳ opening the playtest… </Text>}
        {step === 'ran' && (
          <Button key="report" variant="primary" onPress={() => runReport($, slug, base)}>
            ✓ Report
          </Button>
        )}
        {step === 'reporting' && <Text>⏳ reporting… </Text>}
        {canChecks && (
          <Button key="checks" variant="primary" onPress={() => runChecks($, base)}>
            {busy === 'checks' ? '…' : '▶ Run checks'}
          </Button>
        )}
        {a.approvals.map(x => (
          <Button
            key={x.status}
            variant={x.approved ? undefined : 'primary'}
            dimColor={x.optional && !x.approved}
            onPress={() => press($, base, x.status, x.approved)}
          >
            {busy === x.status ? '…' : x.approved ? '☑' : '☐'} {x.status}
          </Button>
        ))}
      </Box>
    )
  })
}
