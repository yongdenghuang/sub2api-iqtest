package repository

import (
	"context"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqquality"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

// Display-only ordering; never changes the priority used by account scheduling.
func accountCustomListOrder(key, direction string) ([]func(*entsql.Selector), bool) {
	if key != "iq_status" && key != "display_order" {
		return nil, false
	}
	order := "ASC"
	if direction == pagination.SortOrderDesc {
		order = "DESC"
	}
	return []func(*entsql.Selector){func(s *entsql.Selector) {
		id := s.C(dbaccount.FieldID)
		expression := "(SELECT position FROM iq_account_display_order display_order WHERE display_order.account_id = " + id + ")"
		if key == "iq_status" {
			// Match IQStatusCell: only the latest candy verdict matters. Unknown,
			// skipped and request failures always remain after known verdicts.
			expression = "(SELECT CASE iq.state ->> 'last_outcome' WHEN 'quality_fail' THEN 0 WHEN 'pass' THEN 1 END FROM iq_account_state iq WHERE iq.account_id = " + id + " AND iq.kind = 'candy')"
		}
		s.OrderExpr(entsql.Expr(expression + " " + order + " NULLS LAST"))
		if key == "display_order" {
			s.OrderBy(entsql.Asc(s.C(dbaccount.FieldName)))
		}
		s.OrderBy(entsql.Asc(id))
	}}, true
}

func applyIQQualityFilter(ctx context.Context, q *dbent.AccountQuery) *dbent.AccountQuery {
	if quality := iqquality.FromContext(ctx); quality != "" {
		q.Where(func(s *entsql.Selector) {
			s.Where(entsql.P(func(b *entsql.Builder) {
				b.WriteString(iqquality.StatusSQL(s.C(dbaccount.FieldID))).WriteString(" = ").Arg(quality)
			}))
		})
	}
	return q
}
