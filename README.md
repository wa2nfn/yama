YAMA — Yet Another Morse-code App

YAMA is a lightweight, standalone Morse practice application for Windows, macOS, and Linux. No installer, no complex setups, and no network required — just download and run.

Features
* KeyEcho sending mode with real timing capture

* Numerous copy features for Instant Word Recognition (IWR) and head-copy

* Software sidetone oscillator

* A single file executable with no PC changes needed

* Works fully offline

* Simple, predictable terminal UI with clean navigation (TAB/ENTER), and mouse for many actions

* Adjustable speed and tone

* Play live or create downloadable WAV files

Download YAMA
=============
Visit the Releases page: https://github.com/wa2nfn/yama/releases. Select the desired version,

Under the Assets section of the latest release, choose the appropriate archive for your operating system:

* Windows: yama-windows-amd64.zip

* macOS (Apple Silicon / M1/M2/M3/M4): yama-mac-arm64.zip

* macOS (Intel): yama-mac-intel.zip

* Linux: yama-linux-amd64.tar.gz

Save the file to your computer.

Installation & Execution
========================
If you are already familiar with extracting archives, setting executable permissions, and running command-line applications, simply extract the binary, ensure it is executable, and launch it. You can skip the detailed steps below.

Because YAMA is a terminal-based interface (TUI), running it directly from your command prompt or terminal is highly recommended for the best experience. On most modern releases of the supported OSs, you should be able to just launch the app with a double-click. If it does not open that way or the application window closes unexpectedly, run it from a command prompt/terminal window.

Inside the app, press F1 for full application details.

Windows
=======
The application is a standalone executable and requires no extra libraries.

Right-click the downloaded .zip file and select Extract All....

Try double-clicking the extracted yama.exe.

If it does not run, open Command Prompt or PowerShell, navigate to your extracted folder, and run:

DOS
.\yama.exe

Security Note: Windows SmartScreen or other antivirus tools may show a "This app is not commonly downloaded" message. Click "More info" and choose "Run anyway." Feel free to scan the file with your antivirus — YAMA is a single, static Go binary.

macOS
=====
macOS releases are provided as zip archives containing the native binary for your processor.

Double-click the downloaded .zip file to extract the yama application.

Try double-clicking the extracted yama file.

If it does not run, open the Terminal app and navigate to your extracted folder.

Make the file executable:

chmod +x yama
Run the application:

./yama

Security Note: Because Apple blocks software not signed by a paid developer account, running it may trigger an "unidentified developer" warning. If a security popup blocks it, go to System Settings > Privacy & Security, scroll down, and click Allow Anyway next to the YAMA prompt.

Linux
=====
Because YAMA generates Morse code audio, it requires the standard ALSA audio library, which is pre-installed on most desktop Linux distributions.

Extract the downloaded archive (e.g., tar -xzf yama-linux-amd64.tar.gz).

Open your terminal and navigate to the extracted folder.

Make the file executable:

chmod +x yama
Run the application:

./yama

(Note: If you experience audio errors on a very minimal Linux installation, ensure ALSA is installed by running sudo apt install libasound2 or your distribution's equivalent).

License
Open-source. Free for all operators.

Additional Information
======================
IWR - Instant Word Recognition is a significant feature of the app. At a bare minimum, you must check the option "Use IWR" on this page, and the IWR Speed must be greater than the Character Speed. Use the Edit IWR button to get details about IWR words. Also, in the Help menu (function key F1) there is more on IWR.

You can now see how it is straightforward to hear input whether typed in or read from a file (Ctrl-F), but there are many Options (Ctrl-O menu) that modify input or change plain play into headcopy features (Random Words, Word Builder, Text Builder, Word-At-A-Time (aka flashcard), etc.).

Use Numwords (Ctrl-N) after you have loaded text in the Input Text screen if you want to increase/decrease the amount of text to practice. You can also perform edits on the data with: insert/delete, pg up/dn, home/end, backspace instead of a standalone editor. If you increase the number available, you will get additional words added in the same order. For general CW practice, you likely will want to use the random button (or the randomize option in the Ctrl-O screen).

The DataStats (Ctrl-D) is available after some play has taken place. It is for the last played text and is mostly of interest to those using the IWR feature. An alternate form of DataStats can be used with the KeyEcho feature; its presence can be toggled with Ctrl-D without losing your current statistics.

The Audio Impairments (Ctrl-A) and Timing (Ctrl-T) are unique in that you do not have to abandon the current practice session in order to make a change. Timing changes do require a temporary pause (Ctrl-P during Play becomes Pause as the menu bar indicates) and then Ctrl-R resumes play. Impairments is fully dynamic, allowing sound modification during play.

Also during play, a toggle of the SPACEBAR hides/unhides the Text Input screen which is now showing played code.

There is one feature, KeyEcho, which is for sending practice. The bulk of the options for this are on the KeyEcho (Ctrl-K) screen; however, it also works in conjunction with the Timing (Ctrl-T) and the Options (Ctrl-O) that were used for receiving practice. This combination can allow your practice session to improve receiving, head-buffer, IWR, at the same time as sending practice. See Examples below.

This README does not cover all the details of the app; please read or print the entire Help available with function key F1.

Examples
========
I will add examples here so as to not overload the app's F1 Help text. This may grow as I get user feedback. If you develop a particular combination that you find especially helpful, let me know and I will add it and your name/call sign will be added to internet history (LOL).

1. KeyEcho Combination With WordBuilder For IWR
Create an input file of your choice (CW abbreviations, Q-Signals, etc.). I asked AI to make me a file. Here are the simple steps with that file. This simple input gives practice on 23 letters of the alphabet (lots of Qs).

1- Start Yama
2- Ctrl-T, set my practice character speed (and tone). SAVE
3- Ctrl-K, (a few more than necessary) set Tolerance=15% (challenging), Word Count=1, Key Now Alert Tone=All, Last Word Space Dit Cnt=4 (challenging, normal is 7), SideTone and Visual Feedback both=checked. SAVE
4- Ctrl-O, Options. Check these 3: Random Order, Word Builder (your choice), KeyEcho (this is what makes YAMA do echo when Play is done! else its for code copy).
5- Ctrl-F, select the txt file of Q-signals you or AI created.
6- Ctrl-N, I'll increase the 36 standard Q-Signals to 120 so I hear each Q-Signal at least 3 times.

That's all the prep, it's all saved, the next time you might not change anything. Assuming you did the one-time KeyEcho setup (port, adapter signals/pins, and created your comport adapter).

Connect key device to com port adapter, insert into PC.

Ready to practice.
7- Ctrl-P, the app's start play command. (it's immediate unless you set a start delay on Ctrl-O).

What happens now is covered in the F1 Help write up. Basically, YAMA sends a Q-Code, you get a prompt, you send it back. Repeat...
