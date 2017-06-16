// transcribe is a tool for transcribing audio files using Google Speech API. It
// is intended for bulk processing of large (> 1 min) audio files and automates
// GCS upload (and removal).
package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cloud.google.com/go/speech/apiv1"
	"google.golang.org/api/storage/v1"

	"github.com/herohde/transcribe/pkg/transcribe"
	"github.com/herohde/transcribe/pkg/util/storagex"
	"os/exec"
	"path"
	"sync"
	"sync/atomic"
)

var (
	project = flag.String("project", "", "GCP project to use. The project must have the Speech API enabled.")
	output  = flag.String("out", ".", "Directory to place output text files.")
	bucket  = flag.String("bucket", "", "Temporary GCS bucket to hold the audio files. If not provided, a new transient bucket will be created.")
	mono    = flag.Bool("mono", false, "Convert stereo audio file to mono (required if stereo).")
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

	// (1) Validate input
	if len(flag.Args()) == 0 {
		flag.Usage()
		log.Fatal("No files provided.")
	}
	if *project == "" {
		flag.Usage()
		log.Fatal("No project provided.")
	}

	var files []string
	for _, file := range flag.Args() {
		if !strings.HasSuffix(strings.ToLower(file), ".wav") {
			flag.Usage()
			log.Fatalf("File %v is not a supported (.wav) format", file)
		}

		out := filepath.Join(*output, filepath.Base(file)+".txt")
		if _, err := os.Stat(out); err == nil || !os.IsNotExist(err) {
			log.Printf("File %v already transcribed. Ignoring.", file)
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
		log.Fatalf("Failed to create GCS client: %v", err)
	}
	scl, err := speech.NewClient(context.Background())
	if err != nil {
		log.Fatalf("Failed to create speech client: %v", err)
	}

	// (3) Create tmp location, if needed.

	if *bucket == "" {
		*bucket = fmt.Sprintf("transcribe-%v", time.Now().UnixNano())

		if err := storagex.NewBucket(cl, *project, *bucket); err != nil {
			log.Fatalf("Failed to create tmp bucket %v: %v", *bucket, err)
		}
		defer storagex.TryDeleteBucket(cl, *bucket)

		log.Printf("Using temporary GCS bucket '%v'", *bucket)
	}

	log.Printf("Transcribing %v audio files in parallel", len(files))

	// (4) Upload, transcribe and process the files in parallel

	var failures int32

	var wg sync.WaitGroup
	for _, name := range files {
		wg.Add(1)
		go func(filename string) {
			defer wg.Done()

			name := filepath.Base(filename)
			out := filepath.Join(*output, name+".txt")

			log.Printf("Transcribing %v ...", name)

			if err := process(context.Background(), scl, cl, *bucket, filename, out, *mono); err != nil {
				log.Printf("Failed to process %v: %v", name, err)
				atomic.AddInt32(&failures, 1)
				return
			}

			log.Printf("Transcribed %v", name)
		}(name)
	}
	wg.Wait()

	if failures > 0 {
		log.Fatalf("Failed to transcribe %v audio files. Exiting.", failures)
	}
	log.Print("Done")
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
	defer storagex.TryDeleteObject(cl, bucket, object)

	// (c) Transcribe

	before := time.Now()

	phrases, err := transcribe.Submit(ctx, scl, bucket, object)
	if err != nil {
		return err
	}
	data := transcribe.PostProcess(phrases)

	duration := time.Duration((time.Now().Sub(before).Nanoseconds() / 1e9) * 1e9)
	log.Printf("Audio file %v contained %v text segments (%v letters). Time spent: %v", name, len(phrases), len(data), duration)

	// (d) Write output

	if err := ioutil.WriteFile(output, []byte(data), 0644); err != nil {
		return fmt.Errorf("failed to write output: %v", err)
	}
	return nil
}
