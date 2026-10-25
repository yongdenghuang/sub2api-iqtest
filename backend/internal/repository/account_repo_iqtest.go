package repository

import (
	"context"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqquality"
)

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
