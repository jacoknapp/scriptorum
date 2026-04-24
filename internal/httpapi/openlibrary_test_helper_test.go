package httpapi

import (
	"net/http"
	"testing"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

func installOpenLibraryTestClient(t *testing.T, fn roundTripFunc) {
	t.Helper()
	restore := providers.TestSetOpenLibraryHTTPClientFactory(func() *http.Client {
		return &http.Client{
			Timeout:   10 * time.Second,
			Transport: fn,
		}
	})
	t.Cleanup(restore)
}
