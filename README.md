# ascii
Extracts text from binary, including optionally UTF-8/16, with controls and output options.

ASCII mode grabs lower-bit characters.  A few control characters and 0x20 - 0x7E.

UTF8 mode accepts those and recognizes possible UTF-8, using the encoding standard.

Inspired by the 1985-86 ASCII.exe program from SEA (System Enhancement Associates) of ARC (pre-PKZIP fame), 
by Thom Henderson, one of the early heroes of the pre-Internet.

Yes, I still have a copy of ASCII.exe.  It runs fine in DOSBox-X.

Simple usage: ascii &lt;input file&gt;

This will extract standard ASCII, no UTF-8/16, that is six characters or longer, and output it to the console.
