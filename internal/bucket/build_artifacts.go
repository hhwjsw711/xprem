package bucket

import (
	"errors"
	"fmt"
	"time"
	"xprem/internal/types"
)

var ErrBuildDownloadExpired = errors.New("build download link expired")

// BuildsPrefix is the bucket-root directory of every build artifact, a sibling
// of the {appId}/ OTA trees.
const BuildsPrefix = "builds"

const (
	buildStagingDir   = ".uploads"
	buildUploadExpiry = 10 * time.Minute
)

type BuildArtifact struct {
	IdentifierID string
	BuildID      string
	Type         types.BuildArtifactType
}

func (r BuildArtifact) downloadContentType() string {
	if r.Type == types.BuildArtifactAPK {
		return "application/vnd.android.package-archive"
	}
	return "application/octet-stream"
}

func (r BuildArtifact) downloadDisposition() string {
	return fmt.Sprintf(`attachment; filename="%s.%s"`, r.BuildID, r.Type)
}

func (r BuildArtifact) Validate() error {
	if _, err := r.Type.Platform(); err != nil {
		return err
	}
	if err := validateUUID("identifierId", r.IdentifierID); err != nil {
		return err
	}
	return validateUUID("buildId", r.BuildID)
}

// Key is builds/{platform}/{identifierId}/{buildId}.{type}, with an
// .uploads/ segment before the file name for the staging copy.
func (r BuildArtifact) Key(staging bool) (string, error) {
	platform, err := r.Type.Platform()
	if err != nil {
		return "", err
	}
	folder := BuildsPrefix + "/" + string(platform) + "/" + r.IdentifierID + "/"
	if staging {
		folder += buildStagingDir + "/"
	}
	return folder + r.BuildID + "." + string(r.Type), nil
}
