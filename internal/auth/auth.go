package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/YousefProject/servicedesk/internal/model"
)

const SessionCookie = "sd_session"
const CSRFCookie = "sd_csrf"

var (
	ErrInvalidCredentials = errors.New("correo o contraseña incorrectos")
	ErrInactive           = errors.New("la cuenta está desactivada")
)

func HashPassword(pw string) (string, error) {
	if len(pw) < 8 {
		return "", errors.New("la contraseña debe tener al menos 8 caracteres")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw))
}

func NewToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func NormalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

type ctxKey int

const userKey ctxKey = 1
const csrfKey ctxKey = 2

type Identity struct {
	User *model.User
	CSRF string
}

func WithIdentity(ctx context.Context, id *Identity) context.Context {
	ctx = context.WithValue(ctx, userKey, id.User)
	return context.WithValue(ctx, csrfKey, id.CSRF)
}

func User(ctx context.Context) *model.User {
	u, _ := ctx.Value(userKey).(*model.User)
	return u
}

func CurrentUser(r *http.Request) *model.User { return User(r.Context()) }

func CSRFToken(ctx context.Context) string {
	s, _ := ctx.Value(csrfKey).(string)
	return s
}

func IsStaff(ctx context.Context) bool {
	u := User(ctx)
	return u != nil && u.Role.IsStaff()
}

func IsAdmin(ctx context.Context) bool {
	u := User(ctx)
	return u != nil && u.Role == model.RoleAdmin
}

func CanViewTicket(u *model.User, t *model.Ticket) bool {
	if u == nil {
		return false
	}
	if u.Role.IsStaff() {
		return true
	}
	return t.RequesterID == u.ID
}

func CanComment(u *model.User, t *model.Ticket) bool {
	return CanViewTicket(u, t)
}

func CanTriage(u *model.User) bool { return u != nil && u.Role.IsStaff() }

func CanSetInternal(u *model.User) bool { return u != nil && u.Role.IsStaff() }

// CanEditTicket: el staff edita cualquier ticket; el solicitante solo los suyos.
func CanEditTicket(u *model.User, t *model.Ticket) bool {
	if u == nil || t == nil {
		return false
	}
	return u.Role.IsStaff() || t.RequesterID == u.ID
}

// CanDeleteTicket: mismos permisos que editar.
func CanDeleteTicket(u *model.User, t *model.Ticket) bool {
	return CanEditTicket(u, t)
}
