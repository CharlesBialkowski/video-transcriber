package domain

import speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"

type Phrase struct {
	SoundexMap map[string]*speechpb.WordInfo
	Words      []*speechpb.WordInfo
	Transcript string
	Time       float64
	Confidence float64
}
