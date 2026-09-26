# YAMA — Yet Another Morse-code App

YAMA is a lightweight, standalone Morse practice application for Windows, macOS, and Linux. No installer, no complex setups, and no network required — just download and run.

### Features
* KeyEcho sending mode with real timing capture
* Numerous copy features for Instant Word Recognition (IWR) and head-copy
* Software sidetone oscillator
* A single file executable with no PC changes needed
* Works fully offline
* Simple, predictable terminal UI with clean navigation (TAB/ENTER)
* Adjustable speed and tone
* Play live or create downloadable WAV files

### Download YAMA

**Option 1 — Download from GitHub (Web Browser)**
Visit the Releases page: https://github.com/wa2nfn/yama/releases

Under **Assets**, choose your platform: Windows (`yama.exe`), macOS (`yama-darwin`), or Linux (`yama-linux`). Save the file to your computer.

**Option 2 — Download Using curl**

*Windows (PowerShell or CMD):*
```cmd
curl -L -o yama.exe [https://github.com/wa2nfn/yama/releases/latest/download/yama.exe](https://github.com/wa2nfn/yama/releases/latest/download/yama.exe)
```

*macOS (Terminal):*
```bash
curl -L -o yama-darwin [https://github.com/wa2nfn/yama/releases/latest/download/yama-darwin](https://github.com/wa2nfn/yama/releases/latest/download/yama-darwin)
```

*Linux (Terminal):*
```bash
curl -L -o yama-linux [https://github.com/wa2nfn/yama/releases/latest/download/yama-linux](https://github.com/wa2nfn/yama/releases/latest/download/yama-linux)
```

### Installation & Execution

Because YAMA is a terminal-based interface (TUI), running it directly from your command prompt or terminal is highly recommended for the best experience. Double-clicking the file from a file manager can sometimes cause the application window to close unexpectedly if an error occurs. 

*Inside the app, press **F1** for full application details.*

**Windows**
The `yama.exe` file is a standalone executable and requires no extra libraries.
1. Open **Command Prompt** or **PowerShell** and navigate to your download folder.
2. Run the application:
   ```cmd
   .\yama.exe
   ```
*Security Note:* Windows SmartScreen or other antivirus tools may show a "This app is not commonly downloaded" message. Click "More info" and choose "Run anyway." Feel free to scan the file with your antivirus — YAMA is a single, static Go binary.

**Linux**
Because YAMA generates Morse code audio, it requires the standard ALSA audio library, which is pre-installed on most desktop Linux distributions.
1. Open your terminal and navigate to the download folder.
2. Make the file executable:
   ```bash
   chmod +x yama-linux
   ```
3. Run the application:
   ```bash
   ./yama-linux
   ```
*(Note: If you experience audio errors on a very minimal Linux installation, ensure ALSA is installed by running `sudo apt install libasound2` or your distribution's equivalent).*

**macOS**
This is an Intel-based (amd64) binary. It runs perfectly on newer Apple Silicon (M1/M2/M3) Macs using Apple's built-in Rosetta 2 translator.
1. Open the **Terminal** app and navigate to your download folder.
2. Make the file executable:
   ```bash
   chmod +x yama-darwin
   ```
3. Run the application:
   ```bash
   ./yama-darwin
   ```
*Security Note:* Because Apple blocks software not signed by a paid developer account, running it may trigger an "unidentified developer" warning. If a security popup blocks it, go to **System Settings > Privacy & Security**, scroll down, and click **Allow Anyway** next to the YAMA prompt.

### License
Open-source. Free for all operators.
