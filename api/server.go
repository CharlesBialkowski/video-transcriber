package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"video-transcriber/domain"
	"video-transcriber/infrastructure"

	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

type Server struct {
	Db     *gorm.DB
	Router httprouter.Router
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h, p, _ := s.Router.Lookup(r.Method, r.URL.Path)
	if h != nil {
		h(w, r, p)
		return
	}
	s.Response(w, r, Error{Code: 404, Message: "Path not found.", Function: "ServeHTTP", Input: r.URL.Path}, 404)
}

type Error struct {
	Code     int
	Message  string
	Function string
	Input    string
}

func Init() *Server {

	db, _ := infrastructure.Connect(context.TODO())

	server := &Server{Db: db}

	router := server.Routes()

	server.Router = *router

	return server

}

func (s *Server) SendNotifications(n domain.Notification, recipients []uint, r http.Request) {
	client := http.Client{}
	for _, v := range recipients {
		n.Profile.ID = v
		json, _ := json.Marshal(n)
		token := strings.Split(r.Header.Get("Authorization"), " ")[1]
		req, _ := http.NewRequestWithContext(r.Context(),
			"POST",
			fmt.Sprintf("%s/publish?access_token=%s", os.Getenv("NOTIFICATION_SERVICE"), token),
			bytes.NewBuffer(json),
		)
		client.Do(req)
	}
}

func (s *Server) Error(code int, message string, function string, input interface{}) Error {
	inputJSON, _ := json.MarshalIndent(input, "", "    ")
	return Error{
		Code:     code,
		Message:  message,
		Function: function,
		Input:    string(inputJSON),
	}
}

func (s *Server) Decode(w http.ResponseWriter, r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) Response(w http.ResponseWriter, r *http.Request, i interface{}, code int) {
	w.WriteHeader(code)
	if i != nil {
		err := json.NewEncoder(w).Encode(i)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Couldn't encode response data."))
		}
	}
}

func (s *Server) AwaitForShutdown(ctx context.Context, server *http.Server, serverDone chan error, shutdownApplication context.CancelFunc) {
	select {
	case <-ctx.Done():
		s.ShutdownServerGracefully(server)
	case serverError := <-serverDone:
		if serverError != nil {
			log.Error().Err(serverError).Msg("Server returned with error")
		}
		shutdownApplication()
	}
}

func (s *Server) ShutdownServerGracefully(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		// extra handling here
		cancel()
	}()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("Could not shutdown server gracefully")
	}
}

func (s *Server) HandleShutdownSignals(cancel context.CancelFunc) {
	done := make(chan struct{})
	go func() {
		log.Info().Msg("Listening signals...")
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		close(done)
	}()
	go func() {
		<-done
		log.Info().Msg("Shutting down")
		cancel()
	}()
}
