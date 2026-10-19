package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/YousefProject/servicedesk/internal/auth"
)

func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(auth.SessionCookie)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r.WithContext(s.anonCSRF(w, r)))
			return
		}
		sess, user, err := s.store.Session(cookie.Value)
		if err != nil {
			s.clearCookie(w)
			next.ServeHTTP(w, r.WithContext(s.anonCSRF(w, r)))
			return
		}
		ctx := auth.WithIdentity(r.Context(), &auth.Identity{User: user, CSRF: sess.CSRF})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) anonCSRF(w http.ResponseWriter, r *http.Request) context.Context {
	token := ""
	if c, err := r.Cookie(auth.CSRFCookie); err == nil && c.Value != "" {
		token = c.Value
	}
	if token == "" {
		token = auth.NewToken()
		http.SetCookie(w, &http.Cookie{
			Name:     auth.CSRFCookie,
			Value:    token,
			Path:     "/",
			MaxAge:   int(s.conf().SessionTTL.Seconds()),
			HttpOnly: false,
			Secure:   s.conf().CookieSecure,
			SameSite: http.SameSiteLaxMode,
		})
	}
	return auth.WithIdentity(r.Context(), &auth.Identity{User: nil, CSRF: token})
}

func (s *Server) csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			want := auth.CSRFToken(r.Context())
			got := r.Header.Get("X-CSRF-Token")
			if got == "" {
				// FormValue parsea también bodies multipart (adjuntos).
				got = r.FormValue("csrf")
			}
			if want == "" || got != want {
				http.Error(w, "token CSRF inválido", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/static/") && rec.status == http.StatusOK {
			return
		}
		s.logger.Info("http",
			"method", r.Method, "path", r.URL.Path, "status", rec.status,
			"dur_ms", time.Since(start).Milliseconds(), "ip", s.clientIP(r),
		)
	})
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic", "err", fmt.Sprint(rec), "stack", string(debug.Stack()))
				http.Error(w, "error interno", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) clientIP(r *http.Request) string {
	if s.conf().TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
		if xr := r.Header.Get("X-Real-IP"); xr != "" {
			return xr
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) setCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.conf().CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.conf().CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request, userID int64) error {
	token := auth.NewToken()
	csrf := auth.NewToken()
	expires := time.Now().UTC().Add(s.conf().SessionTTL)
	if err := s.store.CreateSession(token, userID, csrf, expires, r.UserAgent(), s.clientIP(r)); err != nil {
		return err
	}
	s.setCookie(w, token, expires)
	return nil
}
