package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/lock"
	"github.com/openclaw/wacli/internal/store"
	"github.com/openclaw/wacli/internal/wa"
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

func TestGroupWritesDelegateWhileSyncOwnsStore(t *testing.T) {
	skipPresenceDelegateSocketTestOnUnsupportedOS(t)
	for _, args := range [][]string{
		{"rename", "--jid", "support@g.us", "--name", "Renamed"},
		{"leave", "--jid", "support@g.us"},
		{"create", "--name", "Support", "--user", "member@s.whatsapp.net"},
		{"participants", "add", "--jid", "support@g.us", "--user", "member@s.whatsapp.net"},
		{"participants", "remove", "--jid", "support@g.us", "--user", "member@s.whatsapp.net"},
		{"participants", "promote", "--jid", "support@g.us", "--user", "member@s.whatsapp.net"},
		{"participants", "demote", "--jid", "support@g.us", "--user", "member@s.whatsapp.net"},
	} {
		t.Run(strings.Join(args[:2], "-"), func(t *testing.T) {
			storeDir := shortPresenceDelegateStoreDir(t)
			lk, err := lock.Acquire(storeDir)
			if err != nil {
				t.Fatal(err)
			}
			defer lk.Release()
			called := make(chan sendDelegateRequest, 1)
			stop, err := startSendDelegateServerForStore(context.Background(), storeDir, sendSpacing{}, func(_ context.Context, req sendDelegateRequest) (sendDelegateResponse, error) {
				called <- req
				return sendDelegateResponse{}, fmt.Errorf("group write reached saving sync")
			})
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			command := append([]string{"--store", storeDir, "--json", "--timeout", "750ms", "groups"}, args...)
			_, _, _ = runPresenceDelegateHelper(t, command)
			select {
			case req := <-called:
				if req.Kind != "groups_mutate" {
					t.Fatalf("kind = %s", req.Kind)
				}
			default:
				t.Fatal("group write opened a standalone connection instead of saving sync")
			}
		})
	}
}

type groupWriteWA struct {
	groupReadWA
	action string
}

func (f *groupWriteWA) SetGroupName(_ context.Context, jid types.JID, name string) error {
	if jid != f.info.JID || name != "Renamed" {
		return fmt.Errorf("rename arguments lost")
	}
	f.action = "rename"
	f.info.GroupName.Name = name
	return f.err
}
func (f *groupWriteWA) SetGroupPhoto(_ context.Context, jid types.JID, photo []byte) (string, error) {
	if jid != f.info.JID {
		return "", fmt.Errorf("picture target lost")
	}
	if _, err := jpeg.DecodeConfig(bytes.NewReader(photo)); err != nil {
		return "", err
	}
	f.action = "picture"
	return "picture-id", f.err
}
func (f *groupWriteWA) CreateGroup(_ context.Context, req wa.CreateGroupRequest) (*types.GroupInfo, error) {
	if req.Name != "Support" || len(req.Participants) != 1 || req.Participants[0].User != "member" || !req.IsAnnounce || !req.IsLocked || !req.IsJoinApprovalRequired || req.LinkedParentJID.User != "community" {
		return nil, fmt.Errorf("create arguments lost")
	}
	f.action = "create"
	return f.info, f.err
}
func (f *groupWriteWA) LeaveGroup(_ context.Context, jid types.JID) error {
	if jid != f.info.JID {
		return fmt.Errorf("leave target lost")
	}
	f.action = "leave"
	return f.err
}
func (f *groupWriteWA) UpdateGroupParticipants(_ context.Context, jid types.JID, users []types.JID, action wa.GroupParticipantAction) ([]types.GroupParticipant, error) {
	if jid != f.info.JID || len(users) != 1 || users[0].User != "member" {
		return nil, fmt.Errorf("membership arguments lost")
	}
	f.action = string(action)
	return []types.GroupParticipant{{JID: users[0], Error: 429}}, f.err
}

func TestGroupWriteResultsUseSavingSocketAndPreserveErrors(t *testing.T) {
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
	fake := &groupWriteWA{groupReadWA: groupReadWA{info: &types.GroupInfo{JID: types.NewJID("support", types.GroupServer)}}}
	stop, err := startSendDelegateServerForStore(context.Background(), storeDir, sendSpacing{}, func(ctx context.Context, req sendDelegateRequest) (sendDelegateResponse, error) {
		return executeDelegatedGroupMutation(ctx, db, fake, req)
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	var photo bytes.Buffer
	if err := jpeg.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	photoPath := filepath.Join(storeDir, "picture.jpg")
	if err := os.WriteFile(photoPath, photo.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		action string
		args   []string
		want   string
	}{
		{"rename", []string{"rename", "--jid", "support@g.us", "--name", "Renamed"}, `"name":"Renamed"`},
		{"picture", []string{"set-picture", "support@g.us", photoPath}, `"picture_id":"picture-id"`},
		{"create", []string{"create", "--name", "Support", "--user", "member@s.whatsapp.net", "--announce-only", "--locked", "--join-approval", "--linked-parent", "community@g.us"}, `"JID":"support@g.us"`},
		{"add", []string{"participants", "add", "--jid", "support@g.us", "--user", "member@s.whatsapp.net"}, `"Error":429`},
		{"remove", []string{"participants", "remove", "--jid", "support@g.us", "--user", "member@s.whatsapp.net"}, `"Error":429`},
		{"promote", []string{"participants", "promote", "--jid", "support@g.us", "--user", "member@s.whatsapp.net"}, `"Error":429`},
		{"demote", []string{"participants", "demote", "--jid", "support@g.us", "--user", "member@s.whatsapp.net"}, `"Error":429`},
		{"leave", []string{"leave", "--jid", "support@g.us"}, `"left":true`},
	} {
		t.Run(tc.action, func(t *testing.T) {
			args := append([]string{"--store", storeDir, "--json", "--timeout", "750ms", "groups"}, tc.args...)
			stdout, stderr, err := runPresenceDelegateHelper(t, args)
			if err != nil || fake.action != tc.action || !strings.Contains(stdout, tc.want) {
				t.Fatalf("write result lost: %s %s %v", stdout, stderr, err)
			}
			fake.err = fmt.Errorf("not participating in that group")
			_, stderr, err = runPresenceDelegateHelper(t, args)
			if err == nil || !strings.Contains(stderr, fake.err.Error()) {
				t.Fatal("write error was not preserved")
			}
			fake.err = nil
		})
	}
}

func TestGroupWritesWithoutSyncNeverOpenAnotherSession(t *testing.T) {
	for _, args := range [][]string{
		{"rename", "--jid", "support@g.us", "--name", "Support"},
		{"create", "--name", "Support"},
		{"leave", "--jid", "support@g.us"},
		{"participants", "add", "--jid", "support@g.us", "--user", "member@s.whatsapp.net"},
	} {
		storeDir := shortPresenceDelegateStoreDir(t)
		_, stderr, err := runPresenceDelegateHelper(t, append([]string{"--store", storeDir, "--json", "groups"}, args...))
		if err == nil || !strings.Contains(stderr, "running sync") {
			t.Error("write must report missing saving sync")
		}
		if _, err := os.Stat(filepath.Join(storeDir, "session.db")); !os.IsNotExist(err) {
			t.Error("write opened a standalone WhatsApp session")
		}
	}
}

func TestQueuedGroupWriteIsCanceledWhenCallerClosesSocket(t *testing.T) {
	server, caller := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer caller.Close()
	slot := make(chan struct{}, 1)
	called := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleSendDelegateConn(ctx, server, func(context.Context, sendDelegateRequest) (sendDelegateResponse, error) {
			called <- struct{}{}
			return sendDelegateResponse{OK: true}, nil
		}, slot, newSendPacer(sendSpacing{}))
	}()
	if err := json.NewEncoder(caller).Encode(sendDelegateRequest{Version: sendDelegateVersion, Kind: groupsMutateKind, GroupAction: "create", TimeoutMS: durationMillis(5 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	caller.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("queued group mutation survived its caller")
	}
	slot <- struct{}{}
	select {
	case <-called:
		t.Fatal("group mutation ran after its caller closed")
	default:
	}
}

func TestGroupMutationDeadlineFitsWorkerProcessBudget(t *testing.T) {
	skipPresenceDelegateSocketTestOnUnsupportedOS(t)
	storeDir := shortPresenceDelegateStoreDir(t)
	called := make(chan sendDelegateRequest, 1)
	stop, err := startSendDelegateServerForStore(context.Background(), storeDir, sendSpacing{}, func(_ context.Context, req sendDelegateRequest) (sendDelegateResponse, error) {
		called <- req
		return sendDelegateResponse{OK: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	started := time.Now()
	if _, err := delegateGroupMutation(context.Background(), &rootFlags{storeDir: storeDir, timeout: 5 * time.Minute}, sendDelegateRequest{GroupAction: "leave", To: "support@g.us"}); err != nil {
		t.Fatal(err)
	}
	req := <-called
	if req.DeadlineUnixMS <= started.UnixMilli() || req.DeadlineUnixMS > started.Add(56*time.Second).UnixMilli() {
		t.Fatal("group write can outlive the worker's sixty-second subprocess budget")
	}
}

func TestGroupReadWaitsForStartupSocketWhileSyncOwnsStore(t *testing.T) {
	skipPresenceDelegateSocketTestOnUnsupportedOS(t)
	storeDir := shortPresenceDelegateStoreDir(t)
	lk, err := lock.Acquire(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Release()
	server := make(chan func(), 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		stop, err := startSendDelegateServerForStore(context.Background(), storeDir, sendSpacing{}, func(_ context.Context, req sendDelegateRequest) (sendDelegateResponse, error) {
			return sendDelegateResponse{OK: true, GroupInfo: &types.GroupInfo{GroupName: types.GroupName{Name: "Support"}}}, nil
		})
		if err != nil {
			server <- func() {}
			return
		}
		server <- stop
	}()
	defer func() { (<-server)() }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := delegateSend(ctx, &rootFlags{storeDir: storeDir, timeout: time.Second}, sendDelegateRequest{Kind: groupsInfoKind, To: "support@g.us"})
	if err != nil {
		t.Fatalf("group read did not wait for saving sync socket: %v", err)
	}
	if resp.GroupInfo == nil || resp.GroupInfo.GroupName.Name != "Support" {
		t.Fatal("group response was lost after startup")
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
