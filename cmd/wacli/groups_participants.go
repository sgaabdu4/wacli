package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/store"
	"github.com/spf13/cobra"
)

func newGroupsParticipantsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "participants",
		Short: "Manage group participants",
	}
	cmd.AddCommand(newGroupsParticipantsListCmd(flags))
	cmd.AddCommand(newGroupsParticipantsActionCmd(flags, "add"))
	cmd.AddCommand(newGroupsParticipantsActionCmd(flags, "remove"))
	cmd.AddCommand(newGroupsParticipantsActionCmd(flags, "promote"))
	cmd.AddCommand(newGroupsParticipantsActionCmd(flags, "demote"))
	return cmd
}

func newGroupsParticipantsListCmd(flags *rootFlags) *cobra.Command {
	var group string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List participants (local snapshot; sync --refresh-groups to update)",
		Long: `List participants from the last group snapshot saved in the local database.

This command does not connect to WhatsApp. The snapshot can be empty or stale.
Run "wacli sync --once --refresh-groups" to fetch joined groups and replace
their local participant snapshots.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(group) == "" {
				return fmt.Errorf("--jid is required")
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			var participants []store.GroupParticipant
			var delegateErr error = errSendDelegateUnavailable
			if !flags.isReadOnly() {
				var resp sendDelegateResponse
				resp, delegateErr = delegateSend(ctx, flags, sendDelegateRequest{Kind: groupsParticipantsKind, To: group})
				participants = resp.Participants
			}
			if delegateErr != nil {
				if !errors.Is(delegateErr, errSendDelegateUnavailable) {
					return delegateErr
				}
				a, lk, err := newApp(ctx, flags, false, false)
				if err != nil {
					return err
				}
				defer closeApp(a, lk)

				participants, err = a.DB().ListGroupParticipants(group)
				if err != nil {
					return err
				}
			}
			if participants == nil {
				participants = []store.GroupParticipant{}
			}

			if flags.asJSON {
				return out.WriteJSON(os.Stdout, participants)
			}

			w := newTableWriter(os.Stdout)
			fmt.Fprintln(w, "USER JID\tROLE\tUPDATED")
			for _, p := range participants {
				updated := "-"
				if !p.UpdatedAt.IsZero() {
					updated = p.UpdatedAt.Local().Format("2006-01-02 15:04:05")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", p.UserJID, p.Role, updated)
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&group, "jid", "", "group JID (…@g.us)")
	return cmd
}

func newGroupsParticipantsActionCmd(flags *rootFlags, action string) *cobra.Command {
	var group string
	var users []string
	cmd := &cobra.Command{
		Use:   action,
		Short: action + " participants",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(group) == "" || len(users) == 0 {
				return fmt.Errorf("--jid and at least one --user are required")
			}
			if err := flags.requireWritable(); err != nil {
				return err
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			gjid, err := parseGroupJID(group)
			if err != nil {
				return err
			}
			if _, err := parseGroupUserJIDs(users); err != nil {
				return err
			}
			resp, err := delegateGroupMutation(ctx, flags, sendDelegateRequest{GroupAction: action, To: gjid.String(), Phones: users})
			if err != nil {
				return err
			}
			updated := resp.Updated
			if flags.asJSON {
				return out.WriteJSON(os.Stdout, updated)
			}
			fmt.Fprintln(os.Stdout, "OK")
			return nil
		},
	}
	cmd.Flags().StringVar(&group, "jid", "", "group JID (…@g.us)")
	cmd.Flags().StringSliceVar(&users, "user", nil, "user phone number (+E164 and formatting ok) or JID (repeatable)")
	return cmd
}
