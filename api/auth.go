package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"video-transcriber/domain"

	"github.com/dgrijalva/jwt-go"
	"github.com/julienschmidt/httprouter"
)

func (s *Server) HandleMicrosoftLogin() httprouter.Handle {

	type Input struct {
		IDToken string
	}

	type Output struct {
		JWT string
	}

	type MicrosoftClaims struct {
		Id    uint
		Name  string
		Email string
		jwt.StandardClaims
	}

	type Claims struct {
		Id uint
		jwt.StandardClaims
	}

	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		input := &Input{}
		output := &Output{}

		err := s.Decode(w, r, input)
		if err != nil {
			s.Response(
				w, r,
				s.Error(http.StatusBadRequest, err.Error(), "HandleMicrosoftLogin", input),
				http.StatusBadRequest,
			)
			return
		}

		microsoftClaims := &MicrosoftClaims{}
		_, err = jwt.ParseWithClaims(input.IDToken, microsoftClaims, func(token *jwt.Token) (interface{}, error) {
			return os.Getenv("MICROSOFT_SECRET"), nil
		})

		profile := &domain.Profile{
			Name:           microsoftClaims.Name,
			MicrosoftEmail: microsoftClaims.Email,
		}
		db := s.Db.Where(&domain.Profile{MicrosoftEmail: microsoftClaims.Email}).FirstOrCreate(profile)
		if db.Error != nil {
			s.Response(
				w, r,
				s.Error(http.StatusInternalServerError, db.Error.Error(), "HandleMicrosoftLogin", input),
				http.StatusInternalServerError,
			)
			return
		}

		accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
			Id: profile.ID,
			StandardClaims: jwt.StandardClaims{
				ExpiresAt: time.Now().Add(time.Minute * 60).Unix(),
				Issuer:    "video-transcriber",
			},
		})

		accessString, err := accessToken.SignedString([]byte(os.Getenv("ACCESS_KEY")))
		output.JWT = accessString
		if err != nil {
			s.Response(
				w, r,
				s.Error(http.StatusInternalServerError, err.Error(), "HandleMicrosoftLogin", input),
				http.StatusInternalServerError,
			)
			return
		}

		s.Response(w, r, output, http.StatusOK)
	}
}

func (s *Server) Validate(h httprouter.Handle) httprouter.Handle {
	type Claims struct {
		Id uint
		jwt.StandardClaims
	}

	type Output struct {
		JWT string
	}

	return func(w http.ResponseWriter, r *http.Request, p httprouter.Params) {
		output := &Output{}

		header := r.Header.Get("Authorization")
		authHeader := strings.Split(header, " ")
		token := ""
		if len(authHeader) > 1 {
			token = authHeader[1]
		} else {
			s.Response(
				w, r,
				s.Error(http.StatusInternalServerError, "Bad Authorization header.", "Validate", header),
				http.StatusInternalServerError,
			)
			return
		}

		parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
			}
			return os.Getenv("ACCESS_KEY"), nil
		})

		err = parsed.Claims.Valid()
		if err != nil {
			s.Response(
				w, r,
				s.Error(http.StatusUnauthorized, err.Error(), "Validate", token),
				http.StatusUnauthorized,
			)
			return
		}

		claims, ok := parsed.Claims.(*Claims)
		if !ok {
			s.Response(
				w, r,
				s.Error(http.StatusUnauthorized, err.Error(), "Validate", token),
				http.StatusUnauthorized,
			)
			return
		}

		accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
			Id: claims.Id,
			StandardClaims: jwt.StandardClaims{
				ExpiresAt: time.Now().Add(time.Minute * 60).Unix(),
				Issuer:    "video-transcriber",
			},
		})

		accessString, err := accessToken.SignedString([]byte(os.Getenv("ACCESS_KEY")))
		output.JWT = accessString
		if err != nil {
			s.Response(
				w, r,
				s.Error(http.StatusInternalServerError, err.Error(), "Validate", accessToken),
				http.StatusInternalServerError,
			)
			return
		}

		w.Header().Set("Authorization", "Bearer "+accessString)

		ctx := context.WithValue(r.Context(), "id", claims.Id)
		r = r.WithContext(ctx)

		h(w, r, p)

	}
}
