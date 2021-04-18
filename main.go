package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"video-transcriber/api"
	"video-transcriber/infrastructure"

	"github.com/rs/cors"
	"github.com/rs/zerolog/log"
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

	server := api.Init()

	ctx, shutdownApplication := context.WithCancel(context.Background())
	server.HandleShutdownSignals(shutdownApplication)

	migrationType := os.Getenv("DB_MIGRATION")
	if migrationType != "" {

		err := infrastructure.CreateTables(server.Db)

		if err != nil {
			log.Fatal().Err(err).Msg("Could not run migrations")
		}

	}

	port := os.Getenv("SERVER_PORT")
	if port == "" {
		port = "80"
	}

	httpServer := &http.Server{Addr: fmt.Sprintf(":%s", port), Handler: cors.AllowAll().Handler(server)}
	serverDone := make(chan error)
	go func() {
		serverDone <- httpServer.ListenAndServe()
	}()
	log.Info().Msg("Server is ready for requests!")

	server.AwaitForShutdown(ctx, httpServer, serverDone, shutdownApplication)

}
