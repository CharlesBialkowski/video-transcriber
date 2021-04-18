package api

import "github.com/julienschmidt/httprouter"

func (s *Server) Routes() *httprouter.Router {
	router := httprouter.New()

	router.POST("/transcribe", s.Validate(s.HandleTranscribeRequest()))

	return router
}
