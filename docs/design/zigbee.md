# Zigbee home automation — analysis and proposal

Status: proposed (2026-11-11). Nothing here is implemented yet. Each phase
below lists whether it follows the "new node" recipe (no ADR) or changes a
contract (ADR in the same change, see [docs/adr/README.md](../adr/README.md)).

## 1. Goal

Make PiWorker a good automation brain for a Zigbee home: react to sensors,
buttons and device availability, and command lights, plugs, covers and
thermostats, from flows built in the editor and, within the existing agent
policy, from MCP tools. It must work the same on a Raspberry Pi and on an
x86_64 mini PC, the two usual hosts for a USB Zigbee coordinator.

Non-goals: being a device dashboard (Home Assistant's job), pairing UX beyond
a permit-join switch, OTA firmware updates, and a Zigbee stack of our own.

## 2. How PiWorker talks to Zigbee

| Option | What it is | Verdict |
|---|---|---|
| **Zigbee2MQTT (Z2M)** | Separate service driving the USB coordinator (Sonoff ZBDongle-P/E, SLZB, ConBee, …); exposes every device on an MQTT broker as JSON | **Recommended.** PiWorker already has MQTT nodes with reconnect, secrets and the `low` deploy markers; ~4,000 supported devices; runs on arm/arm64/amd64. |
| deCONZ / Phoscon REST + WebSocket | Vendor stack for ConBee/RaspBee | Possible later as a second backend; smaller device list, ConBee only. |
| ZHA | Home Assistant integration | Not reachable outside Home Assistant. |
| Native stack in Go (EZSP/ZNP over serial) | PiWorker owns the coordinator | **Rejected.** No mature Go stack; device quirks are the bulk of the work Z2M already does; exclusive serial port. |

So the integration surface is Z2M's MQTT API (base topic `zigbee2mqtt`):

| Topic | Direction | Content |
|---|---|---|
| `<base>/<device>` | Z2M → | Full state JSON on every report (`{"occupancy":true,"battery":87,…}`) |
| `<base>/<device>/set` | → Z2M | Command JSON (`{"state":"ON","brightness":120,"transition":2}`) |
| `<base>/<device>/get` | → Z2M | Ask for a fresh read (`{"state":""}`) |
| `<base>/<device>/availability` | Z2M → | `{"state":"online"}` / `offline` (retained) |
| `<base>/bridge/devices` | Z2M → | Retained list of devices with their `exposes` (features, access bits, value ranges) |
| `<base>/bridge/state` | Z2M → | Bridge `online` / `offline` |
| `<base>/bridge/request/…` | → Z2M | Admin requests (permit join, rename, …) answered on `bridge/response/…` |

Groups use the same `<base>/<group>/set` form; scenes are `scene_store` /
`scene_recall` commands.

What is possible **today** with `trigger-mqtt` + `process-filter`/`switch` +
`action-mqtt`: everything, but by hand. The user must know the topic layout,
write JSON payload templates, and filter repeated reports themselves (Z2M
republishes the whole state on every attribute change, so "motion detected"
arrives again with every battery or illuminance report).

## 3. Findings in the current code that shape the plan

1. **One MQTT client per node.** Each `trigger-mqtt`/`action-mqtt` opens its
   own connection (`newMqttClientOptions`). A home with 20 devices and two
   nodes each holds 40 connections to the broker. Fine for a few flows, wasteful
   for a house.
2. **No "on change" semantics.** The MQTT trigger fires per publication;
   deduplication needs a `set-var`/`get-var` pair and an expression per
   property.
3. **No retriggerable timer.** The most common home-automation pattern —
   *turn the light off N minutes after the last motion* — needs a timer that
   restarts on every event. `process-delay` holds each message (max 5 min)
   and cannot be cancelled or restarted.
4. **No sun/time-window conditions.** `trigger-cron` fires at fixed times;
   there is no sunrise/sunset trigger and no "is it night?" helper for
   filters.
5. **Agent node-type listing is full.** `pw_list_node_types` must fit all
   builtin types in 64 KiB (`maxNodeTypeListBytes`,
   `TestListNodeTypesOfTheBuiltins`); the registry uses ~65.3 KB of it, about
   200 bytes spare. Any new node with documentation breaks that test. This
   must be solved before adding Zigbee nodes.
6. **The agent policy already anticipates Zigbee.** ADR-0022 and
   `internal/flow/diff_vars.go` use `zigbee2mqtt/{{payload.device}}/set` as the
   example of a dynamic destination that `agent_deploy = low` refuses to widen.
   Device names as `Destination` and the command as `Operation` fit ADR-0019
   unchanged.

## 4. Proposed features, by phase

### Phase 0 — prerequisites (contract changes, ADRs)

- **0a. Node-type listing budget** (MCP contract → ADR). Options: list
  documentation only when a `type` or `category` filter is given (agents
  already can ask for one type), or drop the "all builtins fit" requirement
  and rely on the existing `documentationOmitted` path. Recommended: the
  first — the unfiltered list keeps schemas and markers, which is what
  authoring needs.
- **0b. Shared MQTT connections** (runtime/resource semantics → ADR). A
  process-wide pool keyed by (broker URL, username, password reference) with
  reference counting, one subscription router per client, released through
  `node.Closer` (ADR-0008). Nodes keep their per-node settings; a node with an
  explicit `clientId` keeps a dedicated client (brokers use it for ACLs and
  persistent sessions). Alternative considered: a named "broker connection"
  entity configured once in Settings and referenced by nodes — nicer UX
  (credentials typed once) but a new storage + REST contract; can come later
  on top of the pool.

### Phase 1 — Zigbee nodes (follow the "new node" recipe)

All take `broker`, credentials and `baseTopic` (default `zigbee2mqtt`) like the
MQTT nodes, so they work with any Z2M install.

| Node | Type id | What it does | Risk / markers |
|---|---|---|---|
| Zigbee Device | `trigger-zigbee` | Subscribes `<base>/<device>`. Settings: `device` (friendly name, `Destination`), `property` (optional: `occupancy`, `contact`, `action`, `temperature`…), `mode` = `change` (default: only when the property's value changes) / `every`. Output `{device, property, value, previous, state}`. `action` events (buttons, remotes: `single`, `double`, `hold`) always fire. | `safe` |
| Zigbee Availability | `trigger-zigbee` mode `availability`, or its own node | Fires on `online`↔`offline` of a device (dead battery, unplugged) or of the bridge. | `safe` |
| Zigbee Set | `action-zigbee` | Publishes to `<base>/<device or group>/set`. `command` preset (`Operation`): `on`, `off`, `toggle`, `brightness`, `color_temp`, `color`, `position` (covers), `setpoint` (TRVs), `scene_recall`, `raw` (JSON template). Value and `transition` support `{{templates}}`. | `external`; `device` is `Destination` |
| Zigbee Get | `process-zigbee-get` | Publishes `/get` and waits (bounded) for the next state on `<base>/<device>`; passes the fresh state downstream. Useful before acting on a stale value. | `external` |

Plus two generic processing nodes the Zigbee flows need (useful beyond Zigbee):

| Node | Type id | What it does |
|---|---|---|
| Timer (retriggerable) | `process-timer` | Starts on a message, restarts on each new one; emits on its `elapsed` output when the duration (minutes/hours, not the 5-minute cap of Delay) passes without new input; a `stop` input/`payload` cancels. *Motion → light on; timer 3 min → light off.* |
| Sun | `trigger-sun` | Fires at sunrise/sunset/dusk/dawn ± offset for configured latitude/longitude (computed locally, no network). Expression helpers `isDaylight()` / `isBetween("22:00","06:30")` for filters, added through `expr_guard.go`. |

Each node ships with tests against the in-process fake broker that the MQTT
tests already use (`mqtt_fakebroker_test.go`), recipes in
`docs/guide/recipes.md`, and the regenerated node reference.

### Phase 2 — discovery in the editor (REST contract → ADR)

- Backend subscribes once per configured broker to `<base>/bridge/devices`
  (retained) and `<base>/bridge/state`; `GET /api/zigbee/devices` returns
  friendly names, model, vendor, power source and the `exposes` list with
  access bits (published / settable / gettable).
- The config form gains an "options from endpoint" hint for `device` and
  `property`, so the user picks *Kitchen light → brightness (0–254)* instead
  of typing names. Only settable features are offered in Zigbee Set.
- Bridge status (online, coordinator, permit join on/off) on a Zigbee page in
  Settings; a
  **permit join** button for 2–4 minutes, admin only, never an agent tool.

### Phase 3 — house-level conveniences (later, optional)

- Device state cache: last known state of every device readable from
  expressions (`zigbee("door").contact`) and by `get-var`-style nodes, so
  conditions such as *only if the window is closed* need no extra flow.
- Low-battery report: a built-in recipe or flow template that collects
  `battery` below a threshold and notifies once per day.
- Importable example flows (motion light, door left open, leak alarm,
  heating schedule, button scenes) — ties into the existing TODO item
  "Flujos de ejemplo importables".
- Agent tools: example flows that publish a device as an MCP tool
  (`trigger-tool → action-zigbee → action-reply`) with `confirm: admin` for
  locks, valves and covers.

## 5. Deployment on Raspberry Pi and x86_64

Z2M, Mosquitto and PiWorker all run on arm, arm64 and amd64. The coordinator
is a USB serial device: pass it by its stable path
(`/dev/serial/by-id/usb-…`), and give Z2M (not PiWorker) the `dialout`
group. A `docker-compose.zigbee.yml` example (Mosquitto + Zigbee2MQTT +
PiWorker on one host network) belongs with Phase 1. Ethernet/Wi-Fi
coordinators (SLZB-06, …) avoid USB passthrough altogether, which is the
easier option in virtual machines on x86_64 hosts.

Keep the coordinator away from USB 3 ports and use a short extension cable:
USB 3 interferes with 2.4 GHz Zigbee on both Pis and PCs.

## 6. Open questions

1. Phase 0b: pool only, or also the named broker connection in Settings?
2. Should Zigbee Set refuse commands to `<base>/bridge/…` (renames, removal,
   permit join) outright? Recommended: yes — those are admin operations.
3. Is deCONZ support wanted, or is Z2M the only backend?
4. Which devices does the owner have? The first recipes and hardware
   validation should cover them.
