package telegram

import (
	"context"
	"encoding/base64"

	gotdsession "github.com/gotd/td/session"
)

type SecretStore interface {
	SecretValue(ctx context.Context, name string) (string, error)
	UpsertSecret(ctx context.Context, name string, value string) error
}

type MTProtoSessionStorage struct {
	Store      SecretStore
	SecretName string
	LastData   []byte
}

func (s *MTProtoSessionStorage) LoadSession(ctx context.Context) ([]byte, error) {
	if s == nil || s.Store == nil {
		return nil, gotdsession.ErrNotFound
	}
	value, err := s.Store.SecretValue(ctx, s.SecretName)
	if err != nil {
		return nil, gotdsession.ErrNotFound
	}
	data, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, err
	}
	s.LastData = append(s.LastData[:0], data...)
	return data, nil
}

func (s *MTProtoSessionStorage) StoreSession(ctx context.Context, data []byte) error {
	s.LastData = append(s.LastData[:0], data...)
	return s.Store.UpsertSecret(ctx, s.SecretName, base64.StdEncoding.EncodeToString(data))
}

func (s *MTProtoSessionStorage) LastDataEncoded() string {
	if s == nil || len(s.LastData) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(s.LastData)
}
