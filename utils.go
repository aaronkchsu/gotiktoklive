package gotiktoklive

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/erni27/imcache"
	pb "github.com/steampoweredtaco/gotiktoklive/proto"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const (
	messageHistoryTimeout           = 15 * time.Minute
	envelopeBusinessTypeSuperFanBox = 19
)

var (
	msgIDCache imcache.Cache[int64, struct{}]
)

func getRandomDeviceID() string {
	const chars = "0123456789"
	b := make([]byte, 20)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func parseMsg(msg *pb.WebcastResponse_Message, warnHandler func(...interface{}), debugHandler func(...interface{}), enableExperimentalEvents bool) (out Event, err error) {
	tReflect, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(msg.Method))
	if err != nil {
		base := base64.RawStdEncoding.EncodeToString(msg.Payload)
		debugHandler(fmt.Sprintf("cannot find type %s:\n%s ", msg.Method, base))
		return nil, nil
	}
	m := tReflect.New().Interface()
	if err = proto.Unmarshal(msg.Payload, m); err != nil {
		base := base64.RawStdEncoding.EncodeToString(msg.Payload)
		err = fmt.Errorf("failed to unmarshal proto %T: %w\n%s", m, err, base)
		debugHandler(err)
		warnHandler(fmt.Errorf("failed to unmarshal proto %T: %w", m, err))
		return nil, nil
	}
	switch pt := m.(type) {
	case *pb.RoomMessage:
		return RoomEvent{
			MessageID: pt.Common.MsgId,
			Timestamp: pt.Common.CreateTime,
			Type:      pt.Common.Method,
			Message:   pt.Content,
			isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil
	case *pb.WebcastRoomPinMessage:
		{
			tReflect, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(pt.OriginalMsgType))
			if err != nil {
				base := base64.RawStdEncoding.EncodeToString(msg.Payload)
				debugHandler("cannot find proto type for pin message %s:\n%s ", msg.Method, base)
				return RoomEvent{
					MessageID: msg.MsgId,
					Timestamp: pt.Common.CreateTime,
					Type:      pt.OriginalMsgType,
					Message:   "<unknown>",
					isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
				}, nil
			}
			m := tReflect.New().Interface()
			if err = proto.Unmarshal(pt.PinnedMessage, m); err != nil {
				base := base64.RawStdEncoding.EncodeToString(msg.Payload)
				err = fmt.Errorf("failed to unmarshal proto %T: %w\n%s", m, err, base)
				debugHandler(err)
				warnHandler(fmt.Errorf("failed to unmarshal proto %T: %w", m, err))
				return RoomEvent{
					MessageID: msg.MsgId,
					Timestamp: pt.Common.CreateTime,
					Type:      pt.OriginalMsgType,
					Message:   "<unknown>",
					isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
				}, nil
			}

			typeStr := pt.OriginalMsgType
			msgPinned := "<unknown pinned type>"
			switch pt2 := m.(type) {
			// Todo make a pin return type
			case *pb.WebcastChatMessage:
				return ChatEvent{
					MessageID: pt.Common.MsgId,
					Timestamp: pt.Common.CreateTime,
					Comment:   "<pinned>: " + pt2.Content,
					User:      toUser(pt2.User),
					isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
				}, nil
			default:
				base := base64.RawStdEncoding.EncodeToString(pt.PinnedMessage)
				err = fmt.Errorf("unimplemented pinned message type %T\n%s", m, base)
				debugHandler(err)
				warnHandler(fmt.Sprintf("unimplemented pinned message type %T", m))

			}
			return RoomEvent{
				MessageID: pt.Common.MsgId,
				Timestamp: pt.Common.CreateTime,
				Type:      typeStr,
				Message:   msgPinned,
				isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
			}, nil
		}
	case *pb.WebcastChatMessage:
		return ChatEvent{
			MessageID:    pt.Common.MsgId,
			Comment:      pt.Content,
			User:         toUser(pt.User),
			UserIdentity: toUserIdentity(pt.UserIdentity),
			Timestamp:    pt.Common.CreateTime,
			isHistory:    msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil
	case *pb.WebcastMemberMessage:
		common := pt.GetCommon()
		displayType := firstNonEmpty(displayTextKey(common), pt.GetAction().String())
		return UserEvent{
			MessageID: common.GetMsgId(),
			Timestamp: common.GetCreateTime(),
			Event:     toUserType(displayType),
			User:      toUser(pt.User),
			isHistory: msg.IsHistory || cachedHistory(common.GetMsgId()),
		}, nil
	case *pb.WebcastLiveGameIntroMessage:
		return RoomEvent{
			MessageID: pt.Common.MsgId,
			Timestamp: pt.Common.CreateTime,
			Type:      pt.Common.Method,
			Message:   pt.GameText.DefaultPattern,
			isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil
	case *pb.WebcastRoomMessage:
		return RoomEvent{
			MessageID: pt.Common.MsgId,
			Timestamp: pt.Common.CreateTime,
			Type:      pt.Common.Method,
			// TODO: Make this actually use pieces list and fill out the format text correctly.
			Message:   pt.Common.DisplayText.DefaultPattern,
			isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil
	case *pb.WebcastRoomUserSeqMessage:
		return ViewersEvent{
			MessageID: pt.Common.MsgId,
			Timestamp: pt.Common.CreateTime,
			Viewers:   int(pt.Total),
			isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil
	case *pb.WebcastSocialMessage:
		common := pt.GetCommon()
		return UserEvent{
			MessageID: common.GetMsgId(),
			Timestamp: common.GetCreateTime(),
			Event:     toUserType(displayTextKey(common)),
			User:      toUser(pt.User),
			isHistory: msg.IsHistory || cachedHistory(common.GetMsgId()),
		}, nil
	case *pb.WebcastBarrageMessage:
		return toSuperFanEvent(pt, msg), nil
	case *pb.WebcastEnvelopeMessage:
		return toSuperFanBoxEvent(pt, msg), nil
	case *pb.WebcastGiftMessage:
		if pt.GiftId == 0 && pt.User == nil {
			return nil, nil
		}

		return GiftEvent{
			MessageID:    pt.Common.MsgId,
			Timestamp:    pt.Common.CreateTime,
			ID:           pt.GiftId,
			GroupID:      pt.GroupId,
			Name:         pt.Gift.Name,
			Describe:     pt.Gift.Describe,
			Diamonds:     int(pt.Gift.DiamondCount),
			RepeatCount:  int(pt.RepeatCount),
			RepeatEnd:    pt.RepeatEnd == 1,
			Type:         int(pt.Gift.Type),
			ToUserID:     int64(pt.UserGiftReciever.UserId),
			User:         toUser(pt.User),
			UserIdentity: toUserIdentity(pt.UserIdentity),
			isHistory:    msg.IsHistory || cachedHistory(pt.Common.MsgId),
			IsComboGift:  pt.GroupId != 0,
		}, nil
	case *pb.WebcastLikeMessage:
		return LikeEvent{
			MessageID:   pt.Common.MsgId,
			Timestamp:   pt.Common.CreateTime,
			Likes:       int(pt.Count),
			TotalLikes:  int(pt.Total),
			User:        toUser(pt.User),
			DisplayType: pt.Common.Method,
			Label:       pt.Common.DisplayText.String(),
			isHistory:   msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil
	case *pb.WebcastSubNotifyMessage:
		return toSubNotifyEvent(pt, msg), nil
	case *pb.WebcastEmoteChatMessage:
		return toEmoteEvent(pt, msg), nil

	case *pb.WebcastQuestionNewMessage:
		return QuestionEvent{
			MessageID: pt.Common.MsgId,
			Timestamp: pt.Common.CreateTime,
			Quesion:   pt.Details.Text,
			User:      toUser(pt.Details.User),
			isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil

	case *pb.WebcastControlMessage:
		return ControlEvent{
			MessageID:   pt.Common.MsgId,
			Timestamp:   pt.Common.CreateTime,
			Action:      int(pt.Action),
			Description: pt.Action.String(),
			isHistory:   msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil

	case *pb.WebcastLinkMicBattle:
		users := []*User{}
		for _, u := range pt.HostTeam {
			groups := u.HostGroup
			for _, group := range groups {
				for _, user := range group.Host {
					urls := make([]string, 5)
					for _, img := range user.Images {
						urls = append(urls, img.UrlList...)
					}
					users = append(users, &User{
						ID:       int64(user.Id),
						Username: user.ProfileId,
						Nickname: user.Name,
						ProfilePicture: &ProfilePicture{
							Urls: urls,
						},
					})

				}

			}
		}
		return MicBattleEvent{
			MessageID: pt.Common.MsgId,
			Timestamp: pt.Common.CreateTime,
			Users:     users,
			isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil

	case *pb.WebcastLinkMicArmies:
		battles := []*Battle{}
		for _, b := range pt.BattleItems {
			battle := &Battle{
				Host:   int64(b.HostUserId),
				Groups: []*BattleGroup{},
			}
			for _, g := range b.BattleGroups {
				group := BattleGroup{
					Points: int(g.Points),
					Users:  []*User{},
				}
				for _, u := range g.Users {
					group.Users = append(group.Users, toUser(u))
				}
				battle.Groups = append(battle.Groups, &group)
			}
			battles = append(battles, battle)
		}
		return BattlesEvent{
			MessageID: pt.Common.MsgId,
			Timestamp: pt.Common.CreateTime,
			Status:    int(pt.BattleStatus),
			Battles:   battles,
			isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil
	case *pb.WebcastLiveIntroMessage:
		return IntroEvent{
			MessageID: pt.Common.MsgId,
			Timestamp: pt.Common.CreateTime,
			ID:        int(pt.RoomId),
			Title:     pt.Content,
			User:      toUser(pt.Host),
			isHistory: msg.IsHistory || cachedHistory(pt.Common.MsgId),
		}, nil

	case *pb.WebcastInRoomBannerMessage:
		var data interface{}
		// TODO: should we make a type for this instead of unmarshalling to see it is an error then feeding it up?
		err = json.Unmarshal([]byte(pt.GetJson()), &data)
		if err != nil {
			return nil, fmt.Errorf("WebcastInRoomBannerMessage: %w\n%s", err, data)
		}

		return RoomBannerEvent{
			MessageID: pt.Header.MsgId,
			Timestamp: pt.Header.CreateTime,
			Data:      data,
			isHistory: msg.IsHistory || cachedHistory(pt.Header.MsgId),
		}, nil

	default:
		base := base64.RawStdEncoding.EncodeToString(msg.Payload)
		err = fmt.Errorf("unimplemented type %T\n%s", m, base)
		debugHandler(err)
		warnHandler(fmt.Sprintf("unimplemented type %T", m))
		return nil, nil
	}
}

func cachedHistory(id int64) bool {
	_, present := msgIDCache.GetOrSet(id, struct{}{}, imcache.WithExpiration(messageHistoryTimeout))
	return present
}

func defaultLogHandler(i ...interface{}) {
	slog.Debug(fmt.Sprint(i...), "logger", "gotiktoklive-default")
}

func routineErrHandler(err ...interface{}) {
	slog.Debug(fmt.Sprint(err...), "logger", "gotiktoklive-default")
}

func toUser(u *pb.User) *User {
	if u == nil {
		return &User{}
	}
	username := firstNonEmpty(u.DisplayId, u.IdStr, u.Nickname)
	user := User{
		ID:       int64(u.Id),
		Username: username,
		Nickname: u.Nickname,
	}

	if u.AvatarJpg != nil && u.AvatarJpg.UrlList != nil {
		user.ProfilePicture = &ProfilePicture{
			Urls: u.AvatarJpg.UrlList,
		}
	} else if u.AvatarLarge != nil && u.AvatarLarge.UrlList != nil {
		user.ProfilePicture = &ProfilePicture{
			Urls: u.AvatarLarge.UrlList,
		}
	}

	user.ExtraAttributes = &ExtraAttributes{
		FollowRole: int(u.UserRole),
	}

	if u.BadgeList != nil {
		var badges []*UserBadge
		for _, badge := range u.BadgeList {
			badges = append(badges, &UserBadge{
				Type: badge.DisplayType.String(),
				Name: badge.String(),
			})
		}
		user.Badge = &BadgeAttributes{
			Badges: badges,
		}
	}
	return &user
}

func toUserIdentity(uid *pb.UserIdentity) *UserIdentity {
	if uid == nil {
		return nil
	}
	return &UserIdentity{
		IsGiftGiver:       uid.IsGiftGiverOfAnchor,
		IsSubscriber:      uid.IsSubscriberOfAnchor,
		IsMutualFollowing: uid.IsMutualFollowingWithAnchor,
		IsFollower:        uid.IsFollowerOfAnchor,
		IsModerator:       uid.IsModeratorOfAnchor,
		IsAnchor:          uid.IsAnchor,
	}
}

func toSuperFanEvent(pt *pb.WebcastBarrageMessage, msg *pb.WebcastResponse_Message) Event {
	common := pt.GetCommon()
	commonBarrageContent := parseBarrageCommonBarrageContent(msg.Payload)
	displayType := firstNonEmpty(
		textKey(pt.GetContent()),
		commonBarrageContent.Key,
		displayTextKey(common),
	)
	if displayType == "" {
		return nil
	}

	normalized := strings.ToLower(displayType)
	if !strings.Contains(normalized, "ttlive_superfan") {
		return nil
	}

	return SuperFanEvent{
		MessageID:      common.GetMsgId(),
		Timestamp:      common.GetCreateTime(),
		Join:           strings.Contains(normalized, "ttlive_superfan_commentnotif_superfanjoined"),
		DisplayType:    displayType,
		DefaultPattern: firstNonEmpty(textDefaultPattern(pt.GetContent()), commonBarrageContent.DefaultPattern, displayTextDefaultPattern(common)),
		User:           barrageUser(pt),
		isHistory:      msg.IsHistory || cachedHistory(common.GetMsgId()),
	}
}

func barrageUser(pt *pb.WebcastBarrageMessage) *User {
	if user := pt.GetUserGradeParam().GetUser(); user != nil {
		return toUser(user)
	}
	if user := pt.GetFansLevelParam().GetUser(); user != nil {
		return toUser(user)
	}
	return &User{}
}

func toSuperFanBoxEvent(pt *pb.WebcastEnvelopeMessage, msg *pb.WebcastResponse_Message) Event {
	common := pt.GetCommon()
	info := pt.GetEnvelopeInfo()
	extras := parseEnvelopeExtras(msg.Payload)
	businessType := int(info.GetBusinessType())
	if extras.BusinessType != 0 {
		businessType = extras.BusinessType
	}

	event := SuperFanBoxEvent{
		MessageID:     common.GetMsgId(),
		Timestamp:     common.GetCreateTime(),
		BusinessType:  businessType,
		EnvelopeID:    info.GetEnvelopeId(),
		SendUserName:  info.GetSendUserName(),
		SendUserID:    info.GetSendUserId(),
		DiamondCount:  int(info.GetDiamondCount()),
		PeopleCount:   int(info.GetPeopleCount()),
		SuperFanCount: extras.SuperFanCount,
		RoomID:        info.GetRoomId(),
		DisplayType:   displayTextKey(common),
		isHistory:     msg.IsHistory || cachedHistory(common.GetMsgId()),
	}

	if event.BusinessType == envelopeBusinessTypeSuperFanBox ||
		strings.Contains(strings.ToLower(event.DisplayType), "ttlive_superfanbox") {
		return event
	}

	return nil
}

func toSubNotifyEvent(pt *pb.WebcastSubNotifyMessage, msg *pb.WebcastResponse_Message) Event {
	common := pt.GetCommon()
	extras := parseSubNotifyExtras(msg.Payload)
	return SubNotifyEvent{
		MessageID:          common.GetMsgId(),
		Timestamp:          common.GetCreateTime(),
		User:               toUser(pt.GetUser()),
		DisplayType:        displayTextKey(common),
		ExhibitionType:     extras.ExhibitionType,
		SubMonth:           int(pt.GetSubMonth()),
		SubscribeType:      int(pt.GetSubscribeType()),
		OldSubscribeStatus: int(pt.GetOldSubscribeStatus()),
		SubscribingStatus:  int(pt.GetSubscribingStatus()),
		GiftSource:         extras.GiftSource,
		IsSend:             pt.GetIsSend(),
		IsCustom:           pt.GetIsCustom(),
		PackageID:          extras.PackageID,
		isHistory:          msg.IsHistory || cachedHistory(common.GetMsgId()),
	}
}

func toEmoteEvent(pt *pb.WebcastEmoteChatMessage, msg *pb.WebcastResponse_Message) Event {
	emotes := make([]Emote, 0, len(pt.GetEmoteList()))
	for _, emote := range pt.GetEmoteList() {
		next := Emote{
			ID:        emote.GetEmoteId(),
			UUID:      emote.GetUuid(),
			ImageURLs: imageURLs(emote.GetImage()),
		}
		if next.ID != "" || next.UUID != "" || len(next.ImageURLs) > 0 {
			emotes = append(emotes, next)
		}
	}
	if len(emotes) == 0 {
		return nil
	}

	common := pt.GetCommon()
	return EmoteEvent{
		MessageID: common.GetMsgId(),
		Timestamp: common.GetCreateTime(),
		User:      toUser(pt.GetUser()),
		Emotes:    emotes,
		isHistory: msg.IsHistory || cachedHistory(common.GetMsgId()),
	}
}

func displayTextKey(common *pb.Common) string {
	return textKey(common.GetDisplayText())
}

func displayTextDefaultPattern(common *pb.Common) string {
	return textDefaultPattern(common.GetDisplayText())
}

func textKey(text *pb.Text) string {
	if text == nil {
		return ""
	}
	return text.GetKey()
}

func textDefaultPattern(text *pb.Text) string {
	if text == nil {
		return ""
	}
	return text.GetDefaultPattern()
}

func imageURLs(image *pb.Image) []string {
	if image == nil {
		return nil
	}
	return append([]string(nil), image.GetUrlList()...)
}

type protoField struct {
	num    protowire.Number
	typ    protowire.Type
	varint uint64
	bytes  []byte
}

type textValue struct {
	Key            string
	DefaultPattern string
}

type envelopeExtras struct {
	BusinessType  int
	SuperFanCount int
}

type subNotifyExtras struct {
	ExhibitionType int
	GiftSource     int
	PackageID      string
}

func forEachField(b []byte, fn func(protoField) error) error {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return protowire.ParseError(n)
		}
		b = b[n:]

		field := protoField{num: num, typ: typ}
		switch typ {
		case protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			field.varint = v
			b = b[n:]
		case protowire.BytesType:
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			field.bytes = v
			b = b[n:]
		case protowire.Fixed32Type:
			_, n := protowire.ConsumeFixed32(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			b = b[n:]
		case protowire.Fixed64Type:
			_, n := protowire.ConsumeFixed64(b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return protowire.ParseError(n)
			}
			b = b[n:]
		}

		if err := fn(field); err != nil {
			return err
		}
	}
	return nil
}

func parseTextBytes(b []byte) textValue {
	var text textValue
	_ = forEachField(b, func(field protoField) error {
		if field.typ != protowire.BytesType {
			return nil
		}

		switch field.num {
		case 1:
			text.Key = string(field.bytes)
		case 2:
			text.DefaultPattern = string(field.bytes)
		}

		return nil
	})
	return text
}

func parseBarrageCommonBarrageContent(b []byte) textValue {
	var text textValue
	_ = forEachField(b, func(field protoField) error {
		if field.num == 24 && field.typ == protowire.BytesType {
			text = parseTextBytes(field.bytes)
		}
		return nil
	})
	return text
}

func parseEnvelopeExtras(b []byte) envelopeExtras {
	var extras envelopeExtras
	_ = forEachField(b, func(field protoField) error {
		if field.num == 2 && field.typ == protowire.BytesType {
			extras = parseEnvelopeInfoExtras(field.bytes)
		}
		return nil
	})
	return extras
}

func parseEnvelopeInfoExtras(b []byte) envelopeExtras {
	var extras envelopeExtras
	_ = forEachField(b, func(field protoField) error {
		switch field.num {
		case 2:
			if field.typ == protowire.VarintType {
				extras.BusinessType = int(field.varint)
			}
		case 16:
			if field.typ == protowire.VarintType {
				extras.SuperFanCount = int(field.varint)
			}
		}
		return nil
	})
	return extras
}

func parseSubNotifyExtras(b []byte) subNotifyExtras {
	var extras subNotifyExtras
	_ = forEachField(b, func(field protoField) error {
		switch field.num {
		case 3:
			if field.typ == protowire.VarintType {
				extras.ExhibitionType = int(field.varint)
			}
		case 11:
			if field.typ == protowire.VarintType {
				extras.GiftSource = int(field.varint)
			}
		case 14:
			if field.typ == protowire.BytesType {
				extras.PackageID = string(field.bytes)
			}
		}
		return nil
	})
	return extras
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string)
	for key, value := range m {
		out[key] = value
	}
	return out
}

func toUserType(displayType string) userEventType {
	normalized := strings.ToLower(displayType)
	switch {
	case normalized == "pm_main_follow_message_viewer_2" ||
		strings.Contains(normalized, "follow"):
		return USER_FOLLOW
	case normalized == "pm_mt_guidance_share" ||
		strings.Contains(normalized, "share"):
		return USER_SHARE
	case normalized == "live_room_enter_toast" ||
		strings.Contains(normalized, "enter") ||
		strings.Contains(normalized, "join"):
		return USER_JOIN
	}
	return userEventType(fmt.Sprintf("User type not implemented, please report: %s", displayType))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
