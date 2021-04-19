package infrastructure

import (
	"context"
	"fmt"
	"os"
	"time"

	"video-transcriber/domain"

	_ "github.com/lib/pq"
	"github.com/rs/zerolog/log"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Connect initializes the DB connection based on the current environment.
func Connect(ctx context.Context) (db *gorm.DB, err error) {
	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context is done, giving up on db connection")
		default:
			db, err = gorm.Open(gormpostgres.Open(os.Getenv("DB_CONNECTION_STRING")), &gorm.Config{})
			if err == nil {
				return
			} else {
				log.Warn().Err(err).Msg("could not connect to DB")
			}
		}
		time.Sleep(1 * time.Second)

	}
}

func CreateTables(db *gorm.DB) error {
	err := db.AutoMigrate(
		&domain.Phrase{},
		&domain.Note{},
		&domain.Profile{},
		&domain.Notification{})

	if err != nil {
		return err
	}

	return nil
}
