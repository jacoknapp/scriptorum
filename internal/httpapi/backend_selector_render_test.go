package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestBackendSelectorRendersOnSettingsAndSetup confirms the book_backend
// selector and the restored Readarr fields render on both settings.html and
// the setup wizard's step_readarr.html, catching template errors or field
// name drift between the Go handlers and the markup.
func TestBackendSelectorRendersOnSettingsAndSetup(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	req := httptest.NewRequest("GET", "/settings", nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET /settings code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="book_backend" value="chaptarr"`, `name="book_backend" value="readarr"`,
		`name="ra_ebooks_base"`, `name="ra_audio_base"`, `onclick="testReadarr('ebooks')"`,
		`data-backend-radio`, `data-backend-section="chaptarr"`, `data-backend-section="readarr"`,
		`function applyBackendSectionVisibility`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("settings.html missing %q", want)
		}
	}

	setupSrv := makeTestServer(t)
	setupSrv.disableCSRF = true
	setupRouter := setupSrv.Router()
	stepReq := httptest.NewRequest("GET", "/setup/step/3", nil)
	stepRec := httptest.NewRecorder()
	setupRouter.ServeHTTP(stepRec, stepReq)
	if stepRec.Code != 200 {
		t.Fatalf("GET /setup/step/3 code=%d body=%s", stepRec.Code, stepRec.Body.String())
	}
	stepBody := stepRec.Body.String()
	for _, want := range []string{
		`name="book_backend" value="chaptarr"`, `name="book_backend" value="readarr"`,
		`name="ra_ebooks_base"`, `name="ra_audio_base"`, `hx-post="/setup/test/readarr?tag=ebooks"`,
		`data-backend-radio`, `data-backend-section="chaptarr"`, `data-backend-section="readarr"`,
		`function applyBackendSectionVisibility`,
	} {
		if !strings.Contains(stepBody, want) {
			t.Fatalf("step_readarr.html missing %q", want)
		}
	}
}
