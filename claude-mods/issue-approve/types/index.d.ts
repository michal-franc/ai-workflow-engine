/** One approval the issue's current status offers (GET …/approvals.json). */
export type ApprovalItem = { status: string; approved: boolean; optional?: boolean }

/** GET /p/<project>/issue/<slug>/approvals.json on the AI Workflow Engine board. */
export type Approvals = {
  slug: string
  title: string
  status: string
  human_approval: string
  approvals: ApprovalItem[]
}

/** Where the Playtest button is in its cycle. */
export type PlaytestStep = 'idle' | 'opening' | 'ran' | 'reporting'

/** A lead's /exit: idle → writing (the handoff turn runs) → written (the next /exit leaves). */
export type HandoffStep = 'idle' | 'writing' | 'written'

/** How Playtest opens its tmux window: the project's scripts/playtest-window, the mod's own on `make playtest`, or no playtest. */
export type PlaytestLauncher = '' | 'script' | 'make'

declare module 'claude-code' {
  interface PluginState {
    'issue-approve': {
      /** The session's issue approvals; null outside a dispatched agent or when the board is down. */
      approvals: Approvals | null
      /** The status whose approval is being sent, '' when idle. */
      sending: string
      /** How Playtest opens its window (checked once at start); '' when the worktree has no playtest. */
      playtestLauncher: PlaytestLauncher
      /** The worktree's Makefile has a `playtest-report` target, so Report is offered after Playtest. */
      hasReport: boolean
      /** The issue has guided check scripts (`issue-cli check-script <slug> --list`, at start and after each turn). */
      hasChecks: boolean
      /** The lead's handoff before /exit (lead sessions only). */
      handoff: HandoffStep
      /** The playtest button's step: idle → opening → ran (window open, offer Report) → reporting → idle. */
      playtest: PlaytestStep
    }
  }
}
