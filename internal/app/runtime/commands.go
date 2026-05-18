package runtime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

type TelegramMTProto interface {
	SaveTelegramMTProtoAppConfig(appID string, appHash string) error
	StartTelegramMTProtoLogin(phone string) (string, error)
	CompleteTelegramMTProtoLogin(phone string, code string, phoneCodeHash string, password string) error
	GetLatestTelegramMTProtoMessage(channelRef string) (map[string]any, error)
}

type Usecase struct {
	telegram TelegramMTProto
	in       io.Reader
	out      io.Writer
}

func NewUsecase(telegram TelegramMTProto, in io.Reader, out io.Writer) Usecase {
	return Usecase{telegram: telegram, in: in, out: out}
}

type MessageSubscriptionLoginInput struct {
	AppID    string
	AppHash  string
	Phone    string
	Password string
}

type MessageSubscriptionLoginStartInput struct {
	AppID   string
	AppHash string
	Phone   string
}

type MessageSubscriptionLoginVerifyInput struct {
	Phone         string
	Code          string
	PhoneCodeHash string
	Password      string
}

type MessageSubscriptionMTProtoTestInput struct {
	ChannelRef string
}

func (u Usecase) RunMessageSubscriptionLogin(_ context.Context, input MessageSubscriptionLoginInput) error {
	if u.telegram == nil {
		return errors.New("message subscription runtime is not configured")
	}
	if err := u.saveAppConfig(input.AppID, input.AppHash); err != nil {
		return err
	}
	reader := bufio.NewReader(u.in)
	phone := strings.TrimSpace(input.Phone)
	if phone == "" {
		value, err := u.prompt(reader, "Telegram phone: ")
		if err != nil {
			return err
		}
		phone = value
	}
	hash, err := u.telegram.StartTelegramMTProtoLogin(phone)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(u.out, "Telegram code sent. Check Telegram app/SMS, then enter the code.")
	code, err := u.prompt(reader, "Code: ")
	if err != nil {
		return err
	}
	password := strings.TrimSpace(input.Password)
	if password == "" {
		value, err := u.prompt(reader, "2FA password, leave empty if not enabled: ")
		if err != nil {
			return err
		}
		password = value
	}
	if err := u.telegram.CompleteTelegramMTProtoLogin(phone, code, hash, password); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(u.out, "Telegram MTProto session stored.")
	return nil
}

func (u Usecase) RunMessageSubscriptionLoginStart(_ context.Context, input MessageSubscriptionLoginStartInput) (string, error) {
	if u.telegram == nil {
		return "", errors.New("message subscription runtime is not configured")
	}
	if err := u.saveAppConfig(input.AppID, input.AppHash); err != nil {
		return "", err
	}
	phone := strings.TrimSpace(input.Phone)
	if phone == "" {
		return "", errors.New("phone is required")
	}
	return u.telegram.StartTelegramMTProtoLogin(phone)
}

func (u Usecase) RunMessageSubscriptionLoginVerify(_ context.Context, input MessageSubscriptionLoginVerifyInput) error {
	if u.telegram == nil {
		return errors.New("message subscription runtime is not configured")
	}
	if strings.TrimSpace(input.Code) == "" {
		return errors.New("code is required")
	}
	return u.telegram.CompleteTelegramMTProtoLogin(input.Phone, input.Code, input.PhoneCodeHash, input.Password)
}

func (u Usecase) RunMessageSubscriptionMTProtoTest(_ context.Context, input MessageSubscriptionMTProtoTestInput) (map[string]any, error) {
	if u.telegram == nil {
		return nil, errors.New("message subscription runtime is not configured")
	}
	channel := strings.TrimSpace(input.ChannelRef)
	if channel == "" {
		return nil, errors.New("channel is required")
	}
	return u.telegram.GetLatestTelegramMTProtoMessage(channel)
}

func (u Usecase) saveAppConfig(appID string, appHash string) error {
	if strings.TrimSpace(appID) == "" && strings.TrimSpace(appHash) == "" {
		return nil
	}
	return u.telegram.SaveTelegramMTProtoAppConfig(appID, appHash)
}

func (u Usecase) prompt(reader *bufio.Reader, prompt string) (string, error) {
	_, _ = fmt.Fprint(u.out, prompt)
	value, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}
