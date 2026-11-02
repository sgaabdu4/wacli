package main

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"os"
	"strings"
	"time"

	"github.com/openclaw/wacli/internal/out"
	"github.com/spf13/cobra"
)

func newGroupsSetPictureCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "set-picture <jid> <jpeg path>",
		Short: "Set a group picture from a JPEG file",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := flags.requireWritable(); err != nil {
				return err
			}
			jid, err := parseGroupJID(args[0])
			if err != nil {
				return err
			}
			photo, err := os.ReadFile(args[1])
			if err != nil {
				return fmt.Errorf("read picture: %w", err)
			}
			if _, err := jpeg.DecodeConfig(bytes.NewReader(photo)); err != nil {
				return fmt.Errorf("picture must be JPEG: %w", err)
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()
			resp, err := delegateGroupMutation(ctx, flags, sendDelegateRequest{GroupAction: "picture", To: jid.String(), GroupPhoto: photo})
			if err != nil {
				return err
			}
			id := resp.PictureID
			if flags.asJSON {
				return out.WriteJSON(os.Stdout, map[string]any{"jid": jid.String(), "picture_id": id})
			}
			fmt.Fprintln(os.Stdout, "OK")
			return nil
		},
	}
}

func newGroupsInfoCmd(flags *rootFlags) *cobra.Command {
	var jidStr string
	cmd := &cobra.Command{
		Use:   "info",
		Short: "Fetch group info (live) and update local DB",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(jidStr) == "" {
				return fmt.Errorf("--jid is required")
			}
			if err := flags.requireWritable(); err != nil {
				return err
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			resp, delegateErr := delegateSend(ctx, flags, sendDelegateRequest{Kind: groupsInfoKind, To: jidStr})
			if delegateErr != nil {
				return fmt.Errorf("group info needs a running sync to save incoming messages; start `wacli sync` and retry: %w", delegateErr)
			}
			info := resp.GroupInfo
			if info == nil {
				return fmt.Errorf("group info is unavailable; upgrade the running sync and retry")
			}

			if flags.asJSON {
				return out.WriteJSON(os.Stdout, info)
			}

			fmt.Fprintf(os.Stdout, "JID: %s\nName: %s\nOwner: %s\nType: %s\n",
				info.JID.String(),
				sanitize(info.GroupName.Name),
				info.OwnerJID.String(),
				groupKindLabel(info.IsParent, info.LinkedParentJID.String()),
			)
			if !info.LinkedParentJID.IsEmpty() {
				fmt.Fprintf(os.Stdout, "Parent: %s\n", info.LinkedParentJID.String())
			}
			fmt.Fprintf(os.Stdout, "Created: %s\nParticipants: %d\n",
				info.GroupCreated.Local().Format(time.RFC3339),
				len(info.Participants),
			)
			return nil
		},
	}
	cmd.Flags().StringVar(&jidStr, "jid", "", "group JID (…@g.us)")
	return cmd
}

func newGroupsRenameCmd(flags *rootFlags) *cobra.Command {
	var jidStr string
	var name string
	cmd := &cobra.Command{
		Use:   "rename",
		Short: "Rename group",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(jidStr) == "" || strings.TrimSpace(name) == "" {
				return fmt.Errorf("--jid and --name are required")
			}
			if err := flags.requireWritable(); err != nil {
				return err
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			gjid, err := parseGroupJID(jidStr)
			if err != nil {
				return err
			}
			if _, err := delegateGroupMutation(ctx, flags, sendDelegateRequest{GroupAction: "rename", To: gjid.String(), Name: name}); err != nil {
				return err
			}
			if flags.asJSON {
				return out.WriteJSON(os.Stdout, map[string]any{"jid": gjid.String(), "name": name})
			}
			fmt.Fprintln(os.Stdout, "OK")
			return nil
		},
	}
	cmd.Flags().StringVar(&jidStr, "jid", "", "group JID (…@g.us)")
	cmd.Flags().StringVar(&name, "name", "", "new name")
	return cmd
}

func newGroupsLeaveCmd(flags *rootFlags) *cobra.Command {
	var jidStr string
	cmd := &cobra.Command{
		Use:   "leave",
		Short: "Leave a group",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(jidStr) == "" {
				return fmt.Errorf("--jid is required")
			}
			if err := flags.requireWritable(); err != nil {
				return err
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()

			gjid, err := parseGroupJID(jidStr)
			if err != nil {
				return err
			}
			if _, err := delegateGroupMutation(ctx, flags, sendDelegateRequest{GroupAction: "leave", To: gjid.String()}); err != nil {
				return err
			}
			if flags.asJSON {
				return out.WriteJSON(os.Stdout, map[string]any{"jid": gjid.String(), "left": true})
			}
			fmt.Fprintln(os.Stdout, "OK")
			return nil
		},
	}
	cmd.Flags().StringVar(&jidStr, "jid", "", "group JID (…@g.us)")
	return cmd
}
