# Yama - Morse Code Terminal Training Application

Yama is a terminal user interface (TUI) application designed for focused Morse 
code (CW) training.  Written in Go, it emphasizes effective auditory 
recognition methods to help users build their copying speed and accuracy.

No install or changes to your PC. Simply put the executable (binary) in a 
directory that is in your PATH. 

For Windows:

Open a Powershell or Cmd Window.
Run "yame.exe" or ".\yama.exe" if you are not in the execution PATH.
Double click the name from Explorer also works.

For Linux:

Open a terminal window.
Run "yama" or "./yama".
Double click will depend on your Linux.

The app opens to screen with title, list of menu items, and Text Input area.
As the prompt indicates, you can type in some text or read in from a file. 
To give a quick try and set and let you set some preferred settings 
(speed, tone), just type a few words and simultaneously press Ctrl and 
P keys to Play.

Your words should have played with several default values. To change them, 
look at the second line with menu names. The letter shown in YELLOW, is the 
key to with the Ctrl key for access. Press Ctrl-T for Timing menu.
Use the TAB or BACKTAB to navigate to an option to change, a few such as
Mode will chnage color indicating a dropdown of choices, in that case use
cursor up/down and hit either Enter or Space to select the value. Most options
simply backspace to clear and retype you choice. When you are donw TAB down to 
the SAVE button and hit ENTER. If you use ESC to exit a screen the changes are 
NOT saved.

IWR - Instant Word Recognition, is a significant feature of the app, at a bare minimum
you must check the option "Use IWR" on this page, and the IWR Speed must be
greater than the Character Speed. Use the Edit IWR button, to get details about
IWR words. Also in the Help menu (function key F1) there is more on IWR.

You can now see how is straight forward to hear input whether typed in, or read from
a file(Ctrl-F), but there are many Options (Ctrl-O menu) that modifiy input or
change plain play into headcopy features (Random Words, Word Builder, Text Builder,
Word-At-A-Time (aka flashcard).

Use Numwords (Ctrl-N) after you have loaded text in the Input Text screen, if you
want to increas/decrease the amount of text to practice. You can also preform edits
on the data with: insert/delete, pg up/dn, home/end, backspace instead of a 
standalone editor.

The DataStats (Ctrl-D) is avaliable after some play has taken place. It is for the 
last placed text and is mostly of interest to those using the IWR feature.

The Audio Impairments (Ctrl-A) and and Timing (Ctrl-A) are unique in that you do not have
to abandon the current proctice session in order to make a change. Timing changes do
require a temporary pause (Ctrl-P during Play becomes Pause as the menu bar indicates) 
and then Crtl-R resumes play. Impairments is fully dynamic, allowing sound modification
during play.

Also during play, a toggle of the SPACEBAR hides/unhides the Text Input screen which is 
now showing played code. 

This README, does not cover all the details of the app, please read or print the entire
Help available with function key F1.

73 WA2NFN




