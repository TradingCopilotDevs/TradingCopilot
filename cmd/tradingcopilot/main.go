package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	appruntime "github.com/TradingCopilotDevs/TradingCopilot/internal/app/runtime"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/composition"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	infralogging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/logging"
	infratracing "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/tracing"
	"go.uber.org/zap"
)

func main() {
	settings := config.Load()
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	shutdownLogging, err := infralogging.Setup(settings, command)
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialize logging: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := shutdownLogging(); err != nil {
			fmt.Fprintf(os.Stderr, "shutdown logging: %v\n", err)
		}
	}()
	shutdownTracing, err := infratracing.Setup(context.Background(), settings, settings.AppName+"/"+command)
	if err != nil {
		fatal(command, fmt.Errorf("initialize tracing: %w", err))
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(ctx); err != nil {
			infralogging.Logger().Warn("shutdown tracing failed",
				infralogging.Event("app.lifecycle"),
				infralogging.Group("app"),
				infralogging.Method(command),
				infralogging.Status(infralogging.StatusError),
				zap.Any(infralogging.FieldDuration, nil),
				zap.Error(err),
			)
		}
	}()
	switch command {
	case "serve":
		if err := serve(settings); err != nil {
			fatal(command, err)
		}
	case "worker":
		if err := composition.RunWorker(settings); err != nil {
			fatal(command, err)
		}
	case "scheduler":
		if err := composition.RunScheduler(settings); err != nil {
			fatal(command, err)
		}
	case "message-subscription-listener":
		if err := runMessageSubscriptionListener(settings); err != nil {
			fatal(command, err)
		}
	case "message-subscription-login":
		if err := runTelegramLogin(settings, os.Args[2:]); err != nil {
			fatal(command, err)
		}
	case "message-subscription-login-start":
		if err := runTelegramLoginStart(settings, os.Args[2:]); err != nil {
			fatal(command, err)
		}
	case "message-subscription-login-verify":
		if err := runTelegramLoginVerify(settings, os.Args[2:]); err != nil {
			fatal(command, err)
		}
	case "message-subscription-mtproto-test":
		if err := runTelegramMTProtoTest(settings, os.Args[2:]); err != nil {
			fatal(command, err)
		}
	case "migrate":
		if err := composition.Migrate(settings); err != nil {
			fatal(command, err)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", command)
		os.Exit(2)
	}
}

func fatal(command string, err error) {
	infralogging.Logger().Error("command failed",
		infralogging.Event("app.lifecycle"),
		infralogging.Group("app"),
		infralogging.Method(command),
		infralogging.Status(infralogging.StatusError),
		infralogging.DurationMS(-1),
		zap.Error(err),
	)
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func runTelegramLogin(settings config.Settings, args []string) error {
	flags := flag.NewFlagSet("message-subscription-login", flag.ExitOnError)
	appID := flags.String("app-id", "", "Telegram API app_id")
	appHash := flags.String("app-hash", "", "Telegram API app_hash")
	phone := flags.String("phone", "", "Telegram account phone number, for example +8613800000000")
	password := flags.String("password", "", "optional Telegram 2FA password")
	if err := flags.Parse(args); err != nil {
		return err
	}
	usecase, err := runtimeUsecase(settings)
	if err != nil {
		return err
	}
	return usecase.RunMessageSubscriptionLogin(context.Background(), appruntime.MessageSubscriptionLoginInput{
		AppID:    *appID,
		AppHash:  *appHash,
		Phone:    *phone,
		Password: *password,
	})
}

func runTelegramLoginStart(settings config.Settings, args []string) error {
	flags := flag.NewFlagSet("message-subscription-login-start", flag.ExitOnError)
	appID := flags.String("app-id", "", "Telegram API app_id")
	appHash := flags.String("app-hash", "", "Telegram API app_hash")
	phone := flags.String("phone", "", "Telegram account phone number, for example +8613800000000")
	if err := flags.Parse(args); err != nil {
		return err
	}
	usecase, err := runtimeUsecase(settings)
	if err != nil {
		return err
	}
	hash, err := usecase.RunMessageSubscriptionLoginStart(context.Background(), appruntime.MessageSubscriptionLoginStartInput{
		AppID:   *appID,
		AppHash: *appHash,
		Phone:   *phone,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Telegram code sent. phone_code_hash=%s\n", hash)
	return nil
}

func runTelegramLoginVerify(settings config.Settings, args []string) error {
	flags := flag.NewFlagSet("message-subscription-login-verify", flag.ExitOnError)
	phone := flags.String("phone", "", "Telegram account phone number; optional if login-start was run against the same database")
	code := flags.String("code", "", "Telegram verification code")
	phoneCodeHash := flags.String("phone-code-hash", "", "phone_code_hash from message-subscription-login-start; optional if login-start was run against the same database")
	password := flags.String("password", "", "optional Telegram 2FA password")
	if err := flags.Parse(args); err != nil {
		return err
	}
	usecase, err := runtimeUsecase(settings)
	if err != nil {
		return err
	}
	if err := usecase.RunMessageSubscriptionLoginVerify(context.Background(), appruntime.MessageSubscriptionLoginVerifyInput{
		Phone:         *phone,
		Code:          *code,
		PhoneCodeHash: *phoneCodeHash,
		Password:      *password,
	}); err != nil {
		return err
	}
	fmt.Println("Telegram MTProto session stored.")
	return nil
}

func runTelegramMTProtoTest(settings config.Settings, args []string) error {
	flags := flag.NewFlagSet("message-subscription-mtproto-test", flag.ExitOnError)
	channel := flags.String("channel", "", "Telegram channel ref, for example https://t.me/zaihuapd, @zaihuapd, or -100...")
	if err := flags.Parse(args); err != nil {
		return err
	}
	usecase, err := runtimeUsecase(settings)
	if err != nil {
		return err
	}
	result, err := usecase.RunMessageSubscriptionMTProtoTest(context.Background(), appruntime.MessageSubscriptionMTProtoTestInput{ChannelRef: *channel})
	if err != nil {
		return err
	}
	fmt.Printf("Telegram MTProto latest message: %+v\n", result)
	return nil
}

func serve(settings config.Settings) error {
	return composition.Serve(settings)
}

func runMessageSubscriptionListener(settings config.Settings) error {
	return composition.RunMessageSubscriptionListener(settings)
}

func runtimeUsecase(settings config.Settings) (appruntime.Usecase, error) {
	return composition.RuntimeUsecase(settings, os.Stdin, os.Stdout)
}
