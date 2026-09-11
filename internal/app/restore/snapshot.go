package restore

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/wxbackup/wxbackup/internal/adapter/backupfmt"
	"github.com/wxbackup/wxbackup/internal/domain"
	"github.com/wxbackup/wxbackup/internal/store/sqlite"
)

const dumpPage = 1000

func loadSnapshot(ctx context.Context, adb *sqlite.AccountDB, acct domain.Account, sel domain.RestoreSelector) (backupfmt.Snapshot, error) {
	convs, err := adb.ListConversations(ctx)
	if err != nil {
		return backupfmt.Snapshot{}, err
	}
	wanted := talkerFilter(sel)
	if wanted != nil {
		var filtered []domain.Conversation
		found := map[string]struct{}{}
		for _, c := range convs {
			if _, ok := wanted[c.TalkerID]; ok {
				filtered = append(filtered, c)
				found[c.TalkerID] = struct{}{}
			}
		}
		for id := range wanted {
			if _, ok := found[id]; !ok {
				return backupfmt.Snapshot{}, ErrUnknownSessionID
			}
		}
		convs = filtered
	}

	var msgs []domain.Message
	for _, c := range convs {
		got, err := dumpMessages(ctx, adb, c.TalkerID)
		if err != nil {
			return backupfmt.Snapshot{}, err
		}
		msgs = append(msgs, got...)
	}

	media, err := adb.ListMedia(ctx)
	if err != nil {
		return backupfmt.Snapshot{}, err
	}
	for i := range media {
		if media[i].Path == "" {
			continue
		}
		if _, err := os.Stat(media[i].Path); err != nil {
			media[i].Path = ""
			media[i].Available = false
		}
	}

	return backupfmt.Snapshot{
		AccountID:     acct.ID,
		WxID:          acct.WxID,
		Conversations: convs,
		Messages:      msgs,
		Media:         media,
	}, nil
}

func dumpMessages(ctx context.Context, adb *sqlite.AccountDB, talkerID string) ([]domain.Message, error) {
	var (
		all    []domain.Message
		before *sqlite.MessageCursor
	)
	for {
		page, err := adb.ListMessages(ctx, talkerID, before, dumpPage)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		all = append(all, page...)
		if len(page) < dumpPage {
			break
		}
		cur := sqlite.CursorFromMessage(page[len(page)-1])
		before = &cur
	}
	// ListMessages is newest-first; official shards store oldest-first.
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	return all, nil
}

func talkerFilter(sel domain.RestoreSelector) map[string]struct{} {
	if sel.Kind != domain.RestoreSessionIDs {
		return nil
	}
	out := make(map[string]struct{}, len(sel.SessionIDs))
	for _, id := range sel.SessionIDs {
		out[id] = struct{}{}
	}
	return out
}

func listPackageFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == backupfmt.BackupDBName || filepath.Ext(name) == "" && len(name) >= 8 && name[:4] == "BAK_" {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}
