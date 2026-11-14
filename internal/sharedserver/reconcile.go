package sharedserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Repair old MySQL writes where the append-only change was committed but the
// resource update matched no row because the positional arguments were wrong.
func reconcileResourceHeads(ctx context.Context, db *sql.DB, driver string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT c.kind,c.resource_id,c.revision,c.deleted,c.data FROM shared_changes c JOIN (SELECT kind,resource_id,MAX(sequence) AS sequence FROM shared_changes GROUP BY kind,resource_id) h ON c.sequence=h.sequence`)
	if err != nil {
		return err
	}
	type head struct {
		kind, id string
		revision int64
		deleted  bool
		data     []byte
	}
	var heads []head
	for rows.Next() {
		var item head
		if err := rows.Scan(&item.kind, &item.id, &item.revision, &item.deleted, &item.data); err != nil {
			rows.Close()
			return err
		}
		heads = append(heads, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range heads {
		var current int64
		err := tx.QueryRowContext(ctx, queryForDriver(driver, `SELECT revision FROM shared_resources WHERE kind=$1 AND id=$2 FOR UPDATE`), item.kind, item.id).Scan(&current)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("missing resource head %s/%s", item.kind, item.id)
			}
			return err
		}
		if current > item.revision {
			return fmt.Errorf("resource head ahead of changes: %s/%s", item.kind, item.id)
		}
		if current == item.revision {
			continue
		}
		statement := `UPDATE shared_resources SET revision=$3,data=$4::jsonb,deleted=$5,updated_at=now() WHERE kind=$1 AND id=$2 AND revision=$6`
		args := []any{item.kind, item.id, item.revision, string(item.data), item.deleted, current}
		if driver == "mysql" {
			statement = `UPDATE shared_resources SET revision=?,data=?,deleted=?,updated_at=now() WHERE kind=? AND id=? AND revision=?`
			args = []any{item.revision, string(item.data), item.deleted, item.kind, item.id, current}
		}
		result, err := tx.ExecContext(ctx, queryForDriver(driver, statement), args...)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("resource head changed during repair: %s/%s", item.kind, item.id)
		}
	}
	return tx.Commit()
}

func queryForDriver(driver, statement string) string {
	if driver == "mysql" {
		return parameterPattern.ReplaceAllString(statement, "?")
	}
	return statement
}
