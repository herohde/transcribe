// Package transcribe is a convenience library for Google Speech API.
package transcribe

import (
	"cloud.google.com/go/speech/apiv1"
	"fmt"

	"context"
	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"
	"strings"
)

// Submit transcribes an 44.1kHz wav file (uploaded to GCS) via the Google Speech
// API. The call is blocking. It returns a list of phrases.
func Submit(ctx context.Context, cl *speech.Client, bucket, object string) ([]string, error) {
	req := &speechpb.LongRunningRecognizeRequest{
		Config: &speechpb.RecognitionConfig{
			Encoding:        speechpb.RecognitionConfig_LINEAR16,
			SampleRateHertz: 44100,
			LanguageCode:    "en-US",
		},
		Audio: &speechpb.RecognitionAudio{
			AudioSource: &speechpb.RecognitionAudio_Uri{Uri: fmt.Sprintf("gs://%v/%v", bucket, object)},
		},
	}

	op, err := cl.LongRunningRecognize(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %v", err)
	}
	resp, err := op.Wait(ctx)
	if err != nil {
		return nil, fmt.Errorf("transcribe failed: %v", err)
	}

	var phrases []string
	for _, result := range resp.Results {
		// We submit requests which return exactly 1 alternative for each
		// phrase. So we don't have to handle "alternatives" in any real sense.
		for _, alt := range result.Alternatives {
			// TODO(herohde) 6/16//2017: Add extra text, if low confidence?
			phrases = append(phrases, alt.Transcript)
		}
	}
	return phrases, nil
}

// PostProcess cleans up the phrases and concatenates them to a single text.
// For now, such post-processing is trivial.
func PostProcess(phrases []string) string {

	// TODO(herohde) 6/11/2017: Add configurable post-processing.
	data := strings.Join(phrases, " ")

	data = strings.Replace(data, "  ", " ", -1)
	data = strings.Replace(data, "\n ", "\n", -1)

	return data
}
