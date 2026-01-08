# SIP Controller (experiment harness)

This command is an **experiment harness** that connects to a running `baresip` instance over `ctrl_tcp` and places/observes calls so you can measure **caller-side call setup latency**.

The primary metric emitted is:

- `latency_ms = CALL_ANSWERED_time - dial_command_write_time`

It supports:

- **Baseline** calls (protocol disabled)
- **Integrated** calls (protocol enabled per call)
- **Multiple simultaneous calls** to the same recipient (attempt/call-id centric tracking)

## Prerequisites

- A running `baresip` instance with `ctrl_tcp` enabled and reachable at `-baresip` (default `localhost:4444`).
- A DIA env file (required by this binary): `-env /path/to/dia.env`
  - Even in baseline mode, the binary currently requires `-env`.

## Build / Run

From the `denseid` module root:

- Run directly:

  `go run ./cmd/sipcontroller -env /path/to/dia.env`

- Or build:

  `go build -o sipcontroller ./cmd/sipcontroller`

## Modes

### 1) Interactive mode (stdin commands)

Start:

`go run ./cmd/sipcontroller -env /path/to/dia.env -baresip localhost:4444`

Commands:

- `dial <phone_or_uri> on|off`
  - `on` = protocol enabled for this call
  - `off` = baseline
- `list`
- `oda <call_id> <attr1,attr2,...>`
- `run <baseline|integrated> <phone> <runs> [concurrency]`
- `quit`

Examples:

- Baseline dial:

  `dial +15551234567 off`

- Integrated dial:

  `dial +15551234567 on`

- Batch run:

  `run baseline +15551234567 20 5`

### 2) Non-interactive experiment mode (recommended)

This mode places calls automatically and prints **one JSON line per completed attempt** to stdout.

Flags:

- `-experiment baseline|integrated`
- `-phone <dialstring>`
- `-runs <N>`
- `-concurrency <N>`

Examples:

- Baseline:

  `go run ./cmd/sipcontroller -env /path/to/dia.env -experiment baseline -phone +15551234567 -runs 50 -concurrency 5`

- Integrated:

  `go run ./cmd/sipcontroller -env /path/to/dia.env -experiment integrated -phone +15551234567 -runs 50 -concurrency 5`

## Output format

Each completed attempt emits a JSON object (and is also logged with the `[Result]` prefix).

Fields (typical):

- `attempt_id`: controller-generated attempt identifier
- `call_id`: baresip call-id (when known)
- `peer_phone`, `peer_uri`
- `direction`: `outgoing` / `incoming`
- `protocol_enabled`: `true|false`
- `dial_sent_unix_ms`: timestamp when the `dial` netstring was written to ctrl_tcp
- `answered_unix_ms`: timestamp when `CALL_ANSWERED` was observed
- `latency_ms`: `answered - dial_sent`
- `outcome`: `answered | closed | error`
- `error`: error detail for `closed`/`error` outcomes

## Notes / Limitations

- Call association for multiple simultaneous calls to the same recipient uses a **FIFO queue per peer** until the baresip call-id is observed via `CALL_OUTGOING` / `CALL_RINGING`.
- `ctrl_tcp` command responses use a short timeout (to prevent hung experiments). This does **not** limit the call setup time; it only prevents the control channel from stalling forever.

## Troubleshooting

- If the controller can’t connect to baresip: verify `-baresip host:port` and that `ctrl_tcp` is enabled in baresip.
- If calls never reach `CALL_ANSWERED`: the remote may not be answering, or baresip may be emitting `CALL_ESTABLISHED` only (check baresip logs).
- If you see dropped events: run with `-verbose` to inspect raw ctrl messages.

## Integrated incoming + ODA-after-answer

To run the **recipient** in integrated mode (answer after DIA verification, then trigger ODA after `CALL_ANSWERED`, then hang up):

- `-incoming-mode integrated -oda-after-answer -oda-attrs <attrs> [-oda-timeout <seconds>]`

Baseline incoming auto-answer (no DIA):

- `-incoming-mode baseline`

## Convenience wrapper: `controller.sh`

There is a helper script at `denseid/controller.sh` that:

- Reads infra IPs from `denseid/infras/hosts.yml`
- Infers host from account (1XXX → client-1, 2XXX → client-2)
- Infers env file as `.env.<account>` (e.g. `.env.1001`)
- Provides “recipes” so you don’t need to remember controller flags

Default ODA attrs for `recv-int-oda` are `name,issuer` (override via optional arg or `ODA_ATTRS`).

Examples:

- Interactive controller (REPL + logs): `./controller.sh it 1001`
- Recipient baseline (auto-answer): `./controller.sh recv-base 2002`
- Recipient integrated + ODA-after-answer: `./controller.sh recv-int-oda 2002`
- Caller baseline batch: `./controller.sh call-base 1001 +15551234567 50 5`
- Caller integrated batch: `./controller.sh call-int 1001 +15551234567 50 5`
- Print resolved IPs: `./controller.sh ips`
