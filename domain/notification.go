package domain

import "time"

type Notification struct {
	ID        uint
	CreatedAt time.Time

	Profile   Profile
	ProfileID uint

	Process string
	Content string
}
