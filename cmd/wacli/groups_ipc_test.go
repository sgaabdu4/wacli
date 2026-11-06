package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/lock"
	"github.com/openclaw/wacli/internal/store"
	"go.mau.fi/whatsmeow/types"
)

func TestGroupReadsDelegateWhileSyncOwnsStore(t *testing.T) {
	skipPresenceDelegateSocketTestOnUnsupportedOS(t)
	for _, tc := range []struct {
		kind string
		args []string
	}{
		{"groups_info", []string{"groups", "info", "--jid", "support@g.us"}},
		{"groups_invite_link", []string{"groups", "invite", "link", "get", "--jid", "support@g.us"}},
		{"groups_participants", []string{"groups", "participants", "list", "--jid", "support@g.us"}},
		{"groups_refresh", []string{"groups", "refresh"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			storeDir := shortPresenceDelegateStoreDir(t)
			lk, err := lock.Acquire(storeDir)
			if err != nil {
				t.Fatal(err)
			}
			defer lk.Release()
			called := make(chan sendDelegateRequest, 1)
			stop, err := startSendDelegateServerForStore(context.Background(), storeDir, sendSpacing{}, func(_ context.Context, req sendDelegateRequest) (sendDelegateResponse, error) {
				called <- req
				return sendDelegateResponse{}, fmt.Errorf("group read reached running sync")
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			args := append([]string{"--store", storeDir, "--json", "--timeout", "750ms"}, tc.args...)
			_, _, _ = runPresenceDelegateHelper(t, args)
			select {
			case req := <-called:
				if req.Kind != tc.kind {
					t.Fatalf("kind = %s, want %s", req.Kind, tc.kind)
				}
				if tc.kind != "groups_refresh" && req.To != "support@g.us" {
					t.Fatal("group target was lost")
				}
			default:
				t.Fatal("group read did not reach the running sync")
			}
		})
	}
}

func TestLiveGroupReadsWithoutSyncNeverOpenAnotherSession(t *testing.T) {
	for _, args := range [][]string{
		{"groups", "info", "--jid", "support@g.us"},
		{"groups", "invite", "link", "get", "--jid", "support@g.us"},
		{"groups", "refresh"},
	} {
		t.Run(strings.Join(args[:2], "-"), func(t *testing.T) {
			storeDir := shortPresenceDelegateStoreDir(t)
			_, stderr, err := runPresenceDelegateHelper(t, append([]string{"--store", storeDir, "--json"}, args...))
			if err == nil || !strings.Contains(stderr, "running sync") {
				t.Error("read must report the missing sync")
			}
			if _, err := os.Stat(filepath.Join(storeDir, "session.db")); !os.IsNotExist(err) {
				t.Error("read opened a standalone WhatsApp session")
			}
		})
	}
}

type groupReadWA struct {
	app.WAClient
	info *types.GroupInfo
	err  error
}

func (f *groupReadWA) GetGroupInfo(context.Context, types.JID) (*types.GroupInfo, error) {
	return f.info, f.err
}
func (f *groupReadWA) GetJoinedGroups(context.Context) ([]*types.GroupInfo, error) {
	return []*types.GroupInfo{f.info}, f.err
}
func (f *groupReadWA) GetGroupInviteLink(_ context.Context, _ types.JID, reset bool) (string, error) {
	if reset {
		return "", fmt.Errorf("read must not reset invite")
	}
	return "https://chat.whatsapp.com/TestInviteCode1234", f.err
}
func (f *groupReadWA) ResolveLIDToPN(_ context.Context, jid types.JID) types.JID { return jid }

func TestGroupReadResultsBypassBlockedSendsAndKeepSnapshots(t *testing.T) {
	skipPresenceDelegateSocketTestOnUnsupportedOS(t)
	storeDir := shortPresenceDelegateStoreDir(t)
	lk, err := lock.Acquire(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Release()
	db, err := store.Open(filepath.Join(storeDir, "wacli.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	jid := types.NewJID("support", types.GroupServer)
	fake := &groupReadWA{info: &types.GroupInfo{
		JID: jid, GroupName: types.GroupName{Name: "Support"},
		Participants: []types.GroupParticipant{{JID: types.NewJID("member", types.DefaultUserServer), IsAdmin: true}},
	}}
	blocked := make(chan struct{})
	release := make(chan struct{})
	stop, err := startSendDelegateServerForStore(context.Background(), storeDir, sendSpacing{min: time.Hour, max: time.Hour}, func(ctx context.Context, req sendDelegateRequest) (sendDelegateResponse, error) {
		if req.Kind == "text" {
			close(blocked)
			<-release
			return sendDelegateResponse{OK: true}, nil
		}
		return executeDelegatedGroupRead(ctx, db, fake, req)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	sendDone := make(chan struct{})
	go func() {
		defer close(sendDone)
		_, _ = delegateSend(context.Background(), &rootFlags{storeDir: storeDir, timeout: time.Second}, sendDelegateRequest{Kind: "text"})
	}()
	defer func() { close(release); <-sendDone }()
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("send did not start")
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"info", "--jid", jid.String()}, `"Name":"Support"`},
		{[]string{"invite", "link", "get", "--jid", jid.String()}, `"link":"https://chat.whatsapp.com/TestInviteCode1234"`},
		{[]string{"participants", "list", "--jid", jid.String()}, `"role":"admin"`},
		{[]string{"refresh"}, `"groups":1`},
	} {
		args := append([]string{"--store", storeDir, "--json", "--timeout", "750ms", "groups"}, tc.args...)
		stdout, _, err := runPresenceDelegateHelper(t, args)
		if err != nil {
			t.Fatalf("group read failed: %v", err)
		}
		if !strings.Contains(stdout, tc.want) {
			t.Fatal("group read returned wrong data")
		}
		if !strings.Contains(stdout, `"success":true`) {
			t.Fatal("invalid group response")
		}
	}
	participants, err := db.ListGroupParticipants(jid.String())
	if err != nil || len(participants) != 1 {
		t.Fatal("group info did not persist participant snapshot")
	}
	fake.err = fmt.Errorf("not participating in that group")
	_, stderr, err := runPresenceDelegateHelper(t, []string{"--store", storeDir, "--json", "groups", "info", "--jid", jid.String()})
	if err == nil || !strings.Contains(stderr, fake.err.Error()) {
		t.Fatal("read error was not preserved")
	}
}
