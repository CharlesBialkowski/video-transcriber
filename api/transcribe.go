package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
	"video-transcriber/domain"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/storage"
	"github.com/julienschmidt/httprouter"
	"github.com/kkdai/youtube"
	"github.com/umahmood/soundex"
	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"
)

func (s *Server) HandleTranscribeRequest() httprouter.Handle {
	type Input struct {
		Links               []string
		ComparisonThreshold float64
	}

	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {

		requester := r.Context().Value("id").(uint)
		input := &Input{}
		err := s.Decode(w, r, input)
		if err != nil {
			s.Response(
				w, r,
				s.Error(http.StatusBadRequest, err.Error(), "HandleTranscribeRequest", input.Links),
				http.StatusBadRequest,
			)
			return
		}

		speechContext := &speechpb.SpeechContext{Phrases: []string{}}
		notes := []*domain.Note{}

		for _, v := range input.Links {
			s.SendNotifications(domain.Notification{
				ProfileID: requester,
				CreatedAt: time.Now(),
				Process:   "transcribe_request",
				Content:   fmt.Sprintf(`{"links_total": , "links_done": , "current_link": }`),
			}, []uint{requester}, *r)
			path, title := s.DownloadFLAC(v, requester, r)
			gsURI, _ := s.UploadAudio(path, requester, r)
			response := s.Recognize(gsURI, speechContext, requester, r)
			note := s.CreateNote(response, title, requester, r)
			notes = append(notes, note)
			speechContext = s.CompareNotes(notes, input.ComparisonThreshold, requester, r)
		}

		for _, v := range notes {
			v.ProfileID = requester
			s.Db.Create(v)
		}

	}
}

func (s *Server) CreateNote(response *speechpb.LongRunningRecognizeResponse, title string, requester uint, r *http.Request) *domain.Note {

	note := &domain.Note{
		Title:   title,
		Phrases: []domain.Phrase{},
	}

	s.SendNotifications(domain.Notification{
		ProfileID: requester,
		CreatedAt: time.Now(),
		Process:   "create_note",
		Content:   fmt.Sprintf(`{"title": }`),
	}, []uint{requester}, *r)

	defer s.SendNotifications(domain.Notification{
		ProfileID: requester,
		CreatedAt: time.Now(),
		Process:   "create_note_done",
		Content:   fmt.Sprintf(`{"title": }`),
	}, []uint{requester}, *r)

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

		note.Phrases = append(note.Phrases, domain.Phrase{
			Transcript: mostConfidentAlternative.Transcript,
			Time:       mostConfidentAlternative.Words[0].GetStartTime().AsDuration().Seconds(),
			Confidence: float64(mostConfidentAlternative.Confidence),
			Words:      mostConfidentAlternative.Words,
		})

	}

	sort.Slice(note.Phrases, func(i, j int) bool {
		return note.Phrases[i].Time < note.Phrases[j].Time
	})

	for _, phrase := range note.Phrases {
		phrase.SoundexMap = make(map[string]*speechpb.WordInfo, 0)
		for _, word := range phrase.Words {
			phrase.SoundexMap[soundex.Code(word.GetWord())] = word
		}
	}

	return note
}

func (s *Server) WriteNote(note *domain.Note, requester uint, r *http.Request) {
	s.SendNotifications(domain.Notification{
		ProfileID: requester,
		CreatedAt: time.Now(),
		Process:   "write_note",
		Content:   fmt.Sprintf(`{"title": "%s"}`, note.Title),
	}, []uint{requester}, *r)

	defer s.SendNotifications(domain.Notification{
		ProfileID: requester,
		CreatedAt: time.Now(),
		Process:   "write_note_done",
		Content:   fmt.Sprintf(`{"title": %s}`, note.Title),
	}, []uint{requester}, *r)

	fileName := fmt.Sprintf("./results/%s", note.Title)

	o, err := os.OpenFile(fileName, os.O_CREATE, os.ModeAppend)
	if err != nil {
		log.Fatal(err.Error())
	}
	defer o.Close()

	o.WriteString(note.Title)
	o.WriteString("\n-------------------------------------\n")

	for _, v := range note.Phrases {
		o.WriteString(fmt.Sprintf("%s\n", v.Transcript))
	}
}

func (s *Server) CompareNotes(notes []*domain.Note, threshold float64, requester uint, r *http.Request) *speechpb.SpeechContext {

	speechContext := &speechpb.SpeechContext{Phrases: []string{}}
	notConfident := []domain.Phrase{}
	confident := []domain.Phrase{}
	for _, note := range notes {
		for _, phrase := range note.Phrases {
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
			for soundex := range not.SoundexMap {
				if word, ok := is.SoundexMap[soundex]; ok {
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

func (s *Server) Recognize(fileURI string, speechContext *speechpb.SpeechContext, requester uint, r *http.Request) *speechpb.LongRunningRecognizeResponse {
	s.SendNotifications(domain.Notification{
		ProfileID: requester,
		CreatedAt: time.Now(),
		Process:   "recognize",
		Content:   fmt.Sprintf(`{"title": "%s"}`, fileURI),
	}, []uint{requester}, *r)

	defer s.SendNotifications(domain.Notification{
		ProfileID: requester,
		CreatedAt: time.Now(),
		Process:   "recognize_done",
		Content:   fmt.Sprintf(`{"title": "%s"}`, fileURI),
	}, []uint{requester}, *r)

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
		req.Config.AudioChannelCount = 2
		op, _ = client.LongRunningRecognize(ctx, req)
	}

	resp, err := op.Wait(ctx)
	if err != nil {
		req.Config.AudioChannelCount = 2
		op, _ = client.LongRunningRecognize(ctx, req)
		resp, _ = op.Wait(ctx)
	}

	return resp

}

func (s *Server) DownloadFLAC(uri string, requester uint, r *http.Request) (string, string) {
	s.SendNotifications(domain.Notification{
		ProfileID: requester,
		CreatedAt: time.Now(),
		Process:   "get_audio",
		Content:   fmt.Sprintf(`{"uri": }`),
	}, []uint{requester}, *r)

	defer s.SendNotifications(domain.Notification{
		ProfileID: requester,
		CreatedAt: time.Now(),
		Process:   "get_audio_done",
		Content:   fmt.Sprintf(`{"uri": }`),
	}, []uint{requester}, *r)

	y := youtube.NewYoutube(true, false)
	y.DecodeURL(uri)
	title := strings.ReplaceAll(y.Title, " ", "")
	if err := y.StartDownload("./static", fmt.Sprintf("%s.mp4", title), "", 0); err != nil {
		fmt.Println(y.Title)
	}
	exec.Command("ffmpeg", "-i", fmt.Sprintf("./static/%s.mp4", title), fmt.Sprintf("./static/%s.flac", title)).Run()
	return fmt.Sprintf("./static/%s.flac", title), y.Title
}

func (s *Server) UploadAudio(path string, requester uint, r *http.Request) (string, error) {
	s.SendNotifications(domain.Notification{
		ProfileID: requester,
		CreatedAt: time.Now(),
		Process:   "upload_audio",
		Content:   fmt.Sprintf(``),
	}, []uint{requester}, *r)

	defer s.SendNotifications(domain.Notification{
		ProfileID: requester,
		CreatedAt: time.Now(),
		Process:   "upload_audio_done",
		Content:   fmt.Sprintf(``),
	}, []uint{requester}, *r)

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
