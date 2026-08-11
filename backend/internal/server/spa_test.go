package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestServeSPAProvidesPublicImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<div id=\"app\"></div>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dist, "images"), 0o700); err != nil {
		t.Fatal(err)
	}
	image := []byte("synthetic-webp")
	if err := os.WriteFile(filepath.Join(dist, "images", "workbench.webp"), image, 0o600); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	serveSPA(router, dist)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/images/workbench.webp", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if contentType := recorder.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "image/webp") {
		t.Fatalf("content-type=%q", contentType)
	}
	if !bytes.Equal(recorder.Body.Bytes(), image) {
		t.Fatalf("body=%q", recorder.Body.Bytes())
	}
}

func TestServeSPADoesNotFallbackForMissingStaticFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dist := t.TempDir()
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<div id=\"app\"></div>"), 0o600); err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	serveSPA(router, dist)

	for _, path := range []string{"/assets/missing.js", "/images/missing.webp"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("path=%s status=%d body=%s", path, recorder.Code, recorder.Body.String())
		}
	}
}
