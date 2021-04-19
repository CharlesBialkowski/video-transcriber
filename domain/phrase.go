package domain

import speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"

type Phrase struct {
	ID uint

	NoteID uint

	SoundexMap map[string]*speechpb.WordInfo `gorm:"-"`
	Words      []*speechpb.WordInfo          `gorm:"-"`
	Transcript string
	Time       float64
	Confidence float64
}
