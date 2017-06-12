# transcribe

Transcribe is a tool for transcribing audio files using Google Speech API. It
is intended for bulk processing of large (> 1 min) audio files and automates
GCS upload (and removal).

# How to use

First, ensure you have Google Speech API enabled in your project as described here:
https://cloud.google.com/speech/docs/getting-started.

$ transcribe [options] file [...]