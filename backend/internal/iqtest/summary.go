package iqtest

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/iqquality"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// accountSummary always covers all non-deleted accounts, independent of the
// table's current filters and page. Status categories are mutually exclusive.
func (s *Service) accountSummary(c *gin.Context) {
	var summary struct {
		Total       int `json:"total"`
		QualityFail int `json:"quality_fail"`
		InfraError  int `json:"infra_error"`
		Normal      int `json:"normal"`
		Untested    int `json:"untested"`
	}
	err := s.store.db.QueryRowContext(c.Request.Context(), `
 SELECT COUNT(*), COUNT(*) FILTER (WHERE quality = 'quality_fail'),
 COUNT(*) FILTER (WHERE quality = 'infra_error'), COUNT(*) FILTER (WHERE quality = 'normal'),
 COUNT(*) FILTER (WHERE quality = 'untested') FROM (
 SELECT `+iqquality.StatusSQL("a.id")+` AS quality FROM accounts a WHERE a.deleted_at IS NULL
 ) account_quality`).Scan(&summary.Total, &summary.QualityFail, &summary.InfraError, &summary.Normal, &summary.Untested)
	if err != nil {
		response.InternalError(c, "failed to load IQ account summary")
		return
	}
	response.Success(c, summary)
}
