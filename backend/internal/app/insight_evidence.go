package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
)

type InsightOrders struct {
	Total      int             `json:"total"`
	Items      []BusinessOrder `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

func metricCursor(u User, q OrderQuery, id int64) string {
	q.MetricCursor = ""
	scope, _ := json.Marshal([]any{u.OrgID, u.ID, u.Role, q, id})
	mac := hmac.New(sha256.New, []byte(os.Getenv("MCP_CALL_SECRET")))
	mac.Write(scope)
	return strconv.FormatInt(id, 10) + "." + hex.EncodeToString(mac.Sum(nil))
}

func readInsightOrders(ctx context.Context, tx *sql.Tx, u User, q OrderQuery, metric, where string, args []any, total int) (InsightOrders, error) {
	result := InsightOrders{Total: total, Items: []BusinessOrder{}}
	source := q
	source.Metric = metric
	var after int64
	if q.MetricCursor != "" {
		head, _, _ := strings.Cut(q.MetricCursor, ".")
		id, err := strconv.ParseInt(head, 10, 64)
		if err != nil || id < 1 || !hmac.Equal([]byte(metricCursor(u, source, id)), []byte(q.MetricCursor)) {
			return result, bad("该指标游标无效，请重新查询")
		}
		after = id
	}
	// Keep the original scope predicate unambiguous. Look up the current assignee
	// only within the order's organization.
	rows, err := tx.QueryContext(ctx, "SELECT w.id,w.number,w.status,w.category,w.location,w.description,w.version,COALESCE((SELECT display_name FROM users WHERE id=w.assignee_id AND org_id=w.org_id),'') FROM work_orders w WHERE "+where+" AND w.id>? ORDER BY w.id LIMIT 6", append(append([]any{}, args...), after)...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var o BusinessOrder
		if err := rows.Scan(&o.ID, &o.Number, &o.Status, &o.Category, &o.Location, &o.Description, &o.Version, &o.Responsible); err != nil {
			return result, err
		}
		if len(result.Items) == 5 {
			result.NextCursor = metricCursor(u, source, result.Items[4].ID)
			break
		}
		result.Items = append(result.Items, o)
	}
	return result, rows.Err()
}
