package gotiktoklive

import (
	"testing"

	pb "github.com/steampoweredtaco/gotiktoklive/proto"
	"google.golang.org/protobuf/proto"
)

func TestParseSuperFanBarrageMessage(t *testing.T) {
	out := parseTestMessage(t, "WebcastBarrageMessage", &pb.WebcastBarrageMessage{
		Common: testCommon(101, "WebcastBarrageMessage", ""),
		Content: &pb.Text{
			Key:            "ttlive_superfan_commentnotif_superfanjoined",
			DefaultPattern: "A Super Fan joined",
		},
		FansLevelParam: &pb.WebcastBarrageMessage_BarrageTypeFansLevelParam{
			User: testUser(),
		},
	})

	event, ok := out.(SuperFanEvent)
	if !ok {
		t.Fatalf("expected SuperFanEvent, got %T", out)
	}
	if !event.Join {
		t.Fatal("expected join Super Fan event")
	}
	if event.DisplayType != "ttlive_superfan_commentnotif_superfanjoined" {
		t.Fatalf("unexpected display type: %s", event.DisplayType)
	}
	if event.User.Username != "tester" {
		t.Fatalf("unexpected user: %+v", event.User)
	}
}

func TestParseSuperFanBoxEnvelopeMessage(t *testing.T) {
	out := parseTestMessage(t, "WebcastEnvelopeMessage", &pb.WebcastEnvelopeMessage{
		Common: testCommon(102, "WebcastEnvelopeMessage", ""),
		EnvelopeInfo: &pb.WebcastEnvelopeMessage_EnvelopeInfo{
			EnvelopeId:   "env-1",
			BusinessType: pb.EnvelopeBusinessType(envelopeBusinessTypeSuperFanBox),
			SendUserName: "sender",
			SendUserId:   "42",
			DiamondCount: 100,
			PeopleCount:  3,
			RoomId:       "room-1",
		},
	})

	event, ok := out.(SuperFanBoxEvent)
	if !ok {
		t.Fatalf("expected SuperFanBoxEvent, got %T", out)
	}
	if event.BusinessType != envelopeBusinessTypeSuperFanBox {
		t.Fatalf("unexpected business type: %d", event.BusinessType)
	}
	if event.EnvelopeID != "env-1" || event.SendUserID != "42" {
		t.Fatalf("unexpected envelope data: %+v", event)
	}
}

func TestParseSocialMessageUsesCommonDisplayText(t *testing.T) {
	out := parseTestMessage(t, "WebcastSocialMessage", &pb.WebcastSocialMessage{
		Common: testCommon(103, "WebcastSocialMessage", "pm_main_follow_message_viewer_2"),
		User:   testUser(),
	})

	event, ok := out.(UserEvent)
	if !ok {
		t.Fatalf("expected UserEvent, got %T", out)
	}
	if event.Event != USER_FOLLOW {
		t.Fatalf("unexpected user event: %s", event.Event)
	}
	if event.User.Username != "tester" {
		t.Fatalf("unexpected user: %+v", event.User)
	}
}

func TestParseSocialMessageKeepsUnknownDisplayType(t *testing.T) {
	out := parseTestMessage(t, "WebcastSocialMessage", &pb.WebcastSocialMessage{
		Common: testCommon(104, "WebcastSocialMessage", "unknown_social_display_type"),
		User:   testUser(),
	})

	event, ok := out.(UserEvent)
	if !ok {
		t.Fatalf("expected UserEvent, got %T", out)
	}
	if event.Event != userEventType("User type not implemented, please report: unknown_social_display_type") {
		t.Fatalf("unexpected user event: %s", event.Event)
	}
}

func TestParseSubNotifyMessage(t *testing.T) {
	out := parseTestMessage(t, "WebcastSubNotifyMessage", &pb.WebcastSubNotifyMessage{
		Common:        testCommon(105, "WebcastSubNotifyMessage", "sub_notify"),
		User:          testUser(),
		SubMonth:      6,
		SubscribeType: pb.SubscribeType_SUBSCRIBETYPE_AUTO,
		IsSend:        true,
	})

	event, ok := out.(SubNotifyEvent)
	if !ok {
		t.Fatalf("expected SubNotifyEvent, got %T", out)
	}
	if event.SubMonth != 6 || event.SubscribeType != int(pb.SubscribeType_SUBSCRIBETYPE_AUTO) || !event.IsSend {
		t.Fatalf("unexpected sub notify data: %+v", event)
	}
	if event.User.Username != "tester" {
		t.Fatalf("unexpected user: %+v", event.User)
	}
}

func TestParseEmoteChatMessage(t *testing.T) {
	out := parseTestMessage(t, "WebcastEmoteChatMessage", &pb.WebcastEmoteChatMessage{
		Common: testCommon(106, "WebcastEmoteChatMessage", ""),
		User:   testUser(),
		EmoteList: []*pb.Emote{{
			EmoteId: "emote-1",
			Uuid:    "uuid-1",
			Image: &pb.Image{
				UrlList: []string{"https://example.test/emote.png"},
			},
		}},
	})

	event, ok := out.(EmoteEvent)
	if !ok {
		t.Fatalf("expected EmoteEvent, got %T", out)
	}
	if len(event.Emotes) != 1 || event.Emotes[0].ID != "emote-1" || len(event.Emotes[0].ImageURLs) != 1 {
		t.Fatalf("unexpected emote data: %+v", event)
	}
}

func parseTestMessage(t *testing.T, method string, m proto.Message) Event {
	t.Helper()

	payload, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	out, err := parseMsg(&pb.WebcastResponse_Message{
		Method:  method,
		Payload: payload,
	}, silentLog, silentLog, false)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func testCommon(messageID int64, method string, displayKey string) *pb.Common {
	return &pb.Common{
		Method:     method,
		MsgId:      messageID,
		CreateTime: 12345,
		DisplayText: &pb.Text{
			Key: displayKey,
		},
	}
}

func testUser() *pb.User {
	return &pb.User{
		Id:        123,
		IdStr:     "123",
		DisplayId: "tester",
		Nickname:  "Test User",
	}
}

func silentLog(...interface{}) {}
