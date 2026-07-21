# MHS Audio Tools

Generate spoken dialog audio for the game with [ElevenLabs](https://elevenlabs.io).
Point it at your dialog (a Dialogue System export or a writer's script), pick the
voices, and it produces one audio file per line — in the exact folder layout the
game's Dialogue System expects.

There are **two tools**, both built from the same core:

| | For whom | What it is |
|---|---|---|
| **MHS Audio Generator** (app) | Everyone — artists, writers, designers | A normal desktop app. Install it, click through three steps, get audio. No terminal, no Python. |
| **`mhsaudio` (CLI)** | Technical users, automation | The same engine as a command-line tool, for scripting and batch runs. |

Most people want the **app**.

## Download

Grab the latest build from the **[Releases page](https://github.com/dalemusser/mhsaudiotools/releases/latest)**.
Not sure which file? See **[docs/DOWNLOAD.md](docs/DOWNLOAD.md)** — it maps your
computer to the right download for both the app and the CLI.

## Install & use

- **[docs/INSTALL.md](docs/INSTALL.md)** — installing on macOS, Windows, and Linux (app and CLI).
- **[docs/USAGE.md](docs/USAGE.md)** — how to use the app and the CLI, with examples.

You'll need a free-or-paid **ElevenLabs API key** (the app asks for it on first
run and saves it). See [Getting an API key](docs/INSTALL.md#getting-an-elevenlabs-api-key).

## Build from source

Requires Go 1.25+. From the repo root:

```bash
make verify     # build + vet + test the engine and CLI
make cli-all    # cross-compile the CLI for all 5 platforms -> dist/
make app        # build the desktop app for THIS machine (needs the Wails CLI)
```

The app is a [Wails](https://wails.io) desktop app; see [wails/README.md](wails/README.md)
for its toolchain. Releases for every platform are built in CI
(`.github/workflows/release.yml`) — push a `v*` tag to produce them. Architecture
and design notes are in [DESIGN.md](DESIGN.md).

## License

[MIT License](LICENSE) — Copyright © 2026 Intelligence Builders. Free to use,
modify, and distribute; keep the copyright notice.
