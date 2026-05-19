package gotiktoklive

import (
	"testing"

	pb "github.com/Davincible/gotiktoklive/proto"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestParseSuperFanBarrageMessage(t *testing.T) {
	content := protoMsg(
		protoString(1, "ttlive_superfan_commentnotif_superfanjoined"),
		protoString(2, "A Super Fan joined"),
	)
	binary := protoMsg(
		protoBytes(5, content),
		protoBytes(50, protoUser("123", "tester", "Test User")),
	)

	out, err := parseMsg(&pb.Message{Type: "WebcastBarrageMessage", Binary: binary}, silentWarn)
	if err != nil {
		t.Fatal(err)
	}

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
	envelopeInfo := protoMsg(
		protoString(1, "env-1"),
		protoVarint(2, envelopeBusinessTypeSuperFanBox),
		protoString(4, "sender"),
		protoVarint(5, 100),
		protoVarint(6, 3),
		protoString(8, "42"),
		protoString(11, "room-1"),
		protoVarint(16, 2),
	)
	binary := protoMsg(
		protoBytes(1, protoCommon("ttlive_superfanbox")),
		protoBytes(2, envelopeInfo),
	)

	out, err := parseMsg(&pb.Message{Type: "WebcastEnvelopeMessage", Binary: binary}, silentWarn)
	if err != nil {
		t.Fatal(err)
	}

	event, ok := out.(SuperFanBoxEvent)
	if !ok {
		t.Fatalf("expected SuperFanBoxEvent, got %T", out)
	}
	if event.BusinessType != envelopeBusinessTypeSuperFanBox {
		t.Fatalf("unexpected business type: %d", event.BusinessType)
	}
	if event.EnvelopeID != "env-1" || event.SuperFanCount != 2 {
		t.Fatalf("unexpected envelope data: %+v", event)
	}
}

func TestParseSocialMessageUsesCommonDisplayText(t *testing.T) {
	binary := protoMsg(
		protoBytes(1, protoCommon("pm_main_follow_message_viewer_2")),
		protoBytes(2, protoUser("123", "tester", "Test User")),
	)

	out, err := parseMsg(&pb.Message{Type: "WebcastSocialMessage", Binary: binary}, silentWarn)
	if err != nil {
		t.Fatal(err)
	}

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
	binary := protoMsg(
		protoBytes(1, protoCommon("unknown_social_display_type")),
		protoBytes(2, protoUser("123", "tester", "Test User")),
	)

	out, err := parseMsg(&pb.Message{Type: "WebcastSocialMessage", Binary: binary}, silentWarn)
	if err != nil {
		t.Fatal(err)
	}

	event, ok := out.(UserEvent)
	if !ok {
		t.Fatalf("expected UserEvent, got %T", out)
	}
	if event.Event != userEventType("User type not implemented, please report: unknown_social_display_type") {
		t.Fatalf("unexpected user event: %s", event.Event)
	}
}

func TestParseSubNotifyMessage(t *testing.T) {
	binary := protoMsg(
		protoBytes(1, protoCommon("sub_notify")),
		protoBytes(2, protoUser("123", "tester", "Test User")),
		protoVarint(4, 6),
		protoVarint(5, 1),
		protoVarint(9, 1),
		protoString(14, "package-1"),
	)

	out, err := parseMsg(&pb.Message{Type: "WebcastSubNotifyMessage", Binary: binary}, silentWarn)
	if err != nil {
		t.Fatal(err)
	}

	event, ok := out.(SubNotifyEvent)
	if !ok {
		t.Fatalf("expected SubNotifyEvent, got %T", out)
	}
	if event.SubMonth != 6 || event.PackageID != "package-1" || !event.IsSend {
		t.Fatalf("unexpected sub notify data: %+v", event)
	}
	if event.User.Username != "tester" {
		t.Fatalf("unexpected user: %+v", event.User)
	}
}

func TestParseEmoteChatMessage(t *testing.T) {
	emote := protoMsg(
		protoString(1, "emote-1"),
		protoBytes(2, protoString(1, "https://example.test/emote.png")),
	)
	binary := protoMsg(
		protoBytes(2, protoUser("123", "tester", "Test User")),
		protoBytes(3, emote),
	)

	out, err := parseMsg(&pb.Message{Type: "WebcastEmoteChatMessage", Binary: binary}, silentWarn)
	if err != nil {
		t.Fatal(err)
	}

	event, ok := out.(EmoteEvent)
	if !ok {
		t.Fatalf("expected EmoteEvent, got %T", out)
	}
	if event.EmoteID != "emote-1" || event.ImageURL == "" {
		t.Fatalf("unexpected emote data: %+v", event)
	}
}

func silentWarn(...interface{}) {}

func protoMsg(fields ...[]byte) []byte {
	var out []byte
	for _, field := range fields {
		out = append(out, field...)
	}
	return out
}

func protoString(number protowire.Number, value string) []byte {
	out := protowire.AppendTag(nil, number, protowire.BytesType)
	return protowire.AppendString(out, value)
}

func protoBytes(number protowire.Number, value []byte) []byte {
	out := protowire.AppendTag(nil, number, protowire.BytesType)
	return protowire.AppendBytes(out, value)
}

func protoVarint(number protowire.Number, value uint64) []byte {
	out := protowire.AppendTag(nil, number, protowire.VarintType)
	return protowire.AppendVarint(out, value)
}

func protoCommon(displayKey string) []byte {
	return protoBytes(8, protoString(1, displayKey))
}

func protoUser(id string, username string, nickname string) []byte {
	return protoMsg(
		protoVarint(1, mustParseUint(id)),
		protoString(3, nickname),
		protoString(38, username),
	)
}

func mustParseUint(value string) uint64 {
	var out uint64
	for _, ch := range value {
		out = out*10 + uint64(ch-'0')
	}
	return out
}
