package wa

import (
	"testing"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestParseLiveMessageAdReferral(t *testing.T) {
	chat, _ := types.ParseJID("123@s.whatsapp.net")
	ev := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: chat},
			ID:            "mid",
			Timestamp:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			PushName:      "Lead",
		},
		Message: &waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{
				Text: proto.String("Hi, I saw your ad"),
				ContextInfo: &waProto.ContextInfo{
					ExternalAdReply: &waProto.ContextInfo_ExternalAdReplyInfo{
						Title:      proto.String("Train with a coach"),
						Body:       proto.String("Book a free call"),
						SourceType: proto.String("ad"),
						SourceID:   proto.String("120200000000000001"),
						SourceURL:  proto.String("https://fb.me/example"),
						SourceApp:  proto.String("instagram"),
						CtwaClid:   proto.String("clid-1"),
					},
					ConversionSource:           proto.String("FB_Ads"),
					EntryPointConversionSource: proto.String("ctwa_ad"),
					EntryPointConversionApp:    proto.String("instagram"),
				},
			},
		},
	}

	want := AdReferral{
		SourceType:       "ad",
		SourceID:         "120200000000000001",
		SourceURL:        "https://fb.me/example",
		SourceApp:        "instagram",
		Title:            "Train with a coach",
		Body:             "Book a free call",
		CtwaClid:         "clid-1",
		ConversionSource: "FB_Ads",
		EntryPointSource: "ctwa_ad",
		EntryPointApp:    "instagram",
	}
	pm := ParseLiveMessage(ev)
	if pm.AdReferral == nil || *pm.AdReferral != want {
		t.Fatalf("AdReferral = %+v, want %+v", pm.AdReferral, want)
	}
	if pm.Text != "Hi, I saw your ad" {
		t.Fatalf("Text = %q", pm.Text)
	}

	ev.Message.ExtendedTextMessage.ContextInfo = &waProto.ContextInfo{IsForwarded: proto.Bool(true)}
	if got := ParseLiveMessage(ev).AdReferral; got != nil {
		t.Fatalf("AdReferral without ad context = %+v, want nil", got)
	}
}
