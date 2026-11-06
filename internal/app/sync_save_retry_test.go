package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/wa"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const saveRetryBody = "private body text"

func newSaveRetryApp(t *testing.T) (*App, *events.Message, *bytes.Buffer) {
	t.Helper()
	a := newTestApp(t)
	a.wa = newFakeWA()
	var eventLog bytes.Buffer
	a.opts.Events = out.NewEventWriter(&eventLog, true)
	previous := liveSaveRetryDelays
	liveSaveRetryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { liveSaveRetryDelays = previous })

	group := types.JID{User: "120363000000000001", Server: types.GroupServer}
	sender := types.JID{User: "123", Server: types.DefaultUserServer}
	return a, &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: group, Sender: sender},
			ID:            "live-save-1",
			Timestamp:     time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
			PushName:      "Alice",
		},
		Message: &waProto.Message{Conversation: proto.String(saveRetryBody)},
	}, &eventLog
}

func warningEvents(t *testing.T, eventLog *bytes.Buffer) []map[string]any {
	t.Helper()
	var warnings []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(eventLog.String()), "\n") {
		if line == "" {
			continue
		}
		var envelope struct {
			Event string         `json:"event"`
			Data  map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			t.Fatalf("event line %q: %v", line, err)
		}
		if envelope.Event == "warning" {
			warnings = append(warnings, envelope.Data)
		}
	}
	return warnings
}

func TestLiveSyncRetriesBusyStoreAndKeepsMessage(t *testing.T) {
	a, evt, eventLog := newSaveRetryApp(t)
	opts := SyncOptions{failLiveStore: func(attempt int) error {
		if attempt == 0 {
			return sqlite3.Error{Code: sqlite3.ErrBusy}
		}
		return nil
	}}

	var stored atomic.Int64
	a.handleLiveSyncMessage(context.Background(), opts, evt, &stored, func(string, string) {}, nil)

	if _, err := a.db.GetMessage(evt.Info.Chat.String(), evt.Info.ID); err != nil {
		t.Fatalf("message not saved after a busy first insert: %v", err)
	}
	if stored.Load() != 1 {
		t.Fatalf("messages stored = %d, want 1", stored.Load())
	}
	warnings := warningEvents(t, eventLog)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	w := warnings[0]
	if w["code"] != "live_message_save_retry" || w["chat_jid"] != evt.Info.Chat.String() || w["msg_id"] != evt.Info.ID {
		t.Fatalf("retry warning = %v", w)
	}
	if !strings.Contains(w["error"].(string), "database is locked") {
		t.Fatalf("retry warning error = %v", w["error"])
	}
	if strings.Contains(eventLog.String(), saveRetryBody) {
		t.Fatalf("events leaked the message body:\n%s", eventLog)
	}
}

func TestLiveSyncWarnsAfterBusyRetriesRunOut(t *testing.T) {
	a, evt, eventLog := newSaveRetryApp(t)
	var attempts int
	opts := SyncOptions{failLiveStore: func(int) error {
		attempts++
		return sqlite3.Error{Code: sqlite3.ErrBusy}
	}}

	var stored atomic.Int64
	a.handleLiveSyncMessage(context.Background(), opts, evt, &stored, func(string, string) {}, nil)

	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4", attempts)
	}
	if stored.Load() != 0 {
		t.Fatalf("messages stored = %d, want 0", stored.Load())
	}
	warnings := warningEvents(t, eventLog)
	if len(warnings) != 4 {
		t.Fatalf("warnings = %d, want one per attempt (4)", len(warnings))
	}
	last := warnings[len(warnings)-1]
	if last["code"] != "live_message_save_failed" || last["chat_jid"] != evt.Info.Chat.String() || last["msg_id"] != evt.Info.ID {
		t.Fatalf("final warning = %v", last)
	}
	if strings.Contains(eventLog.String(), saveRetryBody) {
		t.Fatalf("events leaked the message body:\n%s", eventLog)
	}
}

func TestLiveSyncWarnsOnceWithoutRetryForPermanentStoreError(t *testing.T) {
	a, evt, eventLog := newSaveRetryApp(t)
	raw := openRawWacliDB(t, a)
	if _, err := raw.Exec(`CREATE TRIGGER fail_live_save BEFORE INSERT ON messages BEGIN SELECT RAISE(FAIL, 'injected save failure'); END`); err != nil {
		t.Fatal(err)
	}

	var stored atomic.Int64
	a.handleLiveSyncMessage(context.Background(), SyncOptions{}, evt, &stored, func(string, string) {}, nil)

	warnings := warningEvents(t, eventLog)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	w := warnings[0]
	if w["code"] != "live_message_save_failed" || w["msg_id"] != evt.Info.ID || !strings.Contains(w["error"].(string), "injected save failure") {
		t.Fatalf("warning = %v", w)
	}
	if stored.Load() != 0 {
		t.Fatalf("messages stored = %d, want 0", stored.Load())
	}
}

type cancelOnRetryWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelOnRetryWriter) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if bytes.Contains(p, []byte(`"code":"live_message_save_retry"`)) {
		w.cancel()
	}
	return n, err
}

func TestLiveSyncWarnsWhenCancelledDuringRetryWait(t *testing.T) {
	a, evt, _ := newSaveRetryApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := &cancelOnRetryWriter{cancel: cancel}
	a.opts.Events = out.NewEventWriter(log, true)
	liveSaveRetryDelays = []time.Duration{time.Hour}
	var attempts int
	opts := SyncOptions{failLiveStore: func(int) error {
		attempts++
		return sqlite3.Error{Code: sqlite3.ErrBusy}
	}}

	err := a.storeLiveMessage(ctx, opts, wa.ParseLiveMessage(evt), messageStoreOptions{})

	if err == nil {
		t.Fatal("expected the save to fail")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1: nothing may be written after cancellation", attempts)
	}
	warnings := warningEvents(t, &log.Buffer)
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want a retry then a final failure", warnings)
	}
	last := warnings[1]
	if last["code"] != "live_message_save_failed" || last["chat_jid"] != evt.Info.Chat.String() || last["msg_id"] != evt.Info.ID || last["attempt"] != float64(1) {
		t.Fatalf("final warning = %v", last)
	}
}

func TestLiveSaveRetryDelaysAreShortAndBounded(t *testing.T) {
	want := []time.Duration{100 * time.Millisecond, 500 * time.Millisecond, 2 * time.Second}
	if len(liveSaveRetryDelays) != len(want) {
		t.Fatalf("delays = %v, want %v", liveSaveRetryDelays, want)
	}
	for i := range want {
		if liveSaveRetryDelays[i] != want[i] {
			t.Fatalf("delays = %v, want %v", liveSaveRetryDelays, want)
		}
	}
}
