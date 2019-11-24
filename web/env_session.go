package web

import (
	"net/http"

	"github.com/gorilla/sessions"
)

const SESSION_NAME = "vmango"
const SESSION_USER_KEY = "auth_user"

type Session struct {
	*sessions.Session
}

func (session *Session) AuthUser() *User {
	rawUser := session.Values[SESSION_USER_KEY]
	if user, ok := rawUser.(*User); ok {
		return user
	}
	return &User{FullName: "Anonymous"}
}

func (session *Session) SetAuthUser(user *User) {
	session.Values[SESSION_USER_KEY] = user
}

func (session *Session) IsAuthenticated() bool {
	return session.AuthUser().Authenticated
}

func (env *Environ) Session(request *http.Request) *Session {
	session, err := env.sessions.Get(request, SESSION_NAME)
	if err != nil {
		env.logger.Warn().Err(err).Msg("failed to fetch session, creating new one")
		session.IsNew = true
	}
	return &Session{session}
}
