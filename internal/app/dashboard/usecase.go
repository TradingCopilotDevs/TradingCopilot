package dashboard

import (
	"context"
)

type Loader interface {
	Load(ctx context.Context) (map[string]any, error)
}

type Usecase struct {
	loader   Loader
	settings Settings
}

type Settings struct {
	AppName string
	AppEnv  string
}

func NewUsecase(loader Loader, settings Settings) Usecase {
	return Usecase{loader: loader, settings: settings}
}

func (u Usecase) Load(ctx context.Context) (map[string]any, error) {
	if u.loader == nil {
		return map[string]any{"summary": map[string]any{"appName": u.settings.AppName, "appEnv": u.settings.AppEnv}}, nil
	}
	return u.loader.Load(ctx)
}
