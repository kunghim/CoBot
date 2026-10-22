package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kunghim/CoBot/internal/domain"
)

// droppedTasks is what a team loses when version 23 drops the tasks of the
// old collaboration model.
type droppedTasks struct {
	total, open int
}

// countLegacyTasks counts each team's tasks before the upgrade drops them.
func countLegacyTasks(ctx context.Context, db *sql.DB) (map[string]droppedTasks, error) {
	var exists int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tasks'`).Scan(&exists); err != nil || exists == 0 {
		return nil, err // a profile older than tasks has none to lose
	}
	rows, err := db.QueryContext(ctx, `SELECT team_id, COUNT(*), SUM(CASE WHEN status IN ('done', 'cancelled') THEN 0 ELSE 1 END) FROM tasks GROUP BY team_id`)
	if err != nil {
		return nil, fmt.Errorf("count tasks before upgrade: %w", err)
	}
	defer rows.Close()
	out := map[string]droppedTasks{}
	for rows.Next() {
		var team string
		var d droppedTasks
		if err := rows.Scan(&team, &d.total, &d.open); err != nil {
			return nil, err
		}
		if d.total > 0 {
			out[team] = d
		}
	}
	return out, rows.Err()
}

// tellDroppedTasks leaves each team that lost tasks a note in its project
// manager's conversation: what went, why, and where the snapshot taken
// before the upgrade is. Without it the tasks would just be gone, with
// only an unexplained backup file to show for them.
func tellDroppedTasks(ctx context.Context, db *sql.DB, dropped map[string]droppedTasks, backupPath string) error {
	if len(dropped) == 0 {
		return nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for team, d := range dropped {
		var leader string
		if err := tx.QueryRowContext(ctx, `SELECT leader_id FROM teams WHERE id = ?`, team).Scan(&leader); err != nil || leader == "" {
			continue
		}
		text := fmt.Sprintf("CoBot 升级后，团队协作改成了「需求 + 接力卡片」，旧版的任务没法搬过来，已删除：本团队共 %d 个任务，其中 %d 个还没完成。", d.total, d.open)
		if backupPath != "" {
			text += "升级前的完整数据备份在：" + backupPath + "。"
		}
		text += "还要做的事，重新发给项目经理即可。"
		blocks, err := json.Marshal([]domain.TextBlock{{Type: "text", Text: text}})
		if err != nil {
			return err
		}
		if err := InsertMessage(ctx, tx, domain.Message{
			ID: domain.NewID(), TeamID: team, BotID: leader,
			Kind: domain.MessageKindSystem, Blocks: blocks, CreatedAt: time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("note dropped tasks: %w", err)
		}
	}
	return tx.Commit()
}
