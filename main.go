package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	youtube "github.com/kkdai/youtube"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/storage"
	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"
)

type Phrase struct {
	Transcript string
	Time       float64
}

func main() {

	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "video-transcriber-309519-d926d8f680de.json")

	vIDs := []string{}
	gsURIs := []string{}
	f, _ := os.Open("./vIDs")
	defer f.Close()

	o, err := os.OpenFile("./results", os.O_RDWR, os.ModeAppend)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer o.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		vIDs = append(vIDs, scanner.Text())
	}

	paths := RetrieveAudioFromVideos(vIDs)
	for _, v := range paths {
		gsURI, err := UploadAudioToCloud(v)
		if err != nil {
			log.Fatal(err.Error())
		}
		gsURIs = append(gsURIs, gsURI)
	}

	results := []Phrase{}
	for _, v := range gsURIs {
		resp := ParseAudio(v)
		for _, v := range resp.GetResults() {
			highestConfidence := &speechpb.SpeechRecognitionAlternative{Confidence: 0}
			for _, alts := range v.GetAlternatives() {
				if alts.Confidence > highestConfidence.Confidence {
					highestConfidence = alts
				}
			}
			sort.Slice(highestConfidence.Words, func(i, j int) bool {
				return highestConfidence.Words[i].StartTime.Seconds < highestConfidence.Words[j].StartTime.Seconds
			})
			results = append(results, Phrase{Transcript: highestConfidence.Transcript, Time: highestConfidence.Words[0].StartTime.AsDuration().Seconds()})
		}
		sort.Slice(results, func(i, j int) bool {
			return results[i].Time < results[j].Time
		})
		for _, v := range results {
			o.WriteString(fmt.Sprintf("%s\n", v.Transcript))
		}
		o.WriteString("\n-------------------------------------\n")
	}

}

func ParseAudio(fileURI string) *speechpb.LongRunningRecognizeResponse {
	ctx := context.Background()

	client, err := speech.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	req := &speechpb.LongRunningRecognizeRequest{
		Config: &speechpb.RecognitionConfig{
			Encoding:                   speechpb.RecognitionConfig_FLAC,
			SampleRateHertz:            44100,
			LanguageCode:               "en-US",
			AudioChannelCount:          1,
			EnableAutomaticPunctuation: true,
			EnableWordTimeOffsets:      true,
			SpeechContexts: []*speechpb.SpeechContext{
				&speechpb.SpeechContext{
					Phrases: []string{
						"Plato", "Aristotle", "Moral", "Ethical", "ethics",
						"philosophy", "principles", "friends", "friend", "kant", "natural",
						"forms",
					},
				},
			},
		},
		Audio: &speechpb.RecognitionAudio{
			AudioSource: &speechpb.RecognitionAudio_Uri{
				Uri: fileURI,
			},
		},
	}
	op, err := client.LongRunningRecognize(ctx, req)
	if err != nil {
		fmt.Println(err.Error())
	}

	resp, err := op.Wait(ctx)
	if err != nil {
		fmt.Println(err.Error())
	}

	return resp

}

func RetrieveAudioFromVideos(uris []string) []string {
	filePaths := []string{}
	for _, vID := range uris {
		y := youtube.NewYoutube(true, false)
		y.DecodeURL(vID)
		title := strings.ReplaceAll(y.Title, " ", "")
		if err := y.StartDownload("./static", fmt.Sprintf("%s.mp4", title), "", 0); err != nil {
			fmt.Println(y.Title)
		}
		exec.Command("ffmpeg", "-i", fmt.Sprintf("./static/%s.mp4", title), fmt.Sprintf("./static/%s.flac", title)).Run()
		filePaths = append(filePaths, fmt.Sprintf("./static/%s.flac", title))

	}

	return filePaths
}

func UploadAudioToCloud(path string) (string, error) {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("storage.NewClient: %v", err)
	}
	defer client.Close()

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("os.Open: %v", err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(ctx, time.Second*60)
	defer cancel()

	wc := client.Bucket("gs-transcriber-audio-files").Object(strings.Split(path, "/")[2]).NewWriter(ctx)
	if _, err = io.Copy(wc, f); err != nil {
		return "", fmt.Errorf("io.Copy: %v", err)
	}
	if err := wc.Close(); err != nil {
		return "", fmt.Errorf("Writer.Close: %v", err)
	}

	return fmt.Sprintf("gs://%s/%s", "gs-transcriber-audio-files", strings.Split(path, "/")[2]), nil
}
