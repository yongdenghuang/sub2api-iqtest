// Package iqquality shares the account-list classification with IQ summaries.
package iqquality

import (
	"context"
	"fmt"
	"strings"
)

type filterKey struct{}

func WithFilter(ctx context.Context, value string) (context.Context, error) {
	value = strings.TrimSpace(value)
	switch value {
	case "", "quality_fail", "infra_error", "normal", "untested":
		return context.WithValue(ctx, filterKey{}, value), nil
	default:
		return nil, fmt.Errorf("invalid IQ quality filter")
	}
}

func FromContext(ctx context.Context) string {
	value, _ := ctx.Value(filterKey{}).(string)
	return value
}

// StatusSQL classifies each account once, with quality failures taking precedence
// over request errors. Skipped checks provide no quality conclusion. accountID
// must be a trusted SQL column expression, never request input.
func StatusSQL(accountID string) string {
	exists := func(outcome string) string {
		return "EXISTS (SELECT 1 FROM iq_account_state iq WHERE iq.account_id = " + accountID +
			" AND iq.kind IN ('candy', 'pelican_svg') AND iq.state ->> 'last_outcome' = '" + outcome + "')"
	}
	return "(CASE WHEN " + exists("quality_fail") + " THEN 'quality_fail' WHEN " + exists("infra_error") +
		" THEN 'infra_error' WHEN " + exists("pass") + " THEN 'normal' ELSE 'untested' END)"
}
