package service

import (
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Official release binaries do not contain the custom IQTest feature set.
var ErrIQTestManagedUpdate = infraerrors.Conflict("IQTEST_MANAGED_UPDATE", "IQTest updates and rollbacks are managed by the custom deployment workflow")

func (s *UpdateService) isIQTestBuild() bool {
	return s.buildType == "iqtest" || strings.Contains(s.currentVersion, "-iqtest.")
}
