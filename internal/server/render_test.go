package server

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func TestInitTemplates(t *testing.T) {
	t.Run("success with minimal fs", func(t *testing.T) {
		if err := InitTemplates(makeTestFS()); err != nil {
			t.Fatalf("InitTemplates: %v", err)
		}
	})

	t.Run("error on missing template files", func(t *testing.T) {
		empty := fstest.MapFS{}
		if err := InitTemplates(empty); err == nil {
			t.Error("want error for empty FS, got nil")
		}
	})

	t.Run("success with real project templates", func(t *testing.T) {
		// os.DirFS resolves relative to the package dir (internal/server),
		// so ../../ is the project root.
		rootFS := os.DirFS("../../")
		if err := InitTemplates(rootFS); err != nil {
			t.Fatalf("InitTemplates with real templates: %v", err)
		}
	})

	// Restore test templates so other tests are not affected.
	if err := InitTemplates(makeTestFS()); err != nil {
		t.Fatalf("restore test templates: %v", err)
	}
}

func TestDictFunc(t *testing.T) {
	t.Run("valid call returns map", func(t *testing.T) {
		m, err := dictFunc("key", "value", "n", 42)
		if err != nil {
			t.Fatalf("dictFunc: %v", err)
		}
		if m["key"] != "value" {
			t.Errorf("m[key] = %v, want value", m["key"])
		}
		if m["n"] != 42 {
			t.Errorf("m[n] = %v, want 42", m["n"])
		}
	})

	t.Run("odd number of args returns error", func(t *testing.T) {
		_, err := dictFunc("key")
		if err == nil {
			t.Error("want error for odd args, got nil")
		}
	})

	t.Run("non-string key returns error", func(t *testing.T) {
		_, err := dictFunc(123, "value")
		if err == nil {
			t.Error("want error for non-string key, got nil")
		}
	})
}

func TestRenderPage(t *testing.T) {
	if err := InitTemplates(makeTestFS()); err != nil {
		t.Fatal(err)
	}

	t.Run("sets Content-Type and executes template", func(t *testing.T) {
		w := httptest.NewRecorder()
		renderPage(w, "index.html", map[string]any{"Repos": nil})
		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
		if body := w.Body.String(); body == "" {
			t.Error("body should not be empty")
		}
	})
}

func TestRenderPartial(t *testing.T) {
	if err := InitTemplates(makeTestFS()); err != nil {
		t.Fatal(err)
	}

	t.Run("renders partial with correct content type", func(t *testing.T) {
		w := httptest.NewRecorder()
		renderPartial(w, "repo_list.html", map[string]any{"Repos": nil})
		if w.Code != 200 {
			t.Errorf("status = %d, want 200", w.Code)
		}
		ct := w.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
	})
}
