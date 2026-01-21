package main

import (
	"errors"
	"regexp"
)

var profaneWords = []string{"kerfuffle", "sharbert", "fornax"}

func cleanProfanity(body string) string {
	for _, word := range profaneWords {
		regex := regexp.MustCompile(`\b(?i)` + word + `\b`)
		body = regex.ReplaceAllString(body, "****")
	}
	return body
}

func validateChirp(body string) (string, error) {
	const maxChirpLength = 140
	if len(body) > maxChirpLength {
		return "", errors.New("Chirp is too long")
	}

	cleaned := cleanProfanity(body)
	return cleaned, nil
}
