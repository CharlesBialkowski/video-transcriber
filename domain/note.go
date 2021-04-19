package domain

type Note struct {
	ID uint

	Profile   Profile
	ProfileID uint

	Title   string
	Phrases []Phrase
}
