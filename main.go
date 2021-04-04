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
	"github.com/umahmood/soundex"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/storage"
	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"
)

type Phrase struct {
	SoundexMap map[string]*speechpb.WordInfo
	Words      []*speechpb.WordInfo
	Transcript string
	Time       float64
	Confidence float64
}

type Note struct {
	Title   string
	Results []*Phrase
}

func main() {

	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "video-transcriber-309519-d926d8f680de.json")

	vIDs := []string{}
	f, _ := os.Open("./vIDs")
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		vIDs = append(vIDs, scanner.Text())
	}

	speechContext := &speechpb.SpeechContext{Phrases: []string{}}
	notes := []*Note{}
	for _, v := range vIDs {
		path, title := DownloadFLAC(v)
		gsURI, _ := UploadAudio(path)
		response := Recognize(gsURI, speechContext)
		note := CreateNote(response, title)
		notes = append(notes, note)
		speechContext = CompareNotes(notes, .93)
	}

	for _, v := range notes {
		WriteNote(v)
	}
}

func CreateNote(response *speechpb.LongRunningRecognizeResponse, title string) *Note {

	note := &Note{
		Title:   title,
		Results: []*Phrase{},
	}

	for _, result := range response.GetResults() {

		mostConfidentAlternative := &speechpb.SpeechRecognitionAlternative{Confidence: 0}
		for _, alts := range result.GetAlternatives() {
			if alts.Confidence > mostConfidentAlternative.Confidence {
				mostConfidentAlternative = alts
			}
		}

		sort.Slice(mostConfidentAlternative.Words, func(i, j int) bool {
			return mostConfidentAlternative.Words[i].StartTime.Seconds < mostConfidentAlternative.Words[j].StartTime.Seconds
		})

		note.Results = append(note.Results, &Phrase{
			Transcript: mostConfidentAlternative.Transcript,
			Time:       mostConfidentAlternative.Words[0].GetStartTime().AsDuration().Seconds(),
			Confidence: float64(mostConfidentAlternative.Confidence),
			Words:      mostConfidentAlternative.Words,
		})

	}

	sort.Slice(note.Results, func(i, j int) bool {
		return note.Results[i].Time < note.Results[j].Time
	})

	for _, phrase := range note.Results {
		phrase.SoundexMap = make(map[string]*speechpb.WordInfo, 0)
		for _, word := range phrase.Words {
			phrase.SoundexMap[soundex.Code(word.GetWord())] = word
		}
		log.Printf("%v\v", phrase.SoundexMap)
	}

	return note
}

func WriteNote(note *Note) {
	log.Printf("Writing note %s ...", note.Title)
	defer log.Printf("Done!\n\n----------------------\n\n")

	fileName := fmt.Sprintf("./results/%s", note.Title)

	o, err := os.OpenFile(fileName, os.O_CREATE, os.ModeAppend)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer o.Close()

	o.WriteString(note.Title)
	o.WriteString("\n-------------------------------------\n")

	for _, v := range note.Results {
		o.WriteString(fmt.Sprintf("%s\n", v.Transcript))
	}
}

func CompareNotes(notes []*Note, threshold float64) *speechpb.SpeechContext {

	log.Printf("Comparing existing notes ...")
	defer log.Printf("Done!\n\n----------------------\n\n")

	speechContext := &speechpb.SpeechContext{Phrases: []string{}}
	notConfident := []*Phrase{}
	confident := []*Phrase{}
	for _, note := range notes {
		for _, phrase := range note.Results {
			if phrase.Confidence < threshold {
				notConfident = append(notConfident, phrase)
			} else {
				confident = append(confident, phrase)
			}
		}
	}

	found := map[string]interface{}{}
	for _, not := range notConfident {
		for _, is := range confident {
			for soundex, notWord := range not.SoundexMap {
				if word, ok := is.SoundexMap[soundex]; ok {
					log.Printf("Better match found: (%s, %s)\n", notWord.GetWord(), word.GetWord())
					if _, exists := found[word.Word]; !exists {
						found[word.Word] = nil
					}
				}
			}
		}
	}

	for word := range found {
		speechContext.Phrases = append(speechContext.Phrases, word)
	}

	return speechContext

}

func Recognize(fileURI string, speechContext *speechpb.SpeechContext) *speechpb.LongRunningRecognizeResponse {
	log.Printf("Sending audio to Google STT ...")
	defer log.Printf("Done!\n\n----------------------\n\n")

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
			SpeechContexts:             []*speechpb.SpeechContext{speechContext},
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

func DownloadFLAC(uri string) (string, string) {
	log.Printf("Retrieving Audio File ...")
	defer log.Printf("Done!\n\n----------------------\n\n")

	y := youtube.NewYoutube(true, false)
	y.DecodeURL(uri)
	title := strings.ReplaceAll(y.Title, " ", "")
	if err := y.StartDownload("./static", fmt.Sprintf("%s.mp4", title), "", 0); err != nil {
		fmt.Println(y.Title)
	}
	exec.Command("ffmpeg", "-i", fmt.Sprintf("./static/%s.mp4", title), fmt.Sprintf("./static/%s.flac", title)).Run()
	return fmt.Sprintf("./static/%s.flac", title), y.Title
}

func UploadAudio(path string) (string, error) {
	log.Printf("Uploading to cloud storage for analysis ...")
	defer log.Printf("Done!\n\n----------------------\n\n")

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

	ctx, cancel := context.WithTimeout(ctx, time.Minute*5)
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
