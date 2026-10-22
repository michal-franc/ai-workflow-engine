import { expect, test } from 'claude-code/testing'

const BAND = {
  plugin: 'issue-approve',
  component: 'AbovePrompt',
  props: { hasSurvey: false, isWorking: false, rows: 10, columns: 100 },
} as const

const ENV: Record<string, string> = {
  ISSUE_VIEWER_ISSUE_SLUG: 'ui/foo',
  ISSUE_VIEWER_PROJECT: 'proj',
  ISSUE_VIEWER_URL: 'http://board:8080',
}

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
  on('session.start', (_, e) => ({ cwd: e.cwd }))

  await $.session.start({ cwd: '/', surface: 'terminal', isInteractive: true })

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
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  await $.session.start({ cwd: '/w/proj/.worktrees/ui/foo', surface: 'terminal', isInteractive: true })
  expect(urls).toEqual([
    'http://localhost:8080/project-for?cwd=%2Fw%2Fproj%2F.worktrees%2Fui%2Ffoo',
    'http://localhost:8080/p/proj/issue/ui/foo/approvals.json',
  ])
  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect((await ui.find({ key: 'in progress' }))?.text).toBe('☐ in progress')
  await ui.unmount()
})

test('at human-testing it offers Playtest, then Report, which applies the results', async ($, on) => {
  const runs: string[] = []
  const spawned: string[] = []
  on('env.get', (_, e) => ({ value: ENV[e.name] }))
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
    runs.push(e.argv.join(' '))
    return { value: { exitCode: 0, stdout: 'ticked TP6 TP10\n', stderr: '' } }
  })
  on('process.spawn', async function* ($, e, next) {
    void $
    void next
    spawned.push(e.argv.join(' '))
    yield { stream: 'stdout' as const, text: 'overlay open\n' }
    return { value: { code: 0, signal: null } }
  })
  on('session.start', (_, e) => ({ cwd: e.cwd }))
  await $.session.start({ cwd: '/w/proj/.worktrees/ui/foo', surface: 'terminal', isInteractive: true })
  expect(runs).toEqual(['make -n playtest ISSUE=ui/foo'])

  const ui = await $.ui.mount({ ...BAND, surface: 'terminal' })
  expect((await ui.find({ key: 'playtest' }))?.text).toBe('▶ Playtest')
  expect((await ui.find({ key: 'documentation' }))?.text).toBe('☐ documentation')
  await ui.press({ key: 'playtest' })
  expect(spawned).toEqual(['make playtest ISSUE=ui/foo'])
  expect(toasts[0]).toContain('Playtest finished')
  expect((await ui.find({ key: 'report' }))?.text).toBe('✓ Report')
  await ui.press({ key: 'report' })
  expect(runs[1]).toBe('make playtest-report ISSUE=ui/foo APPLY=1')
  expect((await ui.find({ key: 'playtest' }))?.text).toBe('▶ Playtest')
  await ui.unmount()
})
