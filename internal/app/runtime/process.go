package runtime

import (
	"context"
	"errors"
)

type ProcessRuntime interface {
	SeedDefaults() error
	RunMessageSubscriptionListener(ctx context.Context)
	StartLocalBackgroundLoops(ctx context.Context)
}

type HTTPServer interface {
	ListenAndServe() error
	Shutdown(ctx context.Context) error
}

type ServeOptions struct {
	Runtime                             ProcessRuntime
	Server                              HTTPServer
	MessageSubscriptionListenersInServe bool
	RunLocalBackgroundLoops             bool
	ExpectedShutdownError               func(error) bool
}

func Serve(ctx context.Context, options ServeOptions) error {
	if options.Runtime == nil {
		return errors.New("runtime is required")
	}
	if options.Server == nil {
		return errors.New("server is required")
	}
	if err := options.Runtime.SeedDefaults(); err != nil {
		return err
	}
	if options.MessageSubscriptionListenersInServe {
		go options.Runtime.RunMessageSubscriptionListener(ctx)
	}
	if options.RunLocalBackgroundLoops {
		options.Runtime.StartLocalBackgroundLoops(ctx)
	}
	go func() {
		<-ctx.Done()
		_ = options.Server.Shutdown(context.Background())
	}()
	if err := options.Server.ListenAndServe(); err != nil && !expectedShutdown(options.ExpectedShutdownError, err) {
		return err
	}
	return nil
}

func expectedShutdown(match func(error) bool, err error) bool {
	return match != nil && match(err)
}
