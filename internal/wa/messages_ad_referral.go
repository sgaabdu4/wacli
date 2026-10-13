package wa

import (
	"strings"

	waProto "go.mau.fi/whatsmeow/binary/proto"
)

// AdReferral is the ad attribution WhatsApp attaches to the first message a
// person sends after tapping a Click-to-WhatsApp ad.
type AdReferral struct {
	SourceType       string
	SourceID         string
	SourceURL        string
	SourceApp        string
	Title            string
	Body             string
	CtwaClid         string
	ConversionSource string
	EntryPointSource string
	EntryPointApp    string
}

func adReferralFromContext(ctx *waProto.ContextInfo) *AdReferral {
	ad := ctx.GetExternalAdReply()
	ref := AdReferral{
		SourceType:       strings.TrimSpace(ad.GetSourceType()),
		SourceID:         strings.TrimSpace(ad.GetSourceID()),
		SourceURL:        strings.TrimSpace(ad.GetSourceURL()),
		SourceApp:        strings.TrimSpace(ad.GetSourceApp()),
		Title:            strings.TrimSpace(ad.GetTitle()),
		Body:             strings.TrimSpace(ad.GetBody()),
		CtwaClid:         strings.TrimSpace(ad.GetCtwaClid()),
		ConversionSource: strings.TrimSpace(ctx.GetConversionSource()),
		EntryPointSource: strings.TrimSpace(ctx.GetEntryPointConversionSource()),
		EntryPointApp:    strings.TrimSpace(ctx.GetEntryPointConversionApp()),
	}
	if ref == (AdReferral{}) {
		return nil
	}
	return &ref
}
