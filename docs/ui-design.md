# PiWorker UI conventions

## Personality

A friendly automation laboratory: technically clear, calm enough for monitoring,
with playful details in color and motion. Interface copy stays in English.
Use direct action labels and concrete status text; reserve personality for
introductory and empty-state copy, never security warnings or errors.

## Visual foundation

The dark theme uses graphite surfaces, mint primary actions, and progressively
lighter borders and panels. Tokens live in `web/src/assets/main.css`; components
consume semantic Tailwind colors rather than introducing individual hex values.
Amber, blue, and purple continue to identify trigger, processing, and action nodes.
Status must also be written in text, never communicated by color alone.

Use the shared Button, Input, Card, Badge, and Dialog components. Rounded corners
use the shared radius. Page titles establish hierarchy; supporting copy is muted
but readable. Use Lucide icons already installed in the project. Keep primary
calls to action visually prominent and destructive actions explicitly labelled.

## Interaction

- Flow card actions remain visible for mouse, touch, and keyboard users.
- Search distinguishes an empty installation from no matching results and offers
  a clear-search action.
- Dialog uses the native modal element for focus containment, Escape dismissal,
  and returning focus to the invoking control. Supply a descriptive `title`.
  Content scrolls within the viewport. Do not use it for nonmodal side panels.
- Controls have a visible focus ring; icon-only actions need accessible names.
- Headers wrap when space is limited; the editor canvas must keep its available
  height. On narrow screens, the node palette opens on demand and the minimap
  is hidden so the canvas keeps the full available width.
- Press feedback takes 160 ms, page entrance 240 ms, dialog entrance 180 ms.
  Motion is decorative, never required to understand state. Respect
  `prefers-reduced-motion`; do not animate canvas node positioning or use
  perpetual decorative animation.

## Confirmations and operation feedback

Use `ConfirmDialog` for deleting flows, nodes, connections, or secrets and for
leaving unsaved edits. Name the affected item and explain the consequence.
Focus Cancel (or Keep editing) by default. Escape and backdrop clicks cancel;
clicking the dialog padding does not dismiss it. Nested confirmations preserve
the underlying form. Browser reload/close still uses the browser's native
unsaved-changes warning because custom dialogs cannot block tab closure.

Use `OperationFeedback` and `useFeedback` for flow and secret mutations. Pending
operations prevent repeated submissions. Errors remain visible, preserve user
input, and suggest a next action. Success and errors can be dismissed explicitly;
no short timer removes a message before it can be read. Feedback uses accessible
status/alert roles. Never include secret values in messages. Authentication keeps
its existing inline form validation and session-expiry behavior.

Saving must succeed before deploying. On a save failure, preserve the draft and
allow retry; do not attempt deployment. Editor controls are inert while the flow
is loading or an operation is pending, so edits cannot race a save response.

## Mobile and keyboard editing

Node entries are buttons: tap, click, or activate with the keyboard to add a node
and open configuration. Desktop drag-and-drop is also supported. Added nodes use
a grid placement and fit the viewport. Configuration is a scrollable modal, and
Apply changes updates the draft; Save persists the complete flow.

The Connections dialog is available at every viewport size. It lists eligible
source/output and destination/input nodes, prevents self/duplicate connections,
and supports explicit removal. Connections follow the existing single
output/input persistence model; this is not a new per-port routing feature.
Node deletion also removes its connections. Panning, zooming, selection and
position dragging continue to use the canvas controls.

## Validation

Browser regressions cover search reset, modal keyboard behavior, narrow navigation,
reduced motion, HTTP warnings, authentication, deletion cancellation and retry,
failed-save deployment prevention, unsaved navigation, mobile node configuration
and persisted connections, connection removal, and nested secret confirmations.
Desktop and mobile layouts also receive visual review. This is not a claim of a
complete accessibility certification or testing on every physical mobile device.
HTTPS remains a separate infrastructure follow-up; the HTTP warning is preserved.

## Node payload playground

Processing nodes offer a payload playground alongside configuration. Expressions,
conditions and templates use multiline fields, persistent field descriptions and
explicit replacement examples. At wide viewports, configuration, input and output
can be inspected together; narrow screens stack them in a scrollable modal.

Input format is explicit: JSON validates before submission (including scalar
values and null), while Text sends the exact string. Invalid JSON never silently
becomes text. Topic and object-valued metadata are optional message context and
are passed to the server test endpoint. Test runs use the draft configuration,
without applying it to the flow first. Each request creates a fresh node instance;
stateful/buffering nodes may emit nothing in a one-message test.

Output shows message count, elapsed time, payload type and port. Expandable fields
show the payload; Raw message retains the complete message envelope. The tree
limits entries/depth for readability, with the complete data still available in
the raw view. Use as input copies only that output's payload to the sample input.
No-output results are successful outcomes, distinct from test failures.

Editing input, context or configuration marks previous results as out of date.
Cancellation and closing the node abort the request; late responses cannot replace
the current node's output. Sample drafts are kept per node in component memory
until leaving the editor, and are not saved in flow JSON or browser storage.
Server execution and logging follow the existing node behavior.

## Core editing and execution conventions

- Ports come from node metadata, not category-based assumptions. Handles have
  explicit IDs; the Connections dialog selects both nodes and ports. Duplicate
  connections are checked per pair of ports. Legacy invalid connections are
  visible and block deployment until explicitly repaired.
- Flow status describes execution. Deploy/Stop and the list switch control the
  same operation; saving is not an activation preference. Disabled nodes remain
  disabled when a definition is edited.
- Run manually is available for enabled Manual Inject nodes in a deployed flow
  with no unsaved changes. The dialog distinguishes saved payload from JSON/text
  overrides and explains real side effects and the `payload.data` envelope.
  Invalid JSON blocks sending; submission has pending, success and retry states.
- History loads independently of the live connection. Run details identify the
  recorded node, phase, duration and error. Loading, empty and failed requests
  are distinct; late responses cannot overwrite another selected run.

Regression coverage includes branch round trips, duplicate ports, invalid legacy
connections, runtime status, manual input validation/retry, stopped history and
late responses. A browser test against an isolated Go server also saves and
reloads a branched flow, executes both branches and opens a recorded error after
stopping. Backend tests cover old definitions without port caches, restoration
selection, failed lifecycle persistence, unknown-length injection bodies and
persisting node details before exposing run completion.

## Variables and credential references

The Variables manager is available in the flow list and editor. Values have an
explicit Text/JSON format, preserving numbers, booleans, composites and null.
Replacing or deleting a global value requires confirmation because all flows
share it. A failed save preserves the form and the previous stored value.

Compatible node fields offer filtered suggestions while typing, an Insert
reference button and Ctrl+Space. Arrow keys select, Enter/Tab inserts, and Escape
closes suggestions before dismissing the node dialog. Suggestions insert at the
caret and never send secret values to the browser. Get/Set Variable suggests bare
names; expressions insert bracket notation to support names containing hyphens or
dots; templates insert their own placeholder syntax. Unsupported fields have no
reference affordance. Loading failures offer a retry without discarding edits.

Names include existing global variables and Set Variable names declared in the
current draft. A declared name may not have a stored value until its node runs.
Node previews use isolated variable copies; testing Set Variable does not update
live state. Variables are not credentials and may appear in payloads and Debug.
