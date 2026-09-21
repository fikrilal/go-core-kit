package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	contentTypeJSON    = "application/json"
	contentTypeProblem = "application/problem+json"
)

type envelope struct {
	Data any `json:"data"`
	Meta any `json:"meta,omitempty"`
}

// ListMeta is the contract metadata carried by list responses.
type ListMeta struct {
	NextCursor string `json:"nextCursor,omitempty"`
	Limit      int    `json:"limit"`
	HasMore    bool   `json:"hasMore"`
}

// Data writes a 200 response with the contract envelope.
func Data(w http.ResponseWriter, r *http.Request, v any) {
	writeJSON(w, r, http.StatusOK, envelope{Data: v})
}

// List writes a 200 list response with the contract envelope and metadata.
func List(w http.ResponseWriter, r *http.Request, items any, meta ListMeta) {
	writeJSON(w, r, http.StatusOK, envelope{Data: items, Meta: meta})
}

// Created writes a 201 response with a Location header and the contract envelope.
func Created(w http.ResponseWriter, r *http.Request, location string, v any) {
	w.Header().Set("Location", location)
	writeJSON(w, r, http.StatusCreated, envelope{Data: v})
}

// NoContent writes a 204 response with no body.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Document writes a 200 non-enveloped JSON document; used by contract
// endpoints whose payload is a standard document, such as the JWKS.
func Document(w http.ResponseWriter, r *http.Request, v any) {
	writeJSON(w, r, http.StatusOK, v)
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		WriteError(w, r, fmt.Errorf("marshal response: %w", err))
		return
	}
	writeBody(w, status, contentTypeJSON, body)
}

func writeBody(w http.ResponseWriter, status int, contentType string, body []byte) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
