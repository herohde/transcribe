// transcribe is a tool for transcribing audio files using Google Speech API. It
// is intended for bulk processing of large (> 1 min) audio files and automates
// GCS upload (and removal).
package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cloud.google.com/go/speech/apiv1"
	"github.com/herohde/transcribe/pkg/transcribe"
	"github.com/herohde/transcribe/pkg/util/storagex"
	"github.com/seekerror/build"
	"github.com/seekerror/logw"
	"google.golang.org/api/storage/v1"
)

var (
	project = flag.String("project", "", "GCP project to use. The project must have the Speech API enabled.")
	output  = flag.String("out", ".", "Directory to place output text files.")
	bucket  = flag.String("bucket", "", "Temporary GCS bucket to hold the audio files. If not provided, a new transient bucket will be created.")
	mono    = flag.Bool("mono", false, "Convert stereo audio file to mono (required if stereo).")

	version = build.NewVersion(0, 9, 0)
)

func init() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `usage: transcribe [options] file [...]

Transcribe transcribes audio files using Google Speech API. It is intended
for bulk processing of large (> 1 min) audio files and automates GCS upload
(and removal). Supported format: wav 44.1kHz (stereo or mono).
Options:
`)
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	ctx := context.Background()
	logw.Infof(ctx, "Transcribe, build %v", version)

	// (1) Validate input
	if len(flag.Args()) == 0 {
		flag.Usage()
		logw.Exitf(ctx, "No files provided.")
	}
	if *project == "" {
		flag.Usage()
		logw.Exitf(ctx, "No project provided.")
	}

	var files []string
	for _, file := range flag.Args() {
		if !strings.HasSuffix(strings.ToLower(file), ".wav") {
			flag.Usage()
			logw.Exitf(ctx, "File %v is not a supported (.wav) format", file)
		}

		out := filepath.Join(*output, filepath.Base(file)+".txt")
		if _, err := os.Stat(out); err == nil || !os.IsNotExist(err) {
			logw.Infof(ctx, "File %v already transcribed. Ignoring.", file)
			continue
		}

		files = append(files, file)
	}
	if len(files) == 0 {
		return // exit: nothing to do
	}

	// (2) Create GCP clients

	cl, err := storagex.NewClient(context.Background())
	if err != nil {
		logw.Fatalf(ctx, "Failed to create GCS client: %v", err)
	}
	scl, err := speech.NewClient(context.Background())
	if err != nil {
		logw.Fatalf(ctx, "Failed to create speech client: %v", err)
	}

	// (3) Create tmp location, if needed.

	if *bucket == "" {
		*bucket = fmt.Sprintf("transcribe-%v", time.Now().UnixNano())

		if err := storagex.NewBucket(cl, *project, *bucket); err != nil {
			logw.Fatalf(ctx, "Failed to create tmp bucket %v: %v", *bucket, err)
		}
		defer storagex.TryDeleteBucket(ctx, cl, *bucket)

		logw.Infof(ctx, "Using temporary GCS bucket '%v'", *bucket)
	}

	logw.Infof(ctx, "Transcribing %v audio files in parallel", len(files))

	// (4) Upload, transcribe and process the files in parallel

	var failures int32

	var wg sync.WaitGroup
	for _, name := range files {
		wg.Add(1)
		go func(filename string) {
			defer wg.Done()

			name := filepath.Base(filename)
			out := filepath.Join(*output, name+".txt")

			logw.Infof(ctx, "Transcribing %v ...", name)

			if err := process(context.Background(), scl, cl, *bucket, filename, out, *mono); err != nil {
				logw.Errorf(ctx, "Failed to process %v: %v", name, err)
				atomic.AddInt32(&failures, 1)
				return
			}

			logw.Infof(ctx, "Transcribed %v", name)
		}(name)
	}
	wg.Wait()

	if failures > 0 {
		logw.Fatalf(ctx, "Failed to transcribe %v audio files. Exiting.", failures)
	}
	logw.Infof(ctx, "Done")
}

func process(ctx context.Context, scl *speech.Client, cl *storage.Service, bucket, filename, output string, mono bool) error {
	name := filepath.Base(filename)

	if mono {
		// (a) If stereo, convert first to mono

		tmp := filepath.Join(os.TempDir(), name)

		out, err := exec.Command("sox", filename, tmp, "remix", "1-2").CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to convert %v to mono (err=%v): %v. Do you have sox installed?", name, err, string(out))
		}
		defer os.Remove(tmp)

		filename = tmp
	}

	// (b) Upload

	object := path.Join("tmp/audio", strings.ToLower(name))
	if err := storagex.UploadFile(cl, bucket, object, filename); err != nil {
		return err
	}
	defer storagex.TryDeleteObject(ctx, cl, bucket, object)

	// (c) Transcribe

	before := time.Now()

	phrases, err := transcribe.Submit(ctx, scl, bucket, object)
	if err != nil {
		return err
	}
	data := transcribe.PostProcess(phrases)

	duration := time.Duration((time.Now().Sub(before).Nanoseconds() / 1e9) * 1e9)
	logw.Infof(ctx, "Audio file %v contained %v text segments (%v letters). Time spent: %v", name, len(phrases), len(data), duration)

	// (d) Write output

	if err := ioutil.WriteFile(output, []byte(data), 0644); err != nil {
		return fmt.Errorf("failed to write output: %v", err)
	}
	return nil
}
