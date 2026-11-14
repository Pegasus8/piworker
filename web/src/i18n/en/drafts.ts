// Agent drafts: the drafts list and review pages, their diff and alerts, the
// presentation rules of lib/drafts.ts and the agent marks of lib/flowMarks.ts.
type Change = 'added' | 'removed' | 'changed'

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

export default {
  // lib/drafts.ts: alert kinds flow.Diff raises.
  alertKinds: {
    risk_raised: {
      label: 'Risk raised',
      description: 'The flow as a whole can do more than before: its risk level is higher.'
    },
    privileged_node_added: {
      label: 'Privileged node',
      description: 'A node that can run commands, write files, query databases or reach the hardware now runs.'
    },
    destination_changed: {
      label: 'Destination changed',
      description: 'Where a node sends data or connects changed: a URL, host, broker, topic, recipient, chat or file path.'
    },
    dynamic_destination: {
      label: 'Dynamic destination',
      description: 'A destination is built from message data or variables, so where data goes can change while the flow runs.'
    },
    new_host: {
      label: 'New host',
      description: 'The flow contacts a host that the compared version never contacts.'
    },
    secret_added: {
      label: 'New secret used',
      description: 'The flow uses a secret that the compared version never uses.'
    },
    secret_moved: {
      label: 'Secret used elsewhere',
      description: 'A secret is used by a node or setting that did not use it before, so it may be sent somewhere new.'
    },
    risk_switch_on: {
      label: 'Risk switch turned on',
      description: 'An opt-in that widens what a node may do (such as private network access or dash arguments) is on.'
    },
    sensitive_literal: {
      label: 'Credential typed in',
      description: 'A credential setting holds a typed-in value instead of a {{secret.NAME}} reference.'
    },
    credentialed_request_changed: {
      label: 'Credential used differently',
      description: 'A node that uses a credential now sends a different request or gets new input, so the credential you approved is used for something else.'
    },
    rate_changed: {
      label: 'Rate changed',
      description: 'On a flow that reaches other services, how often a trigger fires or how many messages a limiter lets through changed.'
    },
    global_variable_write: {
      label: 'Global variable written',
      description: 'What the flow writes to a global variable may have changed. Other flows read global variables for their URLs, commands and conditions.'
    },
    global_variable_read: {
      label: 'Global variable read by a tool',
      description: 'A tool whose path changed reads global variables: what it reads goes back to the agent that calls it.'
    },
    tool_path_changed: {
      label: 'Tool changed',
      description: 'What a published tool runs changed: an agent calling it, under its grant and confirmation setting, would run something you did not approve.'
    },
    data_read_changed: {
      label: 'Protected data read',
      description: 'A new or changed node reads variables, the settings earlier nodes pass along, or what a credential returned: it could hand them back to an agent through the flow\'s runs.'
    }
  },
  unknownAlert: 'Alert',
  unknownAlertDescription: 'An alert this editor does not know yet: read its message.',
  nodeLabel: (id: string) => `Node ${id}`,
  wholeFlow: 'Whole flow',
  onSetting: (node: string, setting: string) => `${node} · setting ${setting}`,

  risk: {
    safe: 'Safe',
    external: 'External',
    privileged: 'Privileged',
    unknown: 'Unknown risk',
    raised: (before: string, after: string) => `Risk raised from ${before} to ${after}`,
    lowered: (before: string, after: string) => `Risk lowered from ${before} to ${after}`,
    same: (risk: string) => `Risk unchanged: ${risk}`
  },

  groups: { added: 'Added nodes', removed: 'Removed nodes', changed: 'Changed nodes' },
  nodesChanged: (n: number, change: Change) => `${plural(n, 'node')} ${change}`,
  connectionsAdded: (n: number) => `${plural(n, 'connection')} added`,
  connectionsRemoved: (n: number) => `${plural(n, 'connection')} removed`,
  noChanges: 'No node or connection changes',
  fieldEnabled: 'Enabled',
  fieldType: 'Node type',
  hidden: 'hidden',
  fieldName: 'Name',
  fieldDescription: 'Description',

  errors: (n: number) => plural(n, 'error'),
  warnings: (n: number) => plural(n, 'warning'),
  noIssues: 'No issues',
  severityError: 'Error',
  severityWarning: 'Warning',

  newFlowTitle: (name: string) => `New flow “${name}”`,
  flowTitle: (id: string) => `Flow ${id}`,

  // draftVersionText
  flowNowAt: (version: number) => `the flow is now at version ${version}`,
  flowDeleted: 'the flow was deleted',
  createsNewFlow: 'Creates a new flow',
  writtenFor: (version: number) => `Written for version ${version}`,
  changesCurrent: (version: number) => `Changes version ${version} (the current one)`,
  writtenAsNew: 'Written as a new flow',
  savedAsVersion: (version: number) => `saved as version ${version}`,

  // diffBasis
  noAlerts: 'No alerts: no destination, secret, credential or risk changes.',
  emptyHeading: 'Changes compared with an empty flow',
  emptyNoteNew: 'Compared with an empty flow: this draft creates a new flow.',
  emptyNoteDeleted: 'Compared with an empty flow: the flow this draft was written for no longer exists.',
  notBase: (version: number) => `, not version ${version} the draft was written for`,
  pendingHeading: (current: number) => `Changes compared with version ${current} (current)`,
  pendingNote: (current: number, notBase: string) => `Compared with the current version, ${current}${notBase}.`,
  decidedHeading: (current: number) => `Changes compared with version ${current} (current, recomputed now)`,
  savedAs: (version: number) => `This draft was saved as version ${version}`,
  wasApproved: 'This draft was approved',
  partOfIt: 'so its changes are already part of it and show here as none',
  partOfFlow: 'so its changes are already part of the flow',
  approvedNote: (current: number, notBase: string, saved: string, part: string) =>
    `Recomputed now against the current version, ${current}${notBase}. ${saved}, ${part}: what shows here is only what differs from the current version.`,
  noAlertsApplied: (current: number) => `No alerts against version ${current}: this draft's changes are already part of it.`,
  noAlertsCurrent: (current: number) => `No alerts against the current version, ${current}.`,
  otherNote: (current: number, notBase: string) =>
    `Recomputed now against the current version, ${current}${notBase}. Nothing from this draft was saved.`,

  // draftState
  state: {
    outOfDate: 'Out of date',
    outOfDateExplanation: (base: number, current: number) =>
      `The agent wrote this draft for version ${base}, but the flow is now at version ${current}. ` +
      'It cannot be approved: ask the agent to redo it on the current version, or reject it.',
    waiting: 'Waiting for review',
    waitingExplanation: 'Nothing changes until you approve it.',
    deployedByAgent: 'Deployed by agent',
    deployedByAgentExplanation: (token: string, version?: number) =>
      `Agent ${token} deployed this draft itself${version ? ` as version ${version}` : ''} (the agent deploy level allows it); you did not review it. ` +
      'The version history can roll it back to the version you last approved.',
    approved: 'Approved',
    approvedExplanation: (version?: number) =>
      version ? `You approved this draft; it was saved as version ${version}.` : 'You approved this draft.',
    rejected: 'Rejected',
    rejectedExplanation: 'You rejected this draft. The agent sees the reason.',
    void: 'Void',
    voidExplanation: 'This draft was closed without a decision (its token was revoked or its flow deleted). Nothing was saved.',
    unknownExplanation: 'This draft can no longer be approved.'
  },

  // approvalBlock
  statusWords: { approved: 'approved', rejected: 'rejected', decided: 'decided' },
  blockDeployedByAgent: 'This draft was already deployed by its agent; there is nothing to approve.',
  blockVoid: 'This draft is void: it was closed without a decision, so there is nothing to approve.',
  blockDecided: (word: string) => `This draft was already ${word}; there is nothing to approve.`,
  blockStale: (current: number) =>
    `This draft is out of date: the flow is now at version ${current}. Ask the agent to redo it, or reject it.`,
  blockErrors: (n: number) =>
    `The draft has ${plural(n, 'error')} (listed under Issues), so it cannot be saved. Ask the agent to fix them, or reject it.`,
  blockSecretsRunning: 'The flow is running, so approving redeploys it, but it uses secrets that are not defined (listed under Issues). Define them in Secrets first.',
  blockSecretsDeploy: 'Deploying needs secrets that are not defined (listed under Issues). Define them in Secrets first, or approve without deploying.',

  // approveConfirmText
  alertsList: (n: number, kinds: string) => `${plural(n, 'alert')}: ${kinds}`,
  noAlertsLower: 'no alerts',
  comparedBoth: (current: number, risk: string, alerts: string, last: number, sinceRisk: string, sinceAlerts: string) =>
    `Compared with the current version, ${current}: ${risk}; ${alerts}. ` +
    `Compared with version ${last}, the one you last approved: ${sinceRisk}; ${sinceAlerts}. ` +
    'Approving also approves these earlier agent changes.',
  createdFrom: (name: string, token: string) => `“${name}” is created as a new flow from this draft by “${token}”.`,
  savedFrom: (name: string, token: string) => `“${name}” is saved as a new version from this draft by “${token}”.`,
  renames: (before: string, after: string) => ` It renames the flow from “${before}” to “${after}”.`,
  describes: ' It changes the flow\'s description.',
  runsRedeploy: 'The flow is running, so it is redeployed with this version.',
  runsDeploy: 'It is deployed right away.',
  runsNot: 'It is not deployed.',

  // applyOutcome
  outcomeDeployError: (action: string, version: number, message: string) =>
    `${action}: saved as version ${version}, but it could not be deployed: ${message}`,
  outcomeDeployed: (action: string, version: number) => `${action}: saved as version ${version} and deployed.`,
  outcomeSaved: (action: string, version: number) => `${action}: saved as version ${version}. It is not running.`,

  // lib/flowMarks.ts
  marks: {
    deployedBy: (running: boolean | undefined, name: string) =>
      `${running ? 'Deployed' : 'Last deployed'} by ${name ? `agent ${name}` : 'an agent'}`,
    deployedTitle: (version: number, name: string, when: string, now?: number) =>
      `Version ${version} was deployed by ${name ? `the agent token “${name}”` : 'an agent token'}${when ? ` on ${when}` : ''}.` +
      (now ? ` The flow is now at version ${now}.` : ''),
    changedByAgent: 'Changed by agent',
    changedTitle: 'The current version was saved by an agent and not approved by you. Open the version history to roll back to the version you last approved.',
    you: 'You (editor)',
    agent: (name: string) => `Agent ${name}`,
    anAgent: 'An agent',
    current: 'Current',
    deployed: 'Deployed',
    notApproved: 'Not approved by you',
    lastApproved: 'Last approved by you',
    fromDraft: 'From a draft',
    noRollback: 'There is no version you approved to roll back to (no version you approved is kept).',
    nothingToRollBack: 'The current version is the one you last approved: there is nothing to roll back.',
    rollbackConfirm: (name: string, last: number, current: number, running: boolean) =>
      `“${name}” goes back to version ${last}, the version you last approved: it is saved again as a new version, after version ${current}. Later versions stay in the history.` +
      (running ? ' The flow is running, so it is redeployed with that version.' : ' The flow is not running; nothing is deployed.')
  },

  // stores/drafts.ts
  approveFailed: 'Could not approve the draft. Try again.',
  rejectFailed: 'Could not reject the draft. Try again.',

  // DraftsView
  list: {
    title: 'Drafts',
    intro: 'Flows and changes agents propose. Nothing is saved or runs until you approve it; review the alerts and the changes first.',
    filterWaiting: 'Waiting',
    filterDecided: 'Decided',
    filterAll: 'All',
    show: 'Show',
    statusLabel: 'Draft status',
    refreshLabel: 'Refresh drafts',
    refresh: 'Refresh',
    outdated: 'Drafts changed since this list loaded. Refreshing shows them, starting again from the newest.',
    loading: 'Loading drafts…',
    loadFailed: 'Could not load the drafts.',
    loadMoreFailed: 'Could not load more drafts.',
    empty: 'No drafts to show.',
    emptyAuthorOff: 'No drafts are waiting for you. Agent authoring is turned off (mcp.author_enabled), so agents cannot propose flows.',
    emptyWaiting: 'No drafts are waiting for you. Agents with the author scope can propose flows here.',
    articleLabel: (name: string) => `Draft ${name}`,
    reviewLabel: (isNew: boolean, name: string) => `Review the draft for ${isNew ? 'the new flow ' : ''}${name}`,
    alerts: (n: number) => plural(n, 'alert'),
    newFlow: 'New flow',
    riskPrefix: 'Risk',
    token: 'Token',
    written: 'Written',
    version: 'Version',
    decided: 'Decided',
    note: 'Note written by the agent (shown as text)',
    review: 'Review…',
    view: 'View',
    loadMore: 'Load more'
  },

  // DraftAlerts and DraftDiff
  before: 'Before',
  after: 'After',
  valuesInDiff: 'The values are in the node\'s changes below.',
  unchangedValue: 'Value (the setting itself did not change)',
  valueNotShown: 'This setting\'s value is not shown here.',
  change: { added: 'Added', removed: 'Removed', changed: 'Changed' },
  nodeAria: (change: string, type: string, id: string) => `${change} node ${type} ${id}`,
  moved: 'Moved on the canvas.',
  fieldsCaption: (id: string) => `Changed fields of node ${id}`,
  field: 'Field',
  destination: 'Destination',
  credential: 'Credential',
  notSet: '(not set)',
  removed: '(removed)',
  sensitiveTitle: 'Sensitive value: never shown',
  connections: 'Connections',
  connectionsAria: (label: string) => `${label}: connections`,

  // DraftReviewView
  review: {
    allDrafts: 'All drafts',
    openInEditor: 'Open the flow in the editor',
    loading: 'Loading the draft…',
    notFound: 'This draft does not exist. Decided drafts are kept for 30 days.',
    loadFailed: 'Could not load the draft.',
    retry: 'Retry',
    headingNew: 'Draft of a new flow: ',
    headingFor: 'Draft for ',
    writtenBy: 'Written by the agent token',
    written: 'Written',
    version: 'Version',
    flow: 'Flow',
    running: 'Running',
    notRunning: 'Not running',
    decided: 'Decided',
    reason: 'Reason: ',
    openFlow: 'Open the flow',
    goToDecision: 'Go to the decision',
    noteHeading: 'Note written by the agent',
    noteShown: (hidden: number) =>
      `Shown as text, exactly as the agent sent it${hidden ? `; ${hidden} hidden character(s) are highlighted as \\u escapes` : ''}.`,
    risk: 'Risk',
    riskBefore: 'Before',
    riskAfter: 'After',
    alerts: (n: number) => `Alerts (${n})`,
    issues: (counts: string) => `Issues: ${counts}`,
    noIssues: 'The draft passes the same checks deploy runs.',
    changesHelp: 'Credentials are never shown; settings marked Destination say where the flow sends data or connects.',
    nameAndDescription: 'Name and description',
    field: 'Field',
    before: 'Before',
    after: 'After',
    empty: '(empty)',
    changesLabel: 'Changes',
    approvedHeading: (version: number) => `Compared with the version you last approved (version ${version})`,
    approvedHelp: 'The current version was saved by an agent, not approved by you. These are the changes since the version you approved: what an agent changed before this draft is included.',
    approvedPrefix: 'Approved',
    withDraftPrefix: 'With this draft',
    noAlertsApproved: 'No alerts compared with the version you approved.',
    sinceApproved: 'Since the version you approved',
    proposedFlow: 'Proposed flow (JSON as the agent wrote it; credentials hidden)',
    decision: 'Decision',
    deployNow: 'Deploy now',
    deployRunning: 'The flow is running, so approving always redeploys it with the new version.',
    deployHelp: 'Start the flow with the new version right after saving it. Otherwise it is saved and stays stopped.',
    approveDeploy: 'Approve and deploy…',
    approveSave: 'Approve and save…',
    reasonLabel: 'Reason for rejecting (optional, shown to the agent)',
    reasonPlaceholder: 'What the agent should change',
    characters: (n: number, max: number) => `${n} / ${max} characters`,
    reject: 'Reject…',
    // Approve and reject
    noteHiddenWarning: (n: number) => ` The agent's note contains ${n} hidden character(s), shown highlighted as \\u escapes.`,
    approveTitle: 'Approve this draft?',
    confirmDeploy: 'Approve and deploy',
    confirmSave: 'Approve and save',
    savingDeploying: 'Saving and deploying…',
    saving: 'Saving…',
    approvedAction: 'Approved',
    notApproved: (message: string) => `Not approved: ${message}`,
    rejectTitle: 'Reject this draft?',
    rejectText: (token: string, withReason: boolean) =>
      `Nothing is saved. The agent “${token}” is told the draft was rejected${withReason ? ', with your reason' : ''}.`,
    rejectConfirm: 'Reject draft',
    rejecting: 'Rejecting…',
    rejected: 'Draft rejected. The agent sees it the next time it checks the draft.',
    notRejected: (message: string) => `Not rejected: ${message}`,
    tryAgain: 'try again'
  }
}
