# Transcribe

Transcribe is a tool for transcribing audio files using Google Speech API. It
is intended for bulk processing of large (> 1 min) audio files -- such as from
dictation recorders -- and automates GCS upload (and removal). It supports
44.1kHz .wav files only.

## How to use

First, ensure you have Google Speech API enabled in your project as described
[here](https://cloud.google.com/speech/docs/getting-started). Note that using
the Google Speech API may not be free.

Second, install
[gcloud](https://cloud.google.com/sdk/) and allow application default
credentials:
```
$ gcloud auth application-default login
```

Third, install 'sox' if stereo conversion is needed:
```
$ apt-get install sox
```
or equivalent. On OSX, an option would be `$ brew install sox`.

Fourth, install the transcribe tool:
```
$ go get github.com/herohde/transcribe
$ go install github.com/herohde/transcribe/cmd/transcribe
```

Then run:
```
$ transcribe --project=myproject [options] file [...]
```
By default, it will transcribe 'bar/foo.wav' into 'foo.wav.txt'. Add `--mono` if
stereo files.

## License

Transcribe is released under the [MIT License](http://opensource.org/licenses/MIT).
