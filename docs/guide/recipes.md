# Recipes

Complete flows to copy and adapt. Each one lists the nodes in order, with the
settings that matter; anything not mentioned keeps its default. Connect the
nodes from left to right unless a recipe says otherwise.

Build them the way [Getting started](getting-started.md#4-build-your-first-flow)
shows: add the node, fill in its settings, **Apply changes**, connect, **Save**,
**Deploy**.

- [Tell me when the Pi overheats](#tell-me-when-the-pi-overheats)
- [React to a webhook only when it matters](#react-to-a-webhook-only-when-it-matters)
- [Fetch something every morning and send it to Telegram](#fetch-something-every-morning-and-send-it-to-telegram)
- [Log a temperature sensor to a file](#log-a-temperature-sensor-to-a-file)
- [Switch a pin from a button](#switch-a-pin-from-a-button)
- [Keep a threshold in a variable](#keep-a-threshold-in-a-variable)
- [Avoid a flood of notifications](#avoid-a-flood-of-notifications)

## Tell me when the Pi overheats

Sends a push notification to your phone through [ntfy](https://ntfy.sh) when
the CPU passes 70 °C.

| # | Node | Settings |
|---|------|----------|
| 1 | **System Metric** | Metric `temperature` · Comparison `above` · Threshold `70` · Poll Interval `30` |
| 2 | **Template** | `Pi is at {{payload.value}} °C` |
| 3 | **Notify** | Format `ntfy` · Webhook URL `{{secret.ntfy_url}}` · Title `PiWorker` |

Before deploying, add a secret named `ntfy_url` under **Secrets** with the
value `https://ntfy.sh/<a-topic-name-only-you-know>`, and subscribe to the
same topic in the ntfy app.

System Metric fires **once** each time the value crosses the threshold, not on
every reading, so you get one notification per overheating episode. For
Discord or Slack, change the format and store that service's webhook URL in
the secret instead.

## React to a webhook only when it matters

Another service or device calls a URL on your Pi; the flow acts only on some
calls.

| # | Node | Settings |
|---|------|----------|
| 1 | **Webhook** | Path `temperature` · Secret Token `{{secret.hook_token}}` |
| 2 | **Switch (If/Else)** | Condition `payload.body.temperature > 30` |
| 3 | **Template** (from the Switch's **True** port) | `Hot: {{payload.body.temperature}} °C in {{payload.body.room}}` |
| 4 | **Notify** | as in the previous recipe |

Add the secret `hook_token` first (any long random text). Then call it:

```bash
curl -X POST "http://raspberrypi.local:8080/webhooks/temperature?token=YOUR_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"temperature": 31.5, "room": "office"}'
```

- The JSON you send arrives in `payload.body`; query parameters in
  `payload.query`.
- Port 8080 is plain HTTP, for devices that cannot check PiWorker's
  certificate; the token then travels unencrypted. Callers that can use HTTPS
  should call `https://<device>:8443/webhooks/temperature`.
- `404` means no deployed flow has that path; `401` means the token is missing
  or wrong.
- Connect the Switch's **False** port to something else if the other case
  also needs handling, or leave it unconnected to ignore it.

## Fetch something every morning and send it to Telegram

| # | Node | Settings |
|---|------|----------|
| 1 | **Cron Schedule** | Cron Expression `0 8 * * *` (every day at 8:00) |
| 2 | **HTTP Request** | Method `GET` · URL of a JSON API, for example `https://api.example.com/today` |
| 3 | **Template** | `Today: {{payload.summary}}` (use the fields your API returns) |
| 4 | **Telegram** | Bot Token `{{secret.telegram_token}}` · Chat ID your chat's ID |

After HTTP Request, the payload is the response: a JSON answer becomes an
object whose fields you reach as `payload.field`. To see what the API returns,
put a **Debug** node after HTTP Request first and trigger the flow with a
**Manual Inject** instead of waiting for the morning.

Cron expressions have five fields: minute, hour, day of month, month, day of
week. `*/15 * * * *` is every 15 minutes; `0 9 * * 1` is Mondays at 9:00.

Requests to addresses inside your own network (`192.168.…`, `localhost`) are
refused unless you turn on **Allow Private Addresses** in the HTTP Request
node.

## Log a temperature sensor to a file

Needs a DS18B20 sensor and 1-Wire enabled on the Pi. The DS18B20 node is
[experimental](troubleshooting.md#a-gpio-i2c-or-sensor-node-fails).

| # | Node | Settings |
|---|------|----------|
| 1 | **DS18B20 Temperature** | Poll interval `60` |
| 2 | **Template** | `{{timestamp}},{{payload.celsius}}` |
| 3 | **Write File** | File Path `/var/lib/piworker/temperature.csv` · Mode `append` |

Each reading adds a line such as `2026-11-11T08:00:00Z,21.4`. The file must be
in a directory the PiWorker service user can write to.

## Switch a pin from a button

A button on GPIO17 turns a relay on GPIO27 on while it is pressed. The GPIO
nodes are [experimental](troubleshooting.md#a-gpio-i2c-or-sensor-node-fails).

| # | Node | Settings |
|---|------|----------|
| 1 | **GPIO Edge** | Pin `GPIO17` · Edge `both` · Pull resistor `up` |
| 2 | **Transform** | `not payload.level` (`payload.level` is `true` when the pin reads high; with a pull-up, a pressed button reads low) |
| 3 | **GPIO Write** | Pin `GPIO27` · Value `payload` |

GPIO Write with the value `payload` sets the pin high when the payload is
true and low when it is false. The service user needs permission to use GPIO:
see [Troubleshooting](troubleshooting.md#a-gpio-i2c-or-sensor-node-fails).

## Keep a threshold in a variable

Change a limit without editing the flow.

1. Under **Variables**, create the global variable `threshold` with the JSON
   value `25`.
2. In any flow, use it in a **Filter**:

   ```
   payload.temperature > (vars["threshold"] ?? 25)
   ```

   `?? 25` is the value used while the variable does not exist.
3. Changing the variable takes effect on the next message. No redeploy.

To remember a value **from** a flow, add **Set Variable** (Variable Name
`lastTemperature`, Value Expression `payload.temperature`); read it elsewhere
as `vars["lastTemperature"]` or `{{vars.lastTemperature}}`.

## Avoid a flood of notifications

Put a **Rate Limit** node before the action: Limit `1`, Window `600000`
(10 minutes, in milliseconds) lets one message through per ten minutes and
drops the rest. To collect messages and send them together instead, use
**Aggregate** (Batch size `10`): it waits for ten messages and passes them on
as one list.
