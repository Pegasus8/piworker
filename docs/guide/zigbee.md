# Zigbee devices

PiWorker automates Zigbee sensors, plugs, lights and buttons through
[Zigbee2MQTT](https://www.zigbee2mqtt.io/), which drives a USB Zigbee
coordinator and publishes every device on an MQTT broker. PiWorker connects to
that broker; it works the same on a Raspberry Pi and on an x86_64 PC.

```
Zigbee devices ─── coordinator (USB) ─── Zigbee2MQTT ─── MQTT broker ─── PiWorker
```

## 1. Set up Zigbee2MQTT and a broker

If you already run Zigbee2MQTT and Mosquitto, skip to step 2.

Otherwise, the repository's `docker-compose.zigbee.yml` starts all three
(Mosquitto, Zigbee2MQTT and PiWorker) on one machine:

```bash
ls /dev/serial/by-id/          # find the coordinator's stable path
ZIGBEE_ADAPTER=/dev/serial/by-id/usb-ITead_Sonoff_Zigbee_3.0_USB_Dongle_Plus_...-if00-port0 \
ZIGBEE_ADAPTER_TYPE=ember \
docker compose -f docker-compose.zigbee.yml up -d
```

`ZIGBEE_ADAPTER_TYPE` is `ember` for the SONOFF ZBDongle-E, `zstack` for the
ZBDongle-P and other CC2652 sticks, `deconz` for a ConBee. The compose file turns
on Zigbee2MQTT's availability reports, which PiWorker uses to tell you when a
device goes offline.

Plug the coordinator into a USB 2 port or use a short USB extension cable: USB 3
ports interfere with Zigbee's 2.4 GHz radio, on a Pi and on a PC alike.

## 2. Add a connection

In the flow list, open **Connections** and add one:

| Setting | Example |
|---------|---------|
| Name | `home` (flows refer to it by this name) |
| Broker | `tcp://192.168.1.10:1883` (`tcp://mosquitto:1883` with the compose file) |
| Username / password | the broker's, if it asks for one; `{{secret.NAME}}` works too |
| Base topic | `zigbee2mqtt` (Zigbee2MQTT's default) |

**Test connection** tries it without saving. The password is never shown
again; leave it empty when editing to keep it.

## 3. Pair your devices

Open the **Zigbee** page, choose **Allow joining** (four minutes), and put the
device in pairing mode. For SONOFF devices this is usually holding the button
for about five seconds until the LED blinks; see the device's manual. The device
appears in the list once Zigbee2MQTT has interviewed it.

Give devices clear names in Zigbee2MQTT (its web interface on port 8081 with the
compose file), for example `bedroom_climate` or `living/plug`. Flows use these
names: rename a device and the flows that name it stop matching it.

The Zigbee page shows, per device, whether it is online, when it last reported
and its current values (temperature, humidity, battery, state, power, energy…).

## 4. Build flows

| Node | What it does |
|------|--------------|
| **Zigbee Device** (trigger) | Fires when a device reports a change: its whole state, or one property (`temperature`, `humidity`, `state`, `power`, `action`…) changing by at least a *minimum change*. Its second output fires when the device goes offline or comes back. |
| **Zigbee Set** (action) | Turns a device on, off or toggles it, sets one property (`brightness`, `power_on_behavior`…) or sends a JSON object. The device may come from the message (`{{payload.device}}`). |
| **Zigbee Get** (processing) | Asks a mains-powered device for a fresh value and passes it on. Battery sensors sleep and never answer. |
| **Timer** (processing) | Fires once no message came for a while; each message restarts it. |
| **Sun** (trigger) | Fires at sunrise, sunset, dawn or dusk, with an offset. |

Pick the connection, the device and the property from the suggestions in each
node's settings: PiWorker reads them from Zigbee2MQTT. **Examples** in the flow
list creates the recipes below with your connection and device names.

Expressions (Filter, Switch, Transform, Set Variable) can read any device's last
report: `zigbee("bedroom_climate").humidity`, or
`zigbee("home", "bedroom_climate")` to name the connection. It is `nil` until the
device has reported since PiWorker started, and always `nil` in a dry-run.

### Example devices

- **SONOFF SNZB-02P** (temperature and humidity, battery): publishes
  `temperature` (°C), `humidity` (%) and `battery` (%) when they change. Use a
  Zigbee Device trigger with property `humidity` and minimum change `1` to react
  to each 1 % step. It cannot answer Zigbee Get.
- **SONOFF S60ZBTPF** (plug with power metering): `state` (`ON`/`OFF`, settable),
  `power` (W), `voltage` (V), `current` (A), `energy` (kWh) and settings such as
  `power_on_behavior`. Zigbee Set *on*/*off*/*toggle* switches it; Zigbee Get
  reads its power on demand. Set `power_on_behavior` to `previous` once, so the
  plug returns to its state after a power cut.

## Recipes

**Humidity controls a dehumidifier.** Zigbee Device (`bedroom_climate`,
property `humidity`, minimum change `1`) → Filter `payload.value >= 65` → Zigbee
Set `on`; and from the same trigger → Filter `payload.value <= 55` → Zigbee Set
`off`. The gap between 65 and 55 keeps the plug from flapping.

**The washing machine finished.** Zigbee Device (`laundry/plug`, property `power`,
minimum change `5`) → Filter `payload.previous != nil && payload.previous > 50 &&
payload.value < 5` → Telegram or Notify "Washing done".

**A sensor went offline.** Zigbee Device (`bedroom_climate`), *Availability*
output → Filter `payload.online == false` → Notify. (Battery sensors are marked
offline by Zigbee2MQTT after about 25 hours without a report.)

**Porch light at sunset.** Sun (your latitude and longitude, sunset, offset
`-15`) → Zigbee Set `on`; Sun (sunrise) → Zigbee Set `off`.

**Turn the heater off after 30 minutes.** Zigbee Device (plug, property `state`)
→ Filter `payload.value == "ON"` → Timer 30 minutes (restart off) → Zigbee Set
`off`.

**Daily low-battery check.** Cron `0 9 * * *` → Filter
`zigbee("bedroom_climate") != nil && zigbee("bedroom_climate").battery < 20` →
Notify "Change the sensor's battery".

## Troubleshooting

- **Deploy refuses the flow: the connection is not defined.** The node names a
  connection that does not exist (or was renamed). Create it under Connections
  or choose another in the node.
- **Nothing fires.** Check the Zigbee page: the broker must show *connected* and
  the bridge *online*, and the device name in the node must match Zigbee2MQTT's
  exactly (case and `/` included).
- **The connection changed but a running flow still uses the old broker.**
  Running flows keep their connection; stop and deploy them again.
- **Zigbee Get always times out.** The device sleeps (battery powered); use a
  Zigbee Device trigger instead.
- **Availability never fires.** Turn on availability in Zigbee2MQTT
  (`availability: enabled: true`).
- **AI agents.** Agents see connection and device names but never the broker's
  credentials, and cannot open the network to new devices. At the `low` agent
  deploy level they may change what an approved flow sends to an approved
  device (for example *on* into *off*); keep commands that matter in agent tools
  with confirmation `admin`. See [AI agents](agents.md).
