package sharedworkspace

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

// Diff compares a freshly exported local snapshot with the last server
// materialization. It does not send Keychain values or device-only paths.
func Diff(ctx context.Context, db *sql.DB, snapshot Snapshot) ([]ResourceMutation, error) {
	rows, err := db.QueryContext(ctx, `SELECT kind,resource_id,revision,content_hash FROM shared_resource_revisions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type old struct {
		revision int64
		hash     string
	}
	previous := map[string]old{}
	for rows.Next() {
		var kind, id string
		var value old
		if err := rows.Scan(&kind, &id, &value.revision, &value.hash); err != nil {
			return nil, err
		}
		previous[kind+"/"+id] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	mutations := []ResourceMutation{}
	for _, item := range snapshot.Records {
		key := item.Kind + "/" + item.ID
		oldValue := previous[key]
		hash, err := CanonicalHash(item.Data)
		if err != nil {
			return nil, err
		}
		if oldValue.revision == 0 || oldValue.hash != hash {
			mutations = append(mutations, ResourceMutation{Kind: item.Kind, ID: item.ID, ExpectedRevision: oldValue.revision, Data: item.Data})
		}
		delete(previous, key)
	}
	for key, value := range previous {
		if value.hash == "" {
			continue // Already deleted on the server.
		}
		kind, id, ok := splitResourceKey(key)
		if !ok {
			return nil, errors.New("invalid local revision key")
		}
		mutations = append(mutations, ResourceMutation{Kind: kind, ID: id, ExpectedRevision: value.revision, Deleted: true})
	}
	order := map[string]int{}
	for index, spec := range Entities {
		order[spec.Kind] = index
	}
	sort.SliceStable(mutations, func(i, j int) bool {
		if mutations[i].Deleted != mutations[j].Deleted {
			return !mutations[i].Deleted
		}
		if mutations[i].Deleted {
			return order[mutations[i].Kind] > order[mutations[j].Kind]
		}
		return order[mutations[i].Kind] < order[mutations[j].Kind]
	})
	return mutations, nil
}

func splitResourceKey(value string) (string, string, bool) {
	for i := 0; i < len(value); i++ {
		if value[i] == '/' {
			return value[:i], value[i+1:], true
		}
	}
	return "", "", false
}
