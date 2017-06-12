# Transcribe

Transcribe is a tool for transcribing audio files using Google Speech API. It
is intended for bulk processing of large (> 1 min) audio files and automates
GCS upload (and removal). It currently supports flac files only.

# How to use

First, ensure you have Google Speech API enabled in your project as described here: https://cloud.google.com/speech/docs/getting-started. Note that using the Google Speech API may not be free.

$ transcribe [options] file [...]
