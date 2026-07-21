# Downloads

All builds live on the **[Releases page](https://github.com/dalemusser/mhsaudiotools/releases/latest)**.
Each release lists a file per platform for both the **app** and the **CLI**.

Files are named `aigenaudio-<app|cli>-<platform>`. Pick your platform below.

## Which computer do I have?

- **Mac** — all Macs since ~2020 are "Apple Silicon" (the chip is M1/M2/M3/M4/M5).
  Use the **macos-arm64** file. *(Older Intel Macs aren't currently built — ask if you need one.)*
- **Windows** — check **Settings → System → About → System type**:
  - "x64-based processor" → **windows-amd64**
  - "ARM-based processor" (Surface Pro X, Snapdragon laptops) → **windows-arm64**
- **Linux** — run `uname -m`:
  - `x86_64` → **linux-amd64**
  - `aarch64` / `arm64` (Raspberry Pi, AWS Graviton, ARM boards) → **linux-arm64**

## The app (most people)

| Platform | File |
|---|---|
| macOS (Apple Silicon) | `aigenaudio-app-macos-arm64.zip` |
| Windows x64 | `aigenaudio-app-windows-amd64.exe` |
| Windows ARM | `aigenaudio-app-windows-arm64.exe` |
| Linux x64 | `aigenaudio-app-linux-amd64.tar.gz` |
| Linux ARM | `aigenaudio-app-linux-arm64.tar.gz` |

## The CLI (technical / automation)

| Platform | File |
|---|---|
| macOS (Apple Silicon) | `aigenaudio-cli-macos-arm64` |
| Windows x64 | `aigenaudio-cli-windows-amd64.exe` |
| Windows ARM | `aigenaudio-cli-windows-arm64.exe` |
| Linux x64 | `aigenaudio-cli-linux-amd64` |
| Linux ARM | `aigenaudio-cli-linux-arm64` |

Next: **[INSTALL.md](INSTALL.md)** to set it up.

---

*Why not download from the code itself?* Builds aren't stored in the git repo —
they're attached to each Release. That keeps the repo small and makes sure every
download is a clean, reproducible build from a tagged version.
