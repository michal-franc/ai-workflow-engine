import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { Approvals, PlaytestStep } from '../types'

// Approve buttons above the prompt of a dispatched agent's session: the same
// approvals the AI Workflow Engine board's Human Approval box offers for the
// session's issue. Dispatch exports ISSUE_VIEWER_PROJECT, ISSUE_VIEWER_ISSUE_SLUG
// and ISSUE_VIEWER_URL; without them (a lead, a hand-started session) the mod
// shows nothing.
//
// The available approvals come from GET …/issue/<slug>/approvals.json, which
// lists the approval-gated transitions out of the issue's current status (the
// workflow with its type and system overlays). A press POSTs …/approve like the
// board's checkbox: it toggles, and the board nudges the agent's tmux window.

const approvals = atom({ plugin: 'issue-approve', key: 'approvals' } as const, null)
const sending = atom({ plugin: 'issue-approve', key: 'sending' } as const, '')
const hasPlaytest = atom({ plugin: 'issue-approve', key: 'hasPlaytest' } as const, false)
const playtest = atom({ plugin: 'issue-approve', key: 'playtest' } as const, 'idle' as PlaytestStep)

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
// or a project's own "playtest…" status).
function readyForPlaytest(status: string): boolean {
  return /human-testing|playtest/.test(status.toLowerCase())
}

// runPlaytest runs `make playtest ISSUE=<slug>` in the session's worktree: the
// overlay opens on a scenario board and the owner judges the steps. It runs as
// long as the owner needs; the end of its output goes in the toast if it fails.
async function runPlaytest($: EngineInterface, slug: string) {
  await update($, playtest, () => 'running')
  let tail = ''
  let code: number | null = null
  try {
    const child = $.process.spawn({ argv: ['make', 'playtest', `ISSUE=${slug}`] })
    for await (const piece of child) {
      tail = (tail + piece.text).slice(-400)
    }
    code = (await child.result).code
  } catch (err) {
    tail = String(err)
  }
  if (code === 0) {
    $.ui.toast('Playtest finished: press Report to tick the boxes that passed')
    await update($, playtest, () => 'ran')
  } else {
    $.ui.toast(`Playtest ended with ${code ?? 'an error'}: ${tail.trim().split('\n').pop() ?? ''}`)
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

  on('session.start', async ($, e, next) => {
    slug = (await $.env.get('ISSUE_VIEWER_ISSUE_SLUG')) ?? ''
    const url = ((await $.env.get('ISSUE_VIEWER_URL')) || 'http://localhost:8080').replace(/\/+$/, '')
    let project = await $.env.get('ISSUE_VIEWER_PROJECT')
    if (slug && !project) project = await projectFor($, url, e.cwd)
    if (slug && project) {
      base = `${url}/p/${project}/issue/${slug}`
      await refresh($, base)
      try {
        const probe = await $.process.run(['make', '-n', 'playtest', `ISSUE=${slug}`], { timeoutMs: 10000 })
        await update($, hasPlaytest, () => probe.exitCode === 0)
      } catch {
        await update($, hasPlaytest, () => false)
      }
      $.clock.every(REFRESH_MS, () => {
        void refresh($, base)
      })
    }
    return next(e)
  })

  // A turn often moves the issue (a transition, a new gate): look again.
  on('turn.complete', async ($, e, next) => {
    await refresh($, base)
    return next(e)
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const a = await read($, approvals)
    const step = await read($, playtest)
    const canPlaytest = !!a && (await read($, hasPlaytest)) && (readyForPlaytest(a.status) || step !== 'idle')
    if (e.props.hasSurvey || !a || (a.approvals.length === 0 && !canPlaytest)) {
      return next(e)
    }
    const busy = await read($, sending)
    const { Box, Button, Text } = $.ui.resolve(e)

    return (
      <Box>
        <Text dimColor>Human approval · {a.status}: </Text>
        {canPlaytest && step === 'idle' && (
          <Button key="playtest" variant="primary" onPress={() => runPlaytest($, slug)}>
            ▶ Playtest
          </Button>
        )}
        {step === 'running' && <Text>⏳ playtest running… </Text>}
        {step === 'ran' && (
          <Button key="report" variant="primary" onPress={() => runReport($, slug, base)}>
            ✓ Report
          </Button>
        )}
        {step === 'reporting' && <Text>⏳ reporting… </Text>}
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
