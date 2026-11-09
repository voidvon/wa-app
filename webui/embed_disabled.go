//go:build !dashboard

package dashboard

import (
	"fmt"
	"io/fs"
)

func Files() (fs.FS, error) {
	return nil, fmt.Errorf("dashboard assets are not embedded; build with scripts/build-binary.sh or set WA_APP_DASHBOARD_STATIC_DIR")
}
