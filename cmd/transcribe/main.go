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
	"golang.org/x/oauth2/google"
	"google.golang.org/api/storage/v1"

	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"
	"os/exec"
	"path"
	"sync"
)

var (
	project = flag.String("project", "", "GCP project to use. The project must have the Speech API enabled.")
	output  = flag.String("out", ".", "Directory to place output text files.")
	bucket  = flag.String("bucket", "", "Temporary GCS bucket to hold the audio files. If not provided, a new transient bucket will be created.")
	mono    = flag.Bool("mono", false, "Convert stereo audio file to mono (required if stereo).")
)

func init() {
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `
usage: transcribe [options] file [...]

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

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "transcribe: no files provided.")
		flag.Usage()
		os.Exit(1)
	}
	if *project == "" {
		fmt.Fprintln(os.Stderr, "transcribe: no project provided.")
		flag.Usage()
		os.Exit(2)
	}

	// (2) Create tmp location, if needed.

	cl, err := newStorageClient(context.Background())
	if err != nil {
		log.Fatalf("Failed to create GCS client: %v", err)
	}

	if *bucket == "" {
		*bucket = fmt.Sprintf("transcribe-%v", time.Now().UnixNano())

		if _, err := cl.Buckets.Insert(*project, &storage.Bucket{Name: *bucket}).Do(); err != nil {
			log.Fatalf("Failed to create tmp bucket %v: %v", *bucket, err)
		}
		log.Printf("Using temporary GCS bucket '%v'", *bucket)

		defer func() {
			if err := cl.Buckets.Delete(*bucket).Do(); err != nil {
				log.Printf("Failed to delete tmp bucket %v: %v", *bucket, err)
			}
		}()
	}

	scl, err := speech.NewClient(context.Background())
	if err != nil {
		log.Fatalf("Failed to create speech client: %v", err)
	}

	log.Printf("Transcribing %v files in parallel", len(files))

	// (3) Upload and transcribe the files in parallel

	var wg sync.WaitGroup
	for _, name := range files {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()

			if !strings.HasSuffix(strings.ToLower(name), ".wav") {
				log.Printf("File %v is not a supported format. Ignoring.", name)
				return
			}

			out := filepath.Join(*output, filepath.Base(name)+".txt")
			if _, err := os.Stat(out); err == nil || !os.IsNotExist(err) {
				log.Printf("File %v already transcribed. Ignoring.", name)
				return
			}

			log.Printf("Transcribing %v ...", filepath.Base(name))

			// (a) If stereo, convert first to mono

			if *mono {
				tmp := filepath.Join(os.TempDir(), filepath.Base(name))

				out, err := exec.Command("sox", name, tmp, "remix", "1-2").CombinedOutput()
				if err != nil {
					log.Printf("Failed to convert %v to mono (err=%v): %v. Do you have sox installed?", name, err, string(out))
					return
				}
				defer os.Remove(tmp)

				name = tmp
			}

			// (b) Upload

			obj := path.Join("tmp/audio", strings.ToLower(filepath.Base(name)))
			fd, err := os.Open(name)
			if err != nil {
				log.Printf("Failed to read %v: %v", name, err)
				return
			}
			defer fd.Close()

			if _, err := cl.Objects.Insert(*bucket, &storage.Object{Name: obj}).Media(fd).Do(); err != nil {
				log.Printf("Failed to upload %v: %v", name, err)
				return
			}
			defer cl.Objects.Delete(*bucket, obj).Do()

			// (c) Transcribe

			req := &speechpb.LongRunningRecognizeRequest{
				Config: &speechpb.RecognitionConfig{
					Encoding:        speechpb.RecognitionConfig_LINEAR16,
					SampleRateHertz: 44100,
					LanguageCode:    "en-US",
				},
				Audio: &speechpb.RecognitionAudio{
					AudioSource: &speechpb.RecognitionAudio_Uri{Uri: fmt.Sprintf("gs://%v/%v", *bucket, obj)},
				},
			}

			op, err := scl.LongRunningRecognize(context.Background(), req)
			if err != nil {
				log.Printf("Failed to transcribe %v: %v", name, err)
				return
			}
			resp, err := op.Wait(context.Background())
			if err != nil {
				log.Printf("Failed to transcribe %v: %v", name, err)
				return
			}

			// TODO(herohde) 6/11/2017: Add simple post-processing.

			var phrases []string
			for _, result := range resp.Results {
				for _, alt := range result.Alternatives {
					// Add text, if low confidence?
					phrases = append(phrases, alt.Transcript)
				}
			}
			data := strings.Join(phrases, " ")

			// (d) Write output

			// if err := ioutil.WriteFile(base+".raw.txt", []byte(data), 0644); err != nil {
			//    log.Printf("Failed to write raw output: %v", err)
			// }

			// data = strings.Replace(data, "paragraph", "\n", -1)
			// data = strings.Replace(data, "Paragraph", "\n", -1)
			data = strings.Replace(data, "  ", " ", -1)

			if err := ioutil.WriteFile(out, []byte(data), 0644); err != nil {
				log.Printf("Failed to write output: %v", err)
			}

			log.Printf("Transcribed %v", filepath.Base(name))
		}(name)
	}
	wg.Wait()

	log.Print("Done")
}

func newStorageClient(ctx context.Context) (*storage.Service, error) {
	httpClient, err := google.DefaultClient(context.Background(), storage.CloudPlatformScope)
	if err != nil {
		return nil, err
	}
	return storage.New(httpClient)
}
