package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Test withUser middleware
func TestWithUserMiddleware(t *testing.T) {
	server := newServerForTest(t)
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := r.Context().Value(ctxUser)
		if user != nil {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("user found"))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("no user"))
		}
	})
	
	// Wrap with withUser middleware
	wrappedHandler := server.withUser(handler)
	
	req := httptest.NewRequest("GET", "/test", nil)
	recorder := httptest.NewRecorder()
	
	wrappedHandler.ServeHTTP(recorder, req)
	
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", recorder.Code)
	}
	
	// Without OIDC setup, should have "no user"
	if recorder.Body.String() != "no user" {
		t.Errorf("Expected 'no user', got %s", recorder.Body.String())
	}
}

// Test requireLogin middleware
func TestRequireLoginMiddleware(t *testing.T) {
	server := newServerForTest(t)
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("protected content"))
	})
	
	// Wrap with requireLogin middleware
	wrappedHandler := server.requireLogin(handler)
	
	// Test without login
	req := httptest.NewRequest("GET", "/protected", nil)
	recorder := httptest.NewRecorder()
	
	wrappedHandler.ServeHTTP(recorder, req)
	
	// Should redirect to login
	if recorder.Code != http.StatusFound {
		t.Errorf("Expected redirect status, got %d", recorder.Code)
	}
	
	location := recorder.Header().Get("Location")
	if location != "/login" {
		t.Errorf("Expected redirect to /login, got %s", location)
	}
}

// Test requireLogin middleware with user
func TestRequireLoginMiddlewareWithUser(t *testing.T) {
	server := newServerForTest(t)
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("protected content"))
	})
	
	// Wrap with requireLogin middleware
	wrappedHandler := server.requireLogin(handler)
	
	// Create request with user context
	req := httptest.NewRequest("GET", "/protected", nil)
	mockSession := &session{Username: "testuser", Admin: false}
	ctx := context.WithValue(req.Context(), ctxUser, mockSession)
	req = req.WithContext(ctx)
	
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)
	
	// Should allow access
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected OK status, got %d", recorder.Code)
	}
	
	if recorder.Body.String() != "protected content" {
		t.Errorf("Expected 'protected content', got %s", recorder.Body.String())
	}
}

// Test requireAdmin middleware
func TestRequireAdminMiddleware(t *testing.T) {
	server := newServerForTest(t)
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("admin content"))
	})
	
	// Wrap with requireAdmin middleware
	wrappedHandler := server.requireAdmin(handler)
	
	// Test without user
	req := httptest.NewRequest("GET", "/admin", nil)
	recorder := httptest.NewRecorder()
	
	wrappedHandler.ServeHTTP(recorder, req)
	
	// Should return forbidden
	if recorder.Code != http.StatusForbidden {
		t.Errorf("Expected forbidden status, got %d", recorder.Code)
	}
}

// Test requireAdmin middleware with non-admin user
func TestRequireAdminMiddlewareWithNonAdmin(t *testing.T) {
	server := newServerForTest(t)
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("admin content"))
	})
	
	// Wrap with requireAdmin middleware
	wrappedHandler := server.requireAdmin(handler)
	
	// Create request with non-admin user context
	req := httptest.NewRequest("GET", "/admin", nil)
	mockSession := &session{Username: "testuser", Admin: false}
	ctx := context.WithValue(req.Context(), ctxUser, mockSession)
	req = req.WithContext(ctx)
	
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)
	
	// Should return forbidden
	if recorder.Code != http.StatusForbidden {
		t.Errorf("Expected forbidden status, got %d", recorder.Code)
	}
}

// Test requireAdmin middleware with admin user
func TestRequireAdminMiddlewareWithAdmin(t *testing.T) {
	server := newServerForTest(t)
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("admin content"))
	})
	
	// Wrap with requireAdmin middleware
	wrappedHandler := server.requireAdmin(handler)
	
	// Create request with admin user context
	req := httptest.NewRequest("GET", "/admin", nil)
	mockSession := &session{Username: "admin", Admin: true}
	ctx := context.WithValue(req.Context(), ctxUser, mockSession)
	req = req.WithContext(ctx)
	
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)
	
	// Should allow access
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected OK status, got %d", recorder.Code)
	}
	
	if recorder.Body.String() != "admin content" {
		t.Errorf("Expected 'admin content', got %s", recorder.Body.String())
	}
}

// Test middleware chaining
func TestMiddlewareChaining(t *testing.T) {
	server := newServerForTest(t)
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("chained content"))
	})
	
	// Chain withUser -> requireLogin -> requireAdmin
	wrappedHandler := server.requireAdmin(
		server.requireLogin(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handler.ServeHTTP(w, r)
			})))
	
	finalHandler := server.withUser(wrappedHandler)
	
	// Test with admin user
	req := httptest.NewRequest("GET", "/admin-protected", nil)
	mockSession := &session{Username: "admin", Admin: true}
	ctx := context.WithValue(req.Context(), ctxUser, mockSession)
	req = req.WithContext(ctx)
	
	recorder := httptest.NewRecorder()
	finalHandler.ServeHTTP(recorder, req)
	
	// Should allow access through entire chain
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected OK status through middleware chain, got %d", recorder.Code)
	}
}

// Test context key type safety
func TestContextKeySafety(t *testing.T) {
	// Test that our context key is type-safe
	ctx := context.Background()
	
	// Add user to context
	mockSession := &session{Username: "test", Admin: false}
	ctx = context.WithValue(ctx, ctxUser, mockSession)
	
	// Retrieve user from context
	user, ok := ctx.Value(ctxUser).(*session)
	if !ok {
		t.Error("Failed to retrieve session from context")
	}
	
	if user.Username != "test" {
		t.Errorf("Expected username 'test', got %s", user.Username)
	}
	
	if user.Admin {
		t.Error("Expected non-admin user")
	}
}

// Benchmark middleware performance
func BenchmarkWithUserMiddleware(b *testing.B) {
	server := newServerForTest(&testing.T{})
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	wrappedHandler := server.withUser(handler)
	
	req := httptest.NewRequest("GET", "/test", nil)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(recorder, req)
	}
}

func BenchmarkRequireLoginMiddleware(b *testing.B) {
	server := newServerForTest(&testing.T{})
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	wrappedHandler := server.requireLogin(handler)
	
	// Create request with user context
	req := httptest.NewRequest("GET", "/protected", nil)
	mockSession := &session{Username: "testuser", Admin: false}
	ctx := context.WithValue(req.Context(), ctxUser, mockSession)
	req = req.WithContext(ctx)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(recorder, req)
	}
}
