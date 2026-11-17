package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func resolveValues(t *testing.T, words []string, json string) (map[string]string, []string) {
	t.Helper()
	v, err := readStdinValues(strings.NewReader(json))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	root := newRootCmd(&rootFlags{})
	args, err := applyStdinValues(root, words, v)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	cmd, _, err := root.Find(words)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	got := map[string]string{}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Changed {
			got[f.Name] = f.Value.String()
		}
	})
	return got, args
}

func TestValuesStdinEverySubcommandTheAppUses(t *testing.T) {
	cases := []struct {
		name  string
		words []string
		json  string
		want  map[string]string
		args  []string
	}{
		{"auth status", []string{"auth", "status"}, `{"flags":{}}`, map[string]string{}, nil},
		{"auth logout", []string{"auth", "logout"}, `{}`, map[string]string{}, nil},
		{"auth pair", []string{"auth"}, `{"flags":{"phone":"+15550000000"}}`, map[string]string{"phone": "+15550000000"}, nil},
		{"sync", []string{"sync"}, `{"flags":{"once":true,"idle-exit":"5s"}}`, map[string]string{"once": "true", "idle-exit": "5s"}, nil},
		{"store stats", []string{"store", "stats"}, `{}`, map[string]string{}, nil},
		{"groups refresh", []string{"groups", "refresh"}, `{}`, map[string]string{}, nil},
		{"contacts check", []string{"contacts", "check"}, `{"args":["+15550000000"]}`, map[string]string{}, []string{"--", "+15550000000"}},
		{"contacts resolve", []string{"contacts", "resolve"}, `{"args":["-x","--y"]}`, map[string]string{}, []string{"--", "-x", "--y"}},
		{"send text", []string{"send", "text"}, `{"flags":{"to":"+15550000000","message":"-hi","mention":["+15550000001","a,b"],"reply-to":"ID1","reply-to-sender":"x@s.whatsapp.net"}}`,
			map[string]string{"to": "+15550000000", "message": "-hi", "mention": "[+15550000001,\"a,b\"]", "reply-to": "ID1", "reply-to-sender": "x@s.whatsapp.net"}, nil},
		{"send file", []string{"send", "file"}, `{"flags":{"to":"+15550000000","file":"/tmp/a.png","filename":"a.png","caption":"c","mime":"image/png","as":"image"}}`,
			map[string]string{"to": "+15550000000", "file": "/tmp/a.png", "filename": "a.png", "caption": "c", "mime": "image/png", "as": "image"}, nil},
		{"messages list", []string{"messages", "list"}, `{"flags":{"chat":"c@g.us","limit":20,"after":"2026-01-01","asc":true}}`,
			map[string]string{"chat": "c@g.us", "limit": "20", "after": "2026-01-01", "asc": "true"}, nil},
		{"messages show", []string{"messages", "show"}, `{"flags":{"chat":"c","id":"m"}}`, map[string]string{"chat": "c", "id": "m"}, nil},
		{"messages delete", []string{"messages", "delete"}, `{"flags":{"chat":"c","id":"m"}}`, map[string]string{"chat": "c", "id": "m"}, nil},
		{"messages purge", []string{"messages", "purge"}, `{"flags":{"chat":"c","id":"m","confirm":true}}`, map[string]string{"chat": "c", "id": "m", "confirm": "true"}, nil},
		{"media download", []string{"media", "download"}, `{"flags":{"chat":"c","id":"m","output":"/tmp/o"}}`, map[string]string{"chat": "c", "id": "m", "output": "/tmp/o"}, nil},
		{"chats list", []string{"chats", "list"}, `{"flags":{"limit":5}}`, map[string]string{"limit": "5"}, nil},
		{"groups list", []string{"groups", "list"}, `{"flags":{"limit":5}}`, map[string]string{"limit": "5"}, nil},
		{"groups info", []string{"groups", "info"}, `{"flags":{"jid":"g@g.us"}}`, map[string]string{"jid": "g@g.us"}, nil},
		{"groups invite link get", []string{"groups", "invite", "link", "get"}, `{"flags":{"jid":"g@g.us"}}`, map[string]string{"jid": "g@g.us"}, nil},
		{"groups leave", []string{"groups", "leave"}, `{"flags":{"jid":"g@g.us"}}`, map[string]string{"jid": "g@g.us"}, nil},
		{"groups rename", []string{"groups", "rename"}, `{"flags":{"jid":"g@g.us","name":"--n"}}`, map[string]string{"jid": "g@g.us", "name": "--n"}, nil},
		{"groups set-picture", []string{"groups", "set-picture"}, `{"args":["g@g.us","/tmp/p.jpg"]}`, map[string]string{}, []string{"--", "g@g.us", "/tmp/p.jpg"}},
		{"groups create", []string{"groups", "create"}, `{"flags":{"name":"n","user":["+15550000001","+15550000002"]}}`, map[string]string{"name": "n", "user": "[+15550000001,+15550000002]"}, nil},
		{"participants list", []string{"groups", "participants", "list"}, `{"flags":{"jid":"g@g.us"}}`, map[string]string{"jid": "g@g.us"}, nil},
		{"participants add", []string{"groups", "participants", "add"}, `{"flags":{"jid":"g@g.us","user":["+15550000001","+15550000002"]}}`, map[string]string{"jid": "g@g.us", "user": "[+15550000001,+15550000002]"}, nil},
		{"participants remove", []string{"groups", "participants", "remove"}, `{"flags":{"jid":"g@g.us","user":["+15550000001"]}}`, map[string]string{"jid": "g@g.us", "user": "[+15550000001]"}, nil},
		{"participants promote", []string{"groups", "participants", "promote"}, `{"flags":{"jid":"g@g.us","user":["+15550000001"]}}`, map[string]string{"jid": "g@g.us", "user": "[+15550000001]"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, args := resolveValues(t, tc.words, tc.json)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("flags: got %v want %v", got, tc.want)
			}
			wantArgs := tc.words
			if tc.args != nil {
				wantArgs = append(append([]string{}, tc.words...), tc.args...)
			}
			if !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("args: got %v want %v", args, wantArgs)
			}
		})
	}
}

func TestValuesStdinRepeatedFlagsSetOncePerItem(t *testing.T) {
	root := newRootCmd(&rootFlags{})
	v, _ := readStdinValues(strings.NewReader(`{"flags":{"mention":["a,b","c"],"to":"x"}}`))
	if _, err := applyStdinValues(root, []string{"send", "text"}, v); err != nil {
		t.Fatal(err)
	}
	cmd, _, _ := root.Find([]string{"send", "text"})
	got, err := cmd.Flags().GetStringArray("mention")
	if err != nil || !reflect.DeepEqual(got, []string{"a,b", "c"}) {
		t.Fatalf("mention: %v %v", got, err)
	}
	root = newRootCmd(&rootFlags{})
	v, _ = readStdinValues(strings.NewReader(`{"flags":{"user":["a,b","c"]}}`))
	if _, err := applyStdinValues(root, []string{"groups", "create"}, v); err != nil {
		t.Fatal(err)
	}
	cmd, _, _ = root.Find([]string{"groups", "create"})
	users, err := cmd.Flags().GetStringSlice("user")
	if err != nil || !reflect.DeepEqual(users, []string{"a,b", "c"}) {
		t.Fatalf("user: %v %v", users, err)
	}
}

func TestValuesStdinInheritedFlagAccepted(t *testing.T) {
	got, _ := resolveValues(t, []string{"store", "stats"}, `{"flags":{"timeout":"30s","read-only":true}}`)
	if got["timeout"] != "30s" || got["read-only"] != "true" {
		t.Fatalf("inherited flags not set: %v", got)
	}
}

func TestValuesStdinRefusesArrayForScalarFlag(t *testing.T) {
	root := newRootCmd(&rootFlags{})
	v, _ := readStdinValues(strings.NewReader(`{"flags":{"to":["a","b"]}}`))
	if _, err := applyStdinValues(root, []string{"send", "text"}, v); err == nil {
		t.Fatal("array for a scalar flag must be refused")
	}
}
