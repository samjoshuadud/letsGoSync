package secrets

import (
	"errors"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	serviceName = "moodletodo-cli"

	keyMoodleWebServiceToken = "moodle-webservice-token"
	keyMoodleSessionCookie   = "moodle-session-cookie"
	keyTodoistToken          = "todoist-token"
)

type Store struct {
	Service string
}

func NewStore() *Store {
	return &Store{Service: serviceName}
}

func (s *Store) SetMoodleWebServiceToken(token string) error {
	return s.set(keyMoodleWebServiceToken, strings.TrimSpace(token))
}

func (s *Store) GetMoodleWebServiceToken() (string, error) {
	return s.get(keyMoodleWebServiceToken)
}

func (s *Store) SetMoodleSessionCookie(cookie string) error {
	return s.set(keyMoodleSessionCookie, strings.TrimSpace(cookie))
}

func (s *Store) GetMoodleSessionCookie() (string, error) {
	return s.get(keyMoodleSessionCookie)
}

func (s *Store) SetTodoistToken(token string) error {
	return s.set(keyTodoistToken, strings.TrimSpace(token))
}

func (s *Store) GetTodoistToken() (string, error) {
	return s.get(keyTodoistToken)
}

func (s *Store) DeleteMoodleWebServiceToken() error {
	return s.delete(keyMoodleWebServiceToken)
}

func (s *Store) DeleteMoodleSessionCookie() error {
	return s.delete(keyMoodleSessionCookie)
}

func (s *Store) DeleteTodoistToken() error {
	return s.delete(keyTodoistToken)
}

func (s *Store) set(user, value string) error {
	if value == "" {
		return errors.New("secret value is empty")
	}
	return keyring.Set(s.Service, user, value)
}

func (s *Store) get(user string) (string, error) {
	v, err := keyring.Get(s.Service, user)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(v), nil
}

func (s *Store) delete(user string) error {
	err := keyring.Delete(s.Service, user)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

func Mask(secret string) string {
	if len(secret) <= 8 {
		return "********"
	}
	return secret[:4] + strings.Repeat("*", len(secret)-8) + secret[len(secret)-4:]
}
