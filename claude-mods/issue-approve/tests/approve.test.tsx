import { expect, mock, test } from 'claude-code/testing'

const BAND = {
  plugin: 'issue-approve',
  component: 'AbovePrompt',
  props: { hasSurvey: false, isWorking: false, rows: 10, columns: 100 },
} as const

const ENV: Record<string, string> = {
  ISSUE_VIEWER_ISSUE_SLUG: 'ui/foo',
  ISSUE_VIEWER_PROJECT: 'proj',
}

// The board's address comes from `issue-cli projects --json`, like every
// issue-cli link.
const PROJECTS = JSON.stringify([
  { slug: 'other', viewer_url: 'http://board:8080' },
  { slug: 'proj', viewer_url: 'http://board:8080/', active: true },
])
const isProjects = (argv: readonly string[]) => argv[0] === 'issue-cli' && argv.includes('projects')

test('shows the issue approvals and approves on press', async ($, on) => {
  let approved = ''
  const posts: string[] = []
  on('env.get', (_, e) => ({ value: ENV[e.name] }))
  on('clock.every', () => ({ deny: 'no timers in tests' }))
  on('http.fetch', (_, e) => {
    if (e.init?.method === 'POST') {
      posts.push(`${e.url} ${e.init.body}`)
      approved = JSON.parse(e.init.body ?? '{}').status
      return { value: { status: 200, ok: true, headers: {}, text: '{}' } }
    }
    const body = {
      slug: 'ui/foo', title: 'Foo', status: 'in design', human_approval: approved,
      approvals: [
        { status: 'backlog', approved: approved === 'backlog' },
        { status: 'in progress', approved: approved === 'in progress' },
      ],
    }
    return { value: { status: 200, ok: true, headers: {}, text: JSON.stringify(body) } }
  })
  on('ui.toast', () => ({ value: {} }))
  const runs: string[] = []
  on('process.run', (_, e) => {
    runs.push(e.argv.join(' '))
    return { value: isProjects(e.argv) ? { exitCode: 0, stdout: PROJECTS, stderr: '' } : { exitCode: 2, stdout: '', stderr: '' } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))

  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  expect(runs[0]).toBe('issue-cli --project proj projects --json')

  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect((await ui.find({ type: 'Text', text: /Human approval · in design/ }))?.text).toBeDefined()
  expect((await ui.find({ key: 'backlog' }))?.text).toBe('☐ backlog')
  await ui.press({ key: 'in progress' })
  expect(posts).toEqual(['http://board:8080/p/proj/issue/ui/foo/approve {"status":"in progress"}'])
  expect((await ui.find({ key: 'in progress' }))?.text).toBe('☑ in progress')
  await ui.unmount()
})

test('outside a dispatched agent it draws nothing of its own', async ($, on) => {
  on('env.get', () => ({ value: undefined }))
  on('process.run', (_, e) => {
    throw new Error(`ran ${e.argv.join(' ')} outside a dispatched agent`)
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('ui.render', ($, e) => {
    const { Text } = $.ui.resolve(e)
    return <Text>engine</Text>
  })
  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect((await ui.find({ type: 'Text', text: /engine/ }))?.text).toBe('engine')
  await ui.unmount()
})

test('without ISSUE_VIEWER_PROJECT it asks the board which project the folder is in', async ($, on) => {
  const urls: string[] = []
  const env: Record<string, string> = { ISSUE_VIEWER_ISSUE_SLUG: 'ui/foo' }
  on('env.get', (_, e) => ({ value: env[e.name] }))
  on('clock.every', () => ({ deny: 'no timers in tests' }))
  on('http.fetch', (_, e) => {
    urls.push(e.url)
    const text = e.url.includes('/project-for?')
      ? JSON.stringify({ project: 'proj' })
      : JSON.stringify({ slug: 'ui/foo', title: 'Foo', status: 'backlog', human_approval: '', approvals: [{ status: 'in progress', approved: false }] })
    return { value: { status: 200, ok: true, headers: {}, text } }
  })
  const runs: string[] = []
  // issue-cli without a usable config: the default board.
  on('process.run', (_, e) => {
    runs.push(e.argv.join(' '))
    return { value: { exitCode: 1, stdout: '', stderr: 'no projects config found' } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  await $.session.start({ cwd: '/w/proj/.worktrees/ui/foo', surface: 'terminal', isInteractive: true })
  expect(runs[0]).toBe('issue-cli projects --json')
  expect(urls).toEqual([
    'http://localhost:8080/project-for?cwd=%2Fw%2Fproj%2F.worktrees%2Fui%2Ffoo',
    'http://localhost:8080/p/proj/issue/ui/foo/approvals.json',
  ])
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect((await ui.find({ key: 'in progress' }))?.text).toBe('☐ in progress')
  await ui.unmount()
})

// ▶ Playtest never runs the playtest as a child of the agent's process: it
// opens a visible tmux window and returns at once (D-2).
const TMUX_ENV: Record<string, string> = { ...ENV, TMUX: '/tmp/tmux-1000/default,1,0', TMUX_PANE: '%7' }

test('at human-testing Playtest opens make playtest in a tmux window, then Report applies the results', async ($, on) => {
  const runs: string[] = []
  on('env.get', (_, e) => ({ value: TMUX_ENV[e.name] }))
  on('clock.every', () => ({ deny: 'no timers in tests' }))
  const toasts: string[] = []
  on('ui.toast', (_, e) => {
    toasts.push(JSON.stringify(e))
    return { value: {} }
  })
  on('http.fetch', () => {
    const body = { slug: 'ui/foo', title: 'Foo', status: 'human-testing', human_approval: '', approvals: [{ status: 'documentation', approved: false }] }
    return { value: { status: 200, ok: true, headers: {}, text: JSON.stringify(body) } }
  })
  on('process.run', (_, e) => {
    if (isProjects(e.argv)) return { value: { exitCode: 0, stdout: PROJECTS, stderr: '' } }
    runs.push(e.argv.join(' '))
    if (e.argv[0] === 'test') return { value: { exitCode: 1, stdout: '', stderr: '' } }
    if (e.argv[1] === 'display-message') return { value: { exitCode: 0, stdout: 'issue-viewer\n', stderr: '' } }
    if (e.argv[1] === 'new-window') return { value: { exitCode: 0, stdout: 'issue-viewer:playtest-foo\n', stderr: '' } }
    return { value: { exitCode: 0, stdout: 'ticked TP6 TP10\n', stderr: '' } }
  })
  // eslint-disable-next-line require-yield
  on('process.spawn', async function* ($, e, next) {
    void $
    void next
    throw new Error(`spawned ${e.argv.join(' ')} under the agent`)
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  await $.session.start({ cwd: '/w/proj/.worktrees/ui/foo', surface: 'terminal', isInteractive: true })
  expect(runs).toEqual([
    'test -x scripts/playtest-window',
    'make -n playtest ISSUE=ui/foo',
    'make -n playtest-report ISSUE=ui/foo',
    'issue-cli --project proj check-script ui/foo --list',
  ])

  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect((await ui.find({ key: 'playtest' }))?.text).toBe('▶ Playtest')
  // Run checks sits next to Playtest when the issue has a check script too (owner's call, D-8).
  expect((await ui.find({ key: 'checks' }))?.text).toBe('▶ Run checks')
  expect((await ui.find({ key: 'documentation' }))?.text).toBe('☐ documentation')
  await ui.press({ key: 'playtest' })
  expect(runs[4]).toBe('tmux display-message -p -t %7 #S')
  expect(runs[5]).toBe(
    "tmux new-window -d -P -F #{session_name}:#{window_name} -t issue-viewer: -n playtest-foo -c /w/proj/.worktrees/ui/foo bash -c make playtest ISSUE='ui/foo'; echo; echo 'Playtest closed.'; read -r -p 'Enter closes this window. ' _",
  )
  expect(toasts[0]).toContain('Playtest opened in tmux window issue-viewer:playtest-foo')
  // The window stays open: Playtest again (reopen) and Report.
  expect((await ui.find({ key: 'playtest' }))?.text).toBe('▶ Playtest')
  expect((await ui.find({ key: 'report' }))?.text).toBe('✓ Report')
  await ui.press({ key: 'report' })
  expect(runs[6]).toBe('make playtest-report ISSUE=ui/foo APPLY=1')
  expect(!!(await ui.find({ key: 'report' }))).toBe(false)
  await ui.unmount()
})

test("a project's scripts/playtest-window opens the playtest; no Report without a playtest-report target", async ($, on) => {
  const runs: string[] = []
  const toasts: string[] = []
  on('env.get', (_, e) => ({ value: TMUX_ENV[e.name] }))
  on('clock.every', () => ({ deny: 'no timers in tests' }))
  on('ui.toast', (_, e) => {
    toasts.push(JSON.stringify(e))
    return { value: {} }
  })
  on('http.fetch', () => {
    const body = { slug: 'combat/bar', title: 'Bar', status: 'human-testing', human_approval: '', approvals: [{ status: 'documentation', approved: false }] }
    return { value: { status: 200, ok: true, headers: {}, text: JSON.stringify(body) } }
  })
  on('process.run', (_, e) => {
    if (isProjects(e.argv)) return { value: { exitCode: 0, stdout: PROJECTS, stderr: '' } }
    runs.push(e.argv.join(' '))
    if (e.argv[0] === 'test') return { value: { exitCode: 0, stdout: '', stderr: '' } }
    if (e.argv[0] === 'scripts/playtest-window') return { value: { exitCode: 0, stdout: 'roguetech:playtest-bar\n', stderr: '' } }
    return { value: { exitCode: 1, stdout: '', stderr: '' } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  await $.session.start({ cwd: '/w/rt', surface: 'terminal', isInteractive: true })
  expect(runs.slice(0, 2)).toEqual(['test -x scripts/playtest-window', 'make -n playtest-report ISSUE=ui/foo'])

  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  await ui.press({ key: 'playtest' })
  expect(runs).toContain('scripts/playtest-window ui/foo')
  expect(runs.some(r => r.startsWith('tmux'))).toBe(false)
  expect(toasts[0]).toContain('Playtest opened in tmux window roguetech:playtest-bar')
  expect(!!(await ui.find({ key: 'report' }))).toBe(false)
  expect((await ui.find({ key: 'playtest' }))?.text).toBe('▶ Playtest')
  await ui.unmount()
})

test('outside tmux Playtest says so and opens nothing', async ($, on) => {
  const runs: string[] = []
  const toasts: string[] = []
  on('env.get', (_, e) => ({ value: ENV[e.name] }))
  on('clock.every', () => ({ deny: 'no timers in tests' }))
  on('ui.toast', (_, e) => {
    toasts.push(JSON.stringify(e))
    return { value: {} }
  })
  on('http.fetch', () => {
    const body = { slug: 'ui/foo', title: 'Foo', status: 'human-testing', human_approval: '', approvals: [] }
    return { value: { status: 200, ok: true, headers: {}, text: JSON.stringify(body) } }
  })
  on('process.run', (_, e) => {
    if (isProjects(e.argv)) return { value: { exitCode: 0, stdout: PROJECTS, stderr: '' } }
    runs.push(e.argv.join(' '))
    return { value: { exitCode: e.argv[0] === 'test' ? 1 : 0, stdout: '', stderr: '' } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  await $.session.start({ cwd: '/w/proj', surface: 'terminal', isInteractive: true })
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  await ui.press({ key: 'playtest' })
  expect(runs.some(r => r.startsWith('tmux'))).toBe(false)
  expect(toasts[0]).toContain('not inside tmux; run make playtest ISSUE=ui/foo in a terminal')
  await ui.unmount()
})

// A tweak's path has no human-testing: the owner checks it at testing, behind
// the shipping gate, so Playtest shows there too, and only behind a gate.
for (const [name, approvals, shown] of [
  ['at testing behind an owner gate (a tweak) it offers Playtest', [{ status: 'shipping', approved: false }], true],
  ['at testing with no owner gate it offers no Playtest', [], false],
] as const) {
  test(name, async ($, on) => {
    on('env.get', (_, e) => ({ value: ENV[e.name] }))
    on('clock.every', () => ({ deny: 'no timers in tests' }))
    on('http.fetch', () => {
      const body = { slug: 'ui/foo', title: 'Foo', status: 'testing', human_approval: '', approvals }
      return { value: { status: 200, ok: true, headers: {}, text: JSON.stringify(body) } }
    })
    on('process.run', () => ({ value: { exitCode: 0, stdout: '', stderr: '' } }))
    on('session.start', (_, e) => ({ cwd: e.cwd }))
    on('ui.render', ($, e) => {
      const { Text } = $.ui.resolve(e)
      return <Text>engine</Text>
    })
    await $.session.start({ cwd: '/w/proj/.worktrees/ui/foo', surface: 'terminal', isInteractive: true })
    const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
    expect(!!(await ui.find({ key: 'playtest' }))).toBe(shown)
    await ui.unmount()
  })
}

test('at human-testing with a check script it offers Run checks, which asks the board to open the window', async ($, on) => {
  const posts: string[] = []
  const toasts: string[] = []
  let hasScript = false
  on('env.get', (_, e) => ({ value: ENV[e.name] }))
  on('clock.every', () => ({ deny: 'no timers in tests' }))
  on('ui.toast', (_, e) => {
    toasts.push(JSON.stringify(e))
    return { value: {} }
  })
  on('http.fetch', (_, e) => {
    if (e.init?.method === 'POST') {
      posts.push(e.url)
      const d = { status: 'started', session: 'check-script-ui-foo', steps: [{ name: 'Create tmux window', status: 'ok' }] }
      return { value: { status: 200, ok: true, headers: {}, text: JSON.stringify(d) } }
    }
    const body = { slug: 'ui/foo', title: 'Foo', status: 'human-testing', human_approval: '', approvals: [{ status: 'documentation', approved: false }] }
    return { value: { status: 200, ok: true, headers: {}, text: JSON.stringify(body) } }
  })
  // No Makefile playtest here; the check script appears during a turn.
  on('process.run', (_, e) => {
    if (isProjects(e.argv)) return { value: { exitCode: 0, stdout: PROJECTS, stderr: '' } }
    const isProbe = e.argv[0] === 'issue-cli'
    return { value: { exitCode: isProbe && hasScript ? 0 : 1, stdout: '', stderr: '' } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('turn.complete', () => ({ text: '' }))
  await $.session.start({ cwd: '/w/proj/.worktrees/ui/foo', surface: 'terminal', isInteractive: true })

  let ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect(!!(await ui.find({ key: 'checks' }))).toBe(false)
  expect(!!(await ui.find({ key: 'playtest' }))).toBe(false)
  await ui.unmount()

  hasScript = true
  await $.turn.complete({ answer: '', durationMs: 1, isAborted: false, turnId: 't1', reason: 'answer' })
  ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect((await ui.find({ key: 'checks' }))?.text).toBe('▶ Run checks')
  await ui.press({ key: 'checks' })
  expect(posts).toEqual(['http://board:8080/p/proj/issue/ui/foo/check-script'])
  expect(toasts.at(-1)).toContain('Started check-script-ui-foo')
  await ui.unmount()
})

// A lead's first /exit writes the handoff note instead of exiting; the next
// one leaves. Workers' /exit is never touched.
const LEAD_ENV: Record<string, string> = { ISSUE_VIEWER_PROJECT: 'proj', ISSUE_VIEWER_LEAD_SCOPE: 'project' }

test("a lead's first /exit starts the handoff turn, the next one exits", async ($, on) => {
  const prompts: string[] = []
  let exited = 0
  const store: Record<string, unknown> = {}
  on('env.get', (_, e) => ({ value: LEAD_ENV[e.name] }))
  on('store.get', (_, e) => ({ value: store[e.key] }))
  on('store.set', (_, e) => { store[e.key] = e.value; return { value: undefined } })
  on('store.delete', (_, e) => { delete store[e.key]; return { value: undefined } })
  on('prompt.submit', (_, e) => { prompts.push(e.text); return { text: e.text } })
  const clock = mock.clock(on)
  on('command.run', () => { exited++; return { text: 'bye' } })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('turn.complete', () => ({ text: '' }))
  on('session.end', (_, e) => ({ sessionId: e.sessionId }))
  await $.session.start({ cwd: '/w/proj', surface: 'terminal', isInteractive: true })

  const first = await $.command.run({ command: 'exit', args: '' })
  await clock.settle()
  expect(exited).toBe(0)
  expect(first.text).toContain('Writing the handoff note first')
  expect(prompts[0]).toContain('write your handoff note now')

  await $.turn.complete({ answer: 'done', durationMs: 1, isAborted: false, turnId: 't1', reason: 'answer' })
  await $.command.run({ command: 'exit', args: '' })
  expect(exited).toBe(1)
  await $.session.end({ reason: 'prompt_input_exit', sessionId: 's1', resume: { sessionId: 's1' } })
  expect(store['missedHandoff:proj']).toBeUndefined()
})

test("a worker's /exit exits at once", async ($, on) => {
  let exited = 0
  on('env.get', (_, e) => ({ value: { ...ENV, ISSUE_VIEWER_LEAD_SCOPE: 'project' }[e.name] }))
  on('clock.every', () => ({ deny: 'no timers in tests' }))
  on('http.fetch', () => ({ value: { status: 200, ok: true, headers: {}, text: JSON.stringify({ slug: 'ui/foo', title: 'Foo', status: 'in progress', human_approval: '', approvals: [] }) } }))
  on('process.run', (_, e) => ({ value: isProjects(e.argv) ? { exitCode: 0, stdout: PROJECTS, stderr: '' } : { exitCode: 1, stdout: '', stderr: '' } }))
  on('prompt.submit', () => { throw new Error('a worker got a handoff prompt') })
  on('command.run', () => { exited++; return { text: 'bye' } })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  await $.session.start({ cwd: '/w/proj/.worktrees/ui/foo', surface: 'terminal', isInteractive: true })
  await $.command.run({ command: 'exit', args: '' })
  expect(exited).toBe(1)
})

test('a lead closed without /exit leaves a note; the next lead session reads it in its first prompt', async ($, on) => {
  const store: Record<string, unknown> = {}
  const seen: string[] = []
  on('env.get', (_, e) => ({ value: LEAD_ENV[e.name] }))
  on('clock.every', () => ({ deny: 'no timers in tests' }))
  on('store.get', (_, e) => ({ value: store[e.key] }))
  on('store.set', (_, e) => { store[e.key] = e.value; return { value: undefined } })
  on('store.delete', (_, e) => { delete store[e.key]; return { value: undefined } })
  on('prompt.submit', (_, e) => { seen.push(e.text); return { text: e.text } })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  on('session.end', (_, e) => ({ sessionId: e.sessionId }))
  await $.session.start({ cwd: '/w/proj', surface: 'terminal', isInteractive: true })
  await $.session.end({ reason: 'other', sessionId: 's1', resume: { sessionId: 's1' } })
  expect((store['missedHandoff:proj'] as { at: string }).at).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/)

  await $.session.start({ cwd: '/w/proj', surface: 'terminal', isInteractive: true })
  await $.prompt.submit({ text: 'briefing' })
  expect(seen[0]).toContain('ended at')
  expect(seen[0]).toContain('without its handoff note')
  expect(store['missedHandoff:proj']).toBeUndefined()
})
