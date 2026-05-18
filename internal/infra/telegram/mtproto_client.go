package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gotd/td/constant"
	gotdtelegram "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

type MTProtoCredentials struct {
	AppID   int
	AppHash string
}

type MTProtoClient struct {
	Credentials    MTProtoCredentials
	LoginStorage   *MTProtoSessionStorage
	SessionStorage *MTProtoSessionStorage
	Proxy          *ProxyConfig
	Location       *time.Location
}

type MTProtoLiveChannel struct {
	ID         uint
	ChannelRef string
	Title      string
}

type MTProtoLiveMessage struct {
	ChannelID   uint
	MessageID   int64
	MessageTime time.Time
	Text        string
	Raw         domainkernel.JSON
}

func (c MTProtoClient) StartLogin(ctx context.Context, phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", errors.New("telegram phone is required")
	}
	client := gotdtelegram.NewClient(c.Credentials.AppID, c.Credentials.AppHash, c.clientOptions(c.LoginStorage, nil))
	var hash string
	err := client.Run(ctx, func(ctx context.Context) error {
		sent, err := client.Auth().SendCode(ctx, phone, auth.SendCodeOptions{})
		if err != nil {
			return RPCError(err)
		}
		if withHash, ok := sent.(interface{ GetPhoneCodeHash() string }); ok {
			hash = withHash.GetPhoneCodeHash()
		}
		if hash == "" {
			return errors.New("telegram did not return phone_code_hash")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return hash, nil
}

func (c MTProtoClient) CompleteLogin(ctx context.Context, phone string, code string, phoneCodeHash string, password string) (string, error) {
	phone = strings.TrimSpace(phone)
	code = strings.TrimSpace(code)
	phoneCodeHash = strings.TrimSpace(phoneCodeHash)
	if phone == "" || code == "" || phoneCodeHash == "" {
		return "", errors.New("phone, code, and phone_code_hash are required")
	}
	client := gotdtelegram.NewClient(c.Credentials.AppID, c.Credentials.AppHash, c.clientOptions(c.LoginStorage, nil))
	err := client.Run(ctx, func(ctx context.Context) error {
		_, err := client.Auth().SignIn(ctx, phone, code, phoneCodeHash)
		if err == nil {
			return nil
		}
		if errors.Is(err, auth.ErrPasswordAuthNeeded) {
			if strings.TrimSpace(password) == "" {
				return errors.New("telegram two-factor password is required")
			}
			_, err = client.Auth().Password(ctx, password)
			return RPCError(err)
		}
		return RPCError(err)
	})
	if err != nil {
		return "", err
	}
	if c.LoginStorage == nil {
		return "", errors.New("telegram login succeeded but no MTProto session storage was configured")
	}
	if len(c.LoginStorage.LastData) == 0 {
		if data, err := c.LoginStorage.LoadSession(ctx); err == nil {
			c.LoginStorage.LastData = data
		}
	}
	encoded := c.LoginStorage.LastDataEncoded()
	if encoded == "" {
		return "", errors.New("telegram login succeeded but no MTProto session was stored")
	}
	return encoded, nil
}

func (c MTProtoClient) LatestMessage(ctx context.Context, channelRef string) (map[string]any, error) {
	result, err := c.RecentMessages(ctx, channelRef, 1, nil)
	if err != nil {
		return nil, err
	}
	if len(result.Messages) == 0 {
		return map[string]any{"status": "empty", "channel_ref": result.ChannelRef, "title": result.Title, "message_id": nil, "message_time": nil, "text": nil}, nil
	}
	message := result.Messages[0]
	return map[string]any{"status": "ok", "channel_ref": result.ChannelRef, "title": result.Title, "message_id": message.MessageID, "message_time": message.MessageTime, "text": message.Text}, nil
}

func (c MTProtoClient) RecentMessages(ctx context.Context, channelRef string, limit int, minMessageID *int64) (PublicResult, error) {
	if limit <= 0 {
		limit = 20
	}
	client := gotdtelegram.NewClient(c.Credentials.AppID, c.Credentials.AppHash, c.clientOptions(c.SessionStorage, nil))
	normalized := NormalizeChannelRef(channelRef)
	result := PublicResult{ChannelRef: normalized}
	err := client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return RPCError(err)
		}
		if !status.Authorized {
			return errors.New("stored Telegram MTProto session is invalid or expired; please log in again")
		}
		manager := peers.Options{Storage: &peers.InmemoryStorage{}, Cache: &peers.InmemoryCache{}}.Build(client.API())
		peer, err := resolvePeer(ctx, manager, client.API(), normalized)
		if err != nil {
			return RPCError(err)
		}
		result.Title = peer.VisibleName()
		req := &tg.MessagesGetHistoryRequest{Peer: peer.InputPeer(), Limit: limit}
		if minMessageID != nil && *minMessageID > 0 {
			req.MinID = int(*minMessageID)
		}
		history, err := client.API().MessagesGetHistory(ctx, req)
		if err != nil {
			return RPCError(err)
		}
		result.Messages = c.messagesFromHistory(history)
		return nil
	})
	return result, err
}

func (c MTProtoClient) Listen(ctx context.Context, channels []MTProtoLiveChannel, handle func(context.Context, MTProtoLiveMessage) error, onResolvedTitle func(context.Context, uint, string) error) error {
	resolved := newLiveChannelMap()
	dispatcher := tg.NewUpdateDispatcher()
	dispatcher.OnNewMessage(func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewMessage) error {
		return c.handleLiveUpdateMessage(ctx, resolved, update.Message, handle)
	})
	dispatcher.OnNewChannelMessage(func(ctx context.Context, entities tg.Entities, update *tg.UpdateNewChannelMessage) error {
		return c.handleLiveUpdateMessage(ctx, resolved, update.Message, handle)
	})
	client := gotdtelegram.NewClient(c.Credentials.AppID, c.Credentials.AppHash, c.clientOptions(c.SessionStorage, dispatcher))
	return client.Run(ctx, func(ctx context.Context) error {
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return RPCError(err)
		}
		if !status.Authorized {
			return errors.New("stored Telegram MTProto session is invalid or expired; please log in again")
		}
		manager := peers.Options{Storage: &peers.InmemoryStorage{}, Cache: &peers.InmemoryCache{}}.Build(client.API())
		for i := range channels {
			channel := channels[i]
			peer, err := resolvePeer(ctx, manager, client.API(), channel.ChannelRef)
			if err != nil {
				continue
			}
			resolved.add(peer.ID(), channel.ID)
			resolved.add(int64(peer.TDLibPeerID()), channel.ID)
			if title := strings.TrimSpace(peer.VisibleName()); title != "" && title != channel.Title && onResolvedTitle != nil {
				if err := onResolvedTitle(ctx, channel.ID, title); err != nil {
					return err
				}
			}
		}
		if resolved.len() == 0 {
			return errors.New("no enabled telegram channels could be resolved")
		}
		<-ctx.Done()
		return ctx.Err()
	})
}

func (c MTProtoClient) clientOptions(storage gotdtelegram.SessionStorage, handler gotdtelegram.UpdateHandler) gotdtelegram.Options {
	opts := gotdtelegram.Options{SessionStorage: storage, NoUpdates: handler == nil, UpdateHandler: handler}
	if dial, ok := ProxyDialer(c.Proxy); ok {
		opts.Resolver = dcs.Plain(dcs.PlainOptions{Dial: dial})
	}
	return opts
}

func (c MTProtoClient) messagesFromHistory(history tg.MessagesMessagesClass) []PublicMessage {
	var raw []tg.MessageClass
	switch typed := history.(type) {
	case *tg.MessagesMessages:
		raw = typed.GetMessages()
	case *tg.MessagesMessagesSlice:
		raw = typed.GetMessages()
	case *tg.MessagesChannelMessages:
		raw = typed.GetMessages()
	default:
		return nil
	}
	out := make([]PublicMessage, 0, len(raw))
	for _, item := range raw {
		message, ok := item.(*tg.Message)
		if !ok || strings.TrimSpace(message.Message) == "" {
			continue
		}
		messageTime := time.Unix(int64(message.Date), 0).In(c.location())
		rawPayload, _ := json.Marshal(map[string]any{"id": message.ID, "date": messageTime.Format(time.RFC3339), "peer_id": fmt.Sprint(message.PeerID), "source": "mtproto"})
		out = append(out, PublicMessage{MessageID: int64(message.ID), MessageTime: messageTime, Text: message.Message, Raw: domainkernel.JSON(rawPayload)})
	}
	return out
}

func (c MTProtoClient) handleLiveUpdateMessage(ctx context.Context, channels *liveChannelMap, messageClass tg.MessageClass, handle func(context.Context, MTProtoLiveMessage) error) error {
	if handle == nil {
		return nil
	}
	message, ok := messageClass.(*tg.Message)
	if !ok || strings.TrimSpace(message.Message) == "" {
		return nil
	}
	channelID, ok := channels.get(message.PeerID)
	if !ok {
		return nil
	}
	messageTime := time.Unix(int64(message.Date), 0).In(c.location())
	rawPayload, _ := json.Marshal(map[string]any{
		"id":      message.ID,
		"date":    messageTime.Format(time.RFC3339),
		"peer_id": fmt.Sprint(message.PeerID),
		"source":  "mtproto_live",
	})
	return handle(ctx, MTProtoLiveMessage{ChannelID: channelID, MessageID: int64(message.ID), MessageTime: messageTime, Text: message.Message, Raw: domainkernel.JSON(rawPayload)})
}

func (c MTProtoClient) location() *time.Location {
	if c.Location != nil {
		return c.Location
	}
	return time.FixedZone("Asia/Shanghai", 8*3600)
}

type MTProtoPeerRef interface {
	ID() int64
	TDLibPeerID() constant.TDLibPeerID
	VisibleName() string
	InputPeer() tg.InputPeerClass
}

type dialogResolvedChannelPeer struct {
	channelID  int64
	accessHash int64
	title      string
}

func (p dialogResolvedChannelPeer) ID() int64 { return p.channelID }

func (p dialogResolvedChannelPeer) TDLibPeerID() constant.TDLibPeerID {
	var id constant.TDLibPeerID
	id.Channel(p.channelID)
	return id
}

func (p dialogResolvedChannelPeer) VisibleName() string {
	if strings.TrimSpace(p.title) != "" {
		return p.title
	}
	return fmt.Sprintf("%d", p.TDLibPeerID())
}

func (p dialogResolvedChannelPeer) InputPeer() tg.InputPeerClass {
	return &tg.InputPeerChannel{ChannelID: p.channelID, AccessHash: p.accessHash}
}

func resolvePeer(ctx context.Context, manager *peers.Manager, api *tg.Client, normalized string) (MTProtoPeerRef, error) {
	value := strings.TrimSpace(normalized)
	if strings.HasPrefix(value, "-100") {
		if raw, err := strconv.ParseInt(value, 10, 64); err == nil {
			tdID := constant.TDLibPeerID(raw)
			peer, err := manager.ResolveTDLibID(ctx, tdID)
			if err == nil {
				return peer, nil
			}
			if api == nil {
				return nil, err
			}
			fallback, fallbackErr := resolveNumericChannelFromDialogs(ctx, api, tdID.ToPlain())
			if fallbackErr == nil {
				return fallback, nil
			}
			return nil, fmt.Errorf("telegram numeric channel %s could not be resolved: %w; dialogs fallback failed: %v. Ensure the logged-in account has joined or opened the channel, or configure a public @username/invite link because Telegram does not expose access_hash for unknown private channels", value, err, fallbackErr)
		}
	}
	if strings.HasPrefix(value, "@") {
		value = strings.TrimPrefix(value, "@")
	}
	return manager.Resolve(ctx, value)
}

func resolveNumericChannelFromDialogs(ctx context.Context, api *tg.Client, channelID int64) (MTProtoPeerRef, error) {
	iter := dialogs.NewQueryBuilder(api).GetDialogs().BatchSize(100).Iter()
	scanned := 0
	for iter.Next(ctx) {
		scanned++
		if scanned > 5000 {
			return nil, fmt.Errorf("numeric channel %d not found after scanning 5000 dialogs", channelID)
		}
		if peer, ok := ChannelFromDialogElem(channelID, iter.Value()); ok {
			return peer, nil
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("numeric channel %d is not present in the logged-in account dialogs", channelID)
}

func ChannelFromDialogElem(channelID int64, elem dialogs.Elem) (MTProtoPeerRef, bool) {
	peer, ok := elem.Dialog.GetPeer().(*tg.PeerChannel)
	if !ok || peer.ChannelID != channelID {
		return nil, false
	}
	input, ok := elem.Peer.(*tg.InputPeerChannel)
	if !ok || input.ChannelID != channelID || input.AccessHash == 0 {
		return nil, false
	}
	title := ""
	if channel, ok := elem.Entities.Channel(channelID); ok && channel != nil {
		title = channel.Title
	}
	return dialogResolvedChannelPeer{channelID: input.ChannelID, accessHash: input.AccessHash, title: title}, true
}

type liveChannelMap struct {
	mu       sync.RWMutex
	channels map[int64]uint
}

func newLiveChannelMap() *liveChannelMap {
	return &liveChannelMap{channels: map[int64]uint{}}
}

func (m *liveChannelMap) add(peerID int64, channelID uint) {
	if peerID == 0 || channelID == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[peerID] = channelID
}

func (m *liveChannelMap) get(peer tg.PeerClass) (uint, bool) {
	for _, key := range peerKeys(peer) {
		m.mu.RLock()
		channelID, ok := m.channels[key]
		m.mu.RUnlock()
		if ok {
			return channelID, true
		}
	}
	return 0, false
}

func (m *liveChannelMap) len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.channels)
}

func peerKeys(peer tg.PeerClass) []int64 {
	switch typed := peer.(type) {
	case *tg.PeerChannel:
		return []int64{typed.ChannelID, int64(constant.TDLibPeerID(-1000000000000 - typed.ChannelID))}
	case *tg.PeerChat:
		return []int64{typed.ChatID, int64(constant.TDLibPeerID(-typed.ChatID))}
	case *tg.PeerUser:
		return []int64{typed.UserID}
	default:
		return nil
	}
}

func RPCError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if strings.Contains(message, "ResendCodeRequest") || strings.Contains(message, "all available options") {
		return errors.New("Telegram has temporarily rejected another verification-code request for this phone number. Wait for Telegram's cooldown, then click Send Code again")
	}
	return err
}
