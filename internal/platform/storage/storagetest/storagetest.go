// Package storagetest provides an in-memory S3-compatible server for tests:
// it accepts path-style and virtual-hosted keys, stores PUT bodies with their
// content type, and serves HEAD, GET, and DELETE.
package storagetest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

type object struct {
	contentType string
	body        []byte
}

// Server is a fake S3 endpoint.
type Server struct {
	*httptest.Server
	Bucket string

	mutex   sync.Mutex
	objects map[string]object
}

// New starts the fake endpoint for one bucket.
func New(bucket string) *Server {
	server := &Server{Bucket: bucket, objects: map[string]object{}}
	server.Server = httptest.NewServer(http.HandlerFunc(server.serve))
	return server
}

// Endpoint returns the base URL a storage client should be configured with.
func (s *Server) Endpoint() string { return s.URL }

// Count reports how many objects are stored.
func (s *Server) Count() int {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return len(s.objects)
}

// Keys returns the stored object keys.
func (s *Server) Keys() []string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	keys := make([]string, 0, len(s.objects))
	for key := range s.objects {
		keys = append(keys, key)
	}
	return keys
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	key, ok := s.key(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.mutex.Lock()
		s.objects[key] = object{contentType: r.Header.Get("Content-Type"), body: body}
		s.mutex.Unlock()
		w.Header().Set("ETag", `"fake"`)
		w.WriteHeader(http.StatusOK)
	case http.MethodHead:
		stored, found := s.get(key)
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", stored.contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(stored.body)))
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		stored, found := s.get(key)
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", stored.contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(stored.body)
	case http.MethodDelete:
		s.mutex.Lock()
		_, found := s.objects[key]
		delete(s.objects, key)
		s.mutex.Unlock()
		if !found {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) get(key string) (object, bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	stored, found := s.objects[key]
	return stored, found
}

// key resolves the object key from a path-style or virtual-hosted request.
func (s *Server) key(r *http.Request) (string, bool) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	if after, found := strings.CutPrefix(path, s.Bucket+"/"); found {
		return after, true
	}
	if strings.HasPrefix(r.Host, s.Bucket+".") {
		return path, path != ""
	}
	return "", false
}
