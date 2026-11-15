# Troubleshooting

Find the symptom, try the fix. For problems with installing, updating or
backups of a `piworkerctl` installation, see the
[operations guide](../operations.md#troubleshooting).

**First step for almost everything: read the log.**

| Installed with | Command |
|----------------|---------|
| `piworkerctl` | `sudo journalctl -u piworker -n 100 --no-pager` |
| Docker | `docker logs --tail 100 piworker` |
| A binary you started | The terminal where it runs; add `-debug` for more detail |

## Getting in

### I cannot open PiWorker

- Use the HTTPS address with the port: `https://<device>:8443`.
  `http://<device>:8080` redirects there.
- Check that it is running: `systemctl status piworker` or `docker ps`.
- `raspberrypi.local` does not resolve on every network. Try the device's IP
  address instead (`hostname -I` on the device shows it).
- If the log says a port is in use, another program has 8443 or 8080. Change
  `addr` / `http_addr` in the [configuration](../../README.md#configuration).

### The browser warns about the certificate

Expected until the device trusts PiWorker's certificate authority. Open
**Trust this device** (`/trust`), compare the fingerprint with the one printed
by the installer or the log, and install the certificate. Once a device trusts
it, a warning means something is wrong: do not continue past it.

### "403" and a message about the host name

PiWorker only answers to names it knows as its own: `localhost`, its host
name, `<hostname>.local` and IP addresses. If you reach it by another name (a
DNS name from your router, a reverse proxy, the Docker host's name), add that
name to `allowed_hosts` in the configuration, or `PIWORKER_ALLOWED_HOSTS` for
Docker, and restart.

### The setup code does not work or has expired

It works once and for 30 minutes. Restart PiWorker to print a new one, or run
`piworker -setup-code` against the same database (the exact command for a
managed installation is in the
[operations guide](../operations.md#setup-and-account-recovery)).

### I forgot my password

Choose **Forgot password?** on the sign-in page and follow
[Recovering access](../../README.md#recovering-access): a command on the
device prints a one-use recovery code.

### HTTPS will not start and I am locked out

`sudo piworkerctl tls off` (or start with `-tls=off`, or
`PIWORKER_TLS_MODE=off` for Docker) serves plain HTTP on the same port so you
can get in and fix the configuration. `sudo piworkerctl tls on` restores
HTTPS.

## Building and running flows

### Deploy is refused

The message names the node and setting, for example
`nodes[3].config.interval: expected number (milliseconds), got string "5s"`.
Open that node, correct the value (numbers are plain numbers in the unit the
field states) and deploy again. Other common causes:

| Message mentions | Fix |
|------------------|-----|
| a secret that does not exist | Create it under **Secrets**, with exactly that name. The Secrets dialog lists names that are used but not defined. |
| a tool name already in use (409) | Another running flow has an Agent Tool with the same name. Rename one. |
| a webhook path already in use | Two running flows cannot share a webhook path. |
| a connection or port | Open **Connections** and reconnect the nodes it names. |

### Deployed, but with a warning

A yellow message after **Deploy** lists what the flow deployed with but you
should know. The usual one says a setting uses a secret that is not defined:
an Email or MQTT username or password, or an HTTP Request URL or header. The
flow runs, but that node gets an empty value there (it signs in without the
password, or not at all) or the HTTP request fails. Define the secret under
**Secrets** with exactly that name, or correct the reference; the warning is
gone on the next deploy.

### I cannot save my changes

Stop the flow first: a running flow cannot be saved. If the editor says the
flow changed elsewhere, another tab (or an approved agent draft) saved a newer
version; choose **Reload** and repeat your change.

### The flow runs but nothing happens

1. Open **Activity → Runs**. No runs at all means the trigger never fired:
   check its settings, or use a **Manual Inject** trigger and **Run manually**
   to test the rest of the flow.
2. Open a run. A failed node shows its error.
3. If every node succeeded, look at the data: put a **Debug** node after the
   step you doubt and read **Activity → Debug**. Most often a field name is
   different from what the template or expression expects (for a webhook, the
   body is under `payload.body`).
4. A **Filter** drops messages whose condition is false, and a **Switch**
   sends them out of **False**. Test the condition in the node's **Payload
   playground**.

### Runs are missing during a burst

When messages arrive faster than the flow finishes them, the trigger waits
instead of starting runs that would lose messages halfway: a **Webhook**
answers 503 (send the request again later), an **Interval
Timer** skips ticks, an MQTT trigger drops what arrives meanwhile. The
service log says `Trigger is waiting` with the flow and how many messages it
holds. Look in **Activity → Runs** for the node that takes long (a slow
request, a long **Delay**) and make it faster, or trigger less often.

### The flow list shows "Failed"

The flow was running before a restart and could not be started again. Open it
and choose **Deploy** to see the reason.

### A multi-output node sends nothing

**Switch (If/Else)** has two outputs. Make sure the next node is connected to
the right one (**True** or **False**); check it in **Connections**.

### My template shows nothing where a value should be

A placeholder without a value becomes empty text. Check the spelling and the
path: `{{payload.body.temperature}}`, not `{{payload.temperature}}`, when the
value comes from a webhook body. Templates use `{{…}}`; expressions
(Transform, Filter, Switch, Set Variable) do not use braces.

## Reaching other things

### The webhook answers 404, 401 or 405

| Status | Meaning |
|--------|---------|
| 404 | No **deployed** flow has that path. Deploy the flow and check the path. |
| 401 | The token is missing or wrong. Send it as `?token=…` or in the `X-Webhook-Token` header. |
| 405 | The node only accepts another HTTP method. |
| 503 | The flow is busy; try again. |

Devices that cannot verify PiWorker's certificate can call
`http://<device>:8080/webhooks/<path>` instead of the HTTPS address.

### HTTP Request or Notify refuses an address on my network

Requests to private addresses (`192.168.…`, `10.…`, `localhost`) are blocked
by default. Turn on **Allow Private Addresses** in that node when the target
really is a device on your network.

### A GPIO, I2C or sensor node fails

The hardware nodes (GPIO Edge, GPIO Write, I2C Read, DS18B20 Temperature) are
**experimental** in this release: they have automated tests but have not been
validated on real hardware. Try them with your own wiring before relying on
them, and report problems with the board model and OS.

- These nodes need a Raspberry Pi (or a board with the same Linux
  interfaces); on another machine they report that the hardware is missing.
- Enable the interface on the Pi (`sudo raspi-config` → *Interface Options*):
  I2C for I2C Read, 1-Wire for DS18B20.
- The service runs as the user `piworker`, which needs the device groups:

  ```bash
  sudo usermod -aG gpio,i2c piworker
  sudo systemctl restart piworker
  ```

### A Command node fails with a value starting with "-"

PiWorker refuses to pass a message value that starts with `-` as a whole
argument, because the program would read it as an option. Put fixed text in
front of the placeholder (`--value={{payload}}`), or see
[Command](../../README.md#actions-output) for the opt-in.

## AI in flows

The nodes that call a language model are described in
[AI in flows](ai.md), whose [troubleshooting](ai.md#troubleshooting) table
lists their error messages. The most common:

| Symptom | Check |
|---------|-------|
| Deploy refused: *names LLM connection "…", which is not defined* | Create it under **LLM connections** with exactly that name, or pick another connection in the node. |
| **Test connection** fails | A wrong API key (401), or a server PiWorker cannot reach: `localhost` is the PiWorker device itself, and Ollama accepts requests from other computers only when started with `OLLAMA_HOST=0.0.0.0`. Keep `/v1` at the end of an Ollama, LM Studio or OpenRouter base URL. |
| Nodes fail with *the connection's daily token limit is spent* | The connection's daily limit is reached until midnight (local time). Raise it in **LLM connections**, or wait. |
| *the answer was cut at maxTokens* | Raise the node's **Max tokens** or lower its **Effort**. |
| A local model ignores its instructions, or **Create with AI** fails with it | Its context window is too small for the prompt and the server cuts it silently. Give the model at least 16 000 to 32 000 tokens of context (Ollama: `OLLAMA_CONTEXT_LENGTH=32768`). |
| **Test node** on an LLM node times out | A test in the Payload playground stops after 2 minutes. The deployed flow uses the node's own **Timeout (s)** (up to 10 minutes). |
| The AI Agent does not call its tools | The tool's flow must be running and separate from the agent's, with **Confirmation** `none`; privileged tools need **Allow privileged tools**. The hints under the node's **Tools** say which apply. |

## AI agents

| Symptom | Check |
|---------|-------|
| The assistant cannot connect | MCP is on (`[mcp] enabled = true`, restart), the URL is `https://<device>:8443/mcp`, and the client trusts PiWorker's certificate (see [AI agents](agents.md#step-4-connect-your-assistant)). |
| The assistant does not see a tool | The flow with the Agent Tool is **deployed**, and the token was granted that tool. |
| A tool stopped working after I edited its flow | Changing what a tool is or reaches suspends its grants. Re-confirm the grant on the **Agents** page. |
| A call answers `approval_required` | The tool's confirmation is `admin`: approve it under **Agents → Approvals**. |
| The author or deploy tools are missing | `author_enabled = true` is needed for drafting; the **Agent deploy** level must be above Off for deploying. |

## Still stuck?

Collect the PiWorker version (`piworker -version`), the relevant log lines and
what you did, and open an issue at
<https://github.com/Pegasus8/piworker/issues>. Remove tokens, passwords and
webhook URLs from anything you paste.
