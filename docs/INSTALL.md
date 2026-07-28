# Installing

First pick the right file for your computer — see **[DOWNLOAD.md](DOWNLOAD.md)**.

> **Heads up on security warnings.** These builds aren't yet signed with a paid
> developer certificate, so macOS and Windows will show a "this is from an
> unidentified developer" warning the first time. That's expected for now; the
> steps below show how to open it anyway. (Signing is planned.)

---

## The app

### macOS (Apple Silicon)

1. Download `mhsaudio-app-macos-arm64.zip` and double-click to unzip.
2. Drag **MHS Audio Generator.app** into your **Applications** folder.
3. The first time, **right-click the app → Open**, then click **Open** in the
   dialog. (Double-clicking the first time will be blocked because the app isn't
   signed yet — right-click → Open gets past it. You only do this once.)

### Windows (x64 or ARM)

1. Download the matching `mhsaudio-app-windows-*.exe`.
2. Double-click it. Windows SmartScreen may say "Windows protected your PC" —
   click **More info → Run anyway**.
3. The app needs Microsoft's **WebView2 runtime**, which is already present on
   virtually all Windows 10/11 machines. If the window is blank, install it free
   from Microsoft ("Evergreen Standalone Installer") and reopen.

### Linux (x64 or ARM)

1. Extract: `tar -xzf mhsaudio-app-linux-*.tar.gz`
2. Make it runnable: `chmod +x mhsaudio`
3. Install the runtime libraries it needs (GTK3 + WebKitGTK):
   ```bash
   # Debian/Ubuntu
   sudo apt install libgtk-3-0 libwebkit2gtk-4.1-0
   ```
4. Run it: `./mhsaudio`

---

## The CLI

The CLI is a single file. Put it somewhere on your `PATH` and make it executable.

### macOS / Linux

```bash
# example for macOS Apple Silicon; use your platform's file
chmod +x mhsaudio-cli-macos-arm64
sudo mv mhsaudio-cli-macos-arm64 /usr/local/bin/mhsaudio

mhsaudio --help
```

On macOS, if you see *"cannot be opened because the developer cannot be verified"*,
clear the quarantine flag once:

```bash
xattr -d com.apple.quarantine /usr/local/bin/mhsaudio
```

### Windows

Put `mhsaudio-cli-windows-amd64.exe` (or `-arm64`) somewhere convenient, then run
it from PowerShell or Command Prompt:

```powershell
.\mhsaudio-cli-windows-amd64.exe --help
```

If SmartScreen blocks it, choose **More info → Run anyway**.

---

## Getting an ElevenLabs API key

Both tools call ElevenLabs on your behalf and need an API key:

1. Sign in at [elevenlabs.io](https://elevenlabs.io).
2. Open your profile → **API Keys** → create a key and copy it.
3. Give it to the tool:
   - **App:** paste it when prompted on first run. On macOS, keep the
     "Store in the macOS Keychain" checkbox on to save it encrypted; otherwise
     it's saved to `~/.elevenlabs_key` (readable only by you).
   - **CLI:** set `ELEVENLABS_API_KEY` in your environment, or pass
     `-key-file <path>`, or save it to `~/.elevenlabs_key`. On macOS,
     `mhsaudio key -store-keychain` moves it into the Keychain.
   See [API key storage](USAGE.md#api-key-storage) in USAGE for the full
   resolution order.

**Never commit or share the key.** Anyone with it can spend the account's credits.

Next: **[USAGE.md](USAGE.md)**.
